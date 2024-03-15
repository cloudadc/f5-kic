package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitee.com/zongzw/f5-as3-parsing/as3parsing"
	f5_bigip "github.com/f5devcentral/f5-bigip-rest-go/bigip"
	"github.com/f5devcentral/f5-bigip-rest-go/utils"

	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func newRestConfig(kubeConfig string) *rest.Config {
	var config *rest.Config
	var err error

	if kubeConfig == "" {
		config, err = rest.InClusterConfig()
		if nil != err {
			panic(err)
		}
	} else if _, err := os.Stat(kubeConfig); err == nil {
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if nil != err {
			panic(err)
		}
	} else {
		config, err = clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfig))
		if nil != err {
			panic(err)
		}
	}
	return config
}

func newKubeClient(kubeConfig string) *kubernetes.Clientset {
	config := newRestConfig(kubeConfig)
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return client
}

func getCredentials() error {
	slog := utils.LogFromContext(context.TODO())
	if len(cmdflags.credsDir) > 0 {
		var usr, pass, bigipURL string
		var err error
		if strings.HasSuffix(cmdflags.credsDir, "/") {
			usr = cmdflags.credsDir + "username"
			pass = cmdflags.credsDir + "password"
			bigipURL = cmdflags.credsDir + "url"
		} else {
			usr = cmdflags.credsDir + "/username"
			pass = cmdflags.credsDir + "/password"
			bigipURL = cmdflags.credsDir + "/url"
		}

		setField := func(field *string, filename, fieldType string) error {
			f, err := os.Open(filename)
			if err != nil {
				return err
			}
			fileBytes, readErr := io.ReadAll(f)
			if readErr != nil {
				slog.Debugf(fmt.Sprintf(
					"No %s in credentials directory, falling back to CLI argument", fieldType))
				if len(*field) == 0 {
					return fmt.Errorf(fmt.Sprintf("BIG-IP %s not specified", fieldType))
				}
			} else {
				*field = strings.TrimSpace(string(fileBytes))
			}
			return nil
		}

		err = setField(&cmdflags.bigipUsername, usr, "username")
		if err != nil {
			return err
		}
		err = setField(&cmdflags.bigipPassword, pass, "password")
		if err != nil {
			return err
		}
		err = setField(&cmdflags.bigipURL, bigipURL, "url")
		if err != nil {
			return err
		}
	}
	// Verify URL is valid
	if !strings.HasPrefix(cmdflags.bigipURL, "https://") {
		cmdflags.bigipURL = "https://" + cmdflags.bigipURL
	}
	u, err := url.Parse(cmdflags.bigipURL)
	if nil != err {
		return fmt.Errorf("error parsing url: %s", err)
	}
	if len(u.Path) > 0 && u.Path != "/" {
		return fmt.Errorf("BIGIP-URL path must be empty or '/'; check URL formatting and/or remove %s from path",
			u.Path)
	}
	return nil
}

func (i *arrayFlags) String() string {
	return fmt.Sprint(*i)
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func sendDeployRequest(r DeployRequest) {
	slog := utils.LogFromContext(r.Context)
	dryRunKinds := []string{Kind_ltm_virtual, Kind_ltm_pool, Kind_ltm_member,
		Kind_ltm_node, Kind_config_sys,
		Kind_net_arps, Kind_net_fdb, Kind_net_route}
	if cmdflags.dryRun && utils.Contains(dryRunKinds, r.Kind) {
		slog.Infof("not handling %s %s in dry-run mode", r.Operation, r.Kind)
		return
	}
	if r.Retries == 0 {
		r.Retries = MaxRetries
	}
	slog.Infof("sent request %s %s %s %s", r.Operation, r.Kind, r.Partition, r.Name)
	pendingDeploys.Add(r)
}

func insertDeployRequest(r DeployRequest) {
	slog := utils.LogFromContext(r.Context)
	dryRunKinds := []string{Kind_ltm_virtual, Kind_ltm_pool, Kind_ltm_member,
		Kind_ltm_node, Kind_config_sys,
		Kind_net_arps, Kind_net_fdb, Kind_net_route}
	if cmdflags.dryRun && utils.Contains(dryRunKinds, r.Kind) {
		slog.Infof("not handling %s %s in dry-run mode", r.Operation, r.Kind)
		return
	}
	if r.Retries == 0 {
		r.Retries = MaxRetries
	}
	slog.Infof("inserted request %s %s %s %s", r.Operation, r.Kind, r.Partition, r.Name)
	pendingDeploys.Insert(r)
}

func retryDeployRequest(r DeployRequest) {
	slog := utils.LogFromContext(r.Context)
	r.Retries -= 1
	if r.Retries >= 0 {
		if r.Kind == Kind_ltm_pool && r.Operation == Operation_update {
			slog.Infof("reinserting ltm request %v", r)
			insertDeployRequest(r)
		} else {
			slog.Infof("retrying ltm request %v", r)
			<-time.After(1 * time.Second)
			pendingDeploys.Add(r)
		}
	} else {
		slog.Errorf("request was retried max time: %v", r)
	}
}

func filterMembersWithPort(ctx context.Context, port int, members []SvcEpsMember) SvcEpsMembers {
	slog := utils.LogFromContext(ctx)

	filtered := SvcEpsMembers{}
	foundport := false
	firstport := -1
	for _, mb := range members {
		if firstport == -1 {
			firstport = mb.TargetPort
		}
		if mb.TargetPort == port {
			foundport = true
			filtered = append(filtered, mb)
		}
	}
	if !foundport && cmdflags.ignoreSvcPort {
		slog.Infof("servicePort %d was not found in endpoints' targetPort list, use the first targetPort %d by default", port, firstport)
		port = firstport
		for _, mb := range members {
			if mb.TargetPort == port {
				filtered = append(filtered, mb)
			}
		}
	}
	if len(filtered) == 0 && len(members) != 0 {
		slog.Warnf("found no member with port %d", port)
	}
	return filtered
}

func svcLabeled(labels map[string]string) bool {
	_, t := labels[Label_Tenant]
	_, a := labels[Label_App]
	_, p := labels[Label_Pool]
	return t && a && p
}

func cmLabeled(labels map[string]string) bool {
	_, t := labels[Label_f5type]
	_, a := labels[Label_as3]

	return t && a && labels[Label_f5type] == "virtual-server"
}

func detectCNIType(node *v1.Node) string {
	for _, c := range node.Status.Conditions {
		if c.Reason == "CiliumIsUp" {
			return "cilium"
		}
		if c.Reason == "CalicoIsUp" {
			return "calico"
		}
		if c.Reason == "FlannelIsUp" {
			return "flannel"
		}
	}

	// the above way takes precedence.
	// For bigip Node, seems that we can not use the Reason above.
	// Add for other scenarios when needed. Seems not needed for calico/cilium
	if _, ok := node.Annotations["flannel.alpha.coreos.com/backend-data"]; ok && node.Annotations["flannel.alpha.coreos.com/backend-data"] != "null" {
		return "flannel"
	}

	if _, ok := node.Annotations["flannel.alpha.coreos.com/backend-v6-data"]; ok && node.Annotations["flannel.alpha.coreos.com/backend-v6-data"] != "null" {
		return "flannel"
	}

	return "unknown"
}

// Convert an IPV4 string to a fake MAC address.
func ipv4ToMac(addr string) string {
	ip := strings.Split(addr, ".")
	if len(ip) != 4 {
		return ""
	}
	var intIP [4]int
	for i, val := range ip {
		intIP[i], _ = strconv.Atoi(val)
	}
	return fmt.Sprintf("0a:0a:%02x:%02x:%02x:%02x", intIP[0], intIP[1], intIP[2], intIP[3])
}

// poolsInCfg return array of <Partition/subFolder/poolName>
func poolsInCfg(cfg *LTMResources) []string {
	pfns := []string{}
	if cfg == nil {
		return pfns
	}
	rexPool := regexp.MustCompile(`^ltm/pool/.*$`)

	for k, ress := range cfg.Json {
		for r := range ress.(map[string]interface{}) {
			if rexPool.MatchString(r) {
				name := strings.ReplaceAll(r, "ltm/pool/", "")
				pfn := utils.Keyname(cfg.Partition, k, name)
				pfns = append(pfns, pfn)
			}
		}
	}

	return pfns
}

// virtualsInCfg return array of <Partition/subFolder/virtualName>
func virtualsInCfg(cfg *LTMResources) []string {
	pfns := []string{}
	if cfg == nil {
		return pfns
	}
	rexVirtual := regexp.MustCompile(`^ltm/virtual/.*$`)

	for k, ress := range cfg.Json {
		for r := range ress.(map[string]interface{}) {
			if rexVirtual.MatchString(r) {
				name := strings.ReplaceAll(r, "ltm/virtual/", "")
				pfn := utils.Keyname(cfg.Partition, k, name)
				pfns = append(pfns, pfn)
			}
		}
	}

	return pfns
}

func as3ToRestJson(ctx context.Context, template string, restjson *map[string]interface{}) error {
	slog := utils.LogFromContext(ctx)
	var as3json map[string]interface{}
	if err := json.Unmarshal([]byte(template), &as3json); err != nil {
		return err
	}
	as3body, _ := utils.MarshalNoEscaping(as3json)
	slog.Debugf("initial as3body: %s", as3body)
	restobjs, err := as3parsing.ParseAS3(ctx, as3json)
	if err != nil {
		return err
	}

	if err := manipulateRestObjects(restobjs); err != nil {
		return fmt.Errorf("failed to manipulate rest objects: %s", err.Error())
	}

	*restjson = restobjs
	return nil
}

func memberPortAndAttrsFromCfg(ctx context.Context, keyname string, as3body string) (int, map[string]interface{}) {
	slog := utils.LogFromContext(ctx)
	port, attrs := 80, map[string]interface{}{}

	pfn := strings.Split(keyname, "/")
	partition, subfolder, name := pfn[0], pfn[1], pfn[2]

	var as3obj map[string]interface{}
	json.Unmarshal([]byte(as3body), &as3obj)

	if decl, ok := as3obj["declaration"].(map[string]interface{}); ok {
		if parti, ok := decl[partition].(map[string]interface{}); ok {
			if folder, ok := parti[subfolder].(map[string]interface{}); ok {
				if pool, ok := folder[name].(map[string]interface{}); ok {
					if ms, f := pool["members"]; f {
						if msl, ok := ms.([]interface{}); ok && len(msl) > 0 {
							if m0, ok := msl[0].(map[string]interface{}); ok {
								for k, v := range m0 {
									switch k {
									case "servicePort":
										if pvalue, ok := v.(float64); ok {
											port = int(pvalue)
										}
									case "serverAddresses":
									case "connectionLimit":
										if lvalue, ok := v.(float64); ok {
											attrs["connectionLimit"] = lvalue
										}
									default:
										slog.Warnf("unknown member attribute: %s, use it as default", k)
										attrs[k] = v
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return port, attrs
}

func nodePortMembers(ctx context.Context, svc *v1.Service) ([]SvcEpsMember, error) {
	cluster := ctx.Value(Ctx_Key_cluster).(string)
	nodeIPs := []string{}
	members := SvcEpsMembers{}
	for _, nd := range nodes.all() {
		if nd.IpAddr == "" {
			return members, utils.RetryErrorf("node ip %s not found yet", nd.Name)
		}
		nodeIPs = append(nodeIPs, nd.IpAddr)
	}

	pfn, err := keynameAS3Labels(svc.Labels)
	if err != nil {
		return nil, err
	}
	weight := 1
	if w, f := svc.Labels[Label_multicluster_pool_weight]; f {
		var err error
		if weight, err = strconv.Atoi(w); err != nil {
			slog := utils.LogFromContext(ctx)
			slog.Warnf("invalid label set '%s': '%s' for svc %s: %s, default to 1",
				Label_multicluster_pool_weight, w, utils.Keyname(svc.Namespace, svc.Name), err.Error())

		}
	}
	for _, port := range svc.Spec.Ports {
		for _, ip := range nodeIPs {
			members = append(members, SvcEpsMember{
				Cluster:    cluster,
				InPool:     pfn,
				Weight:     weight,
				SvcType:    string(v1.ServiceTypeNodePort),
				SvcKey:     utils.Keyname(svc.Namespace, svc.Name),
				TargetPort: port.TargetPort.IntValue(),
				NodePort:   int(port.NodePort),
				IpAddr:     ip,
			})
		}
	}
	return members, nil
}

func clusterMembers(ctx context.Context, ep *v1.Endpoints) ([]SvcEpsMember, error) {
	cluster := ctx.Value(Ctx_Key_cluster).(string)
	slog := utils.LogFromContext(ctx)
	members := SvcEpsMembers{}

	pfn, err := keynameAS3Labels(ep.Labels)
	if err != nil {
		return nil, err
	}
	weight := 1
	if w, f := ep.Labels[Label_multicluster_pool_weight]; f {
		var err error
		if weight, err = strconv.Atoi(w); err != nil {
			slog := utils.LogFromContext(ctx)
			slog.Warnf("invalid label set '%s': '%s' for svc %s: %s, default to 1",
				Label_multicluster_pool_weight, w, utils.Keyname(ep.Namespace, ep.Name), err.Error())
		}
	}
	for _, subset := range ep.Subsets {
		for _, port := range subset.Ports {
			for _, addr := range subset.Addresses {
				member := SvcEpsMember{
					Cluster:    cluster,
					InPool:     pfn,
					Weight:     weight,
					SvcType:    string(v1.ServiceTypeClusterIP),
					SvcKey:     utils.Keyname(ep.Namespace, ep.Name),
					TargetPort: int(port.Port),
					IpAddr:     addr.IP,
				}
				if addr.NodeName == nil {
					return members, fmt.Errorf("%s node name was not appointed in endpoints", addr.IP)
				}
				if k8no := nodes.get(*addr.NodeName); k8no == nil {
					return members, utils.RetryErrorf("%s not found yet", *addr.NodeName)
				} else if k8no.NetType == "vxlan" {
					if utils.IsIpv6(addr.IP) {
						member.MacAddr = k8no.MacAddrV6
						slog.Debugf("mac addr: %s for ip: %s", k8no.MacAddrV6, addr.IP)
					} else {
						member.MacAddr = k8no.MacAddr
					}
				}
				members = append(members, member)
			}
		}
	}
	return members, nil
}

func formatPoolMembers(ctx context.Context, matchport int, attrs map[string]interface{}, sembs SvcEpsMembers) []interface{} {
	members := []interface{}{}
	filtered := filterMembersWithPort(ctx, matchport, sembs)
	for _, m := range filtered {
		var port int
		var sep string
		if m.SvcType == string(v1.ServiceTypeNodePort) {
			port = m.NodePort
		} else {
			port = m.TargetPort
		}
		if utils.IsIpv6(m.IpAddr) {
			sep = "."
		} else {
			sep = ":"
		}
		member := map[string]interface{}{
			"name":    fmt.Sprintf("%s%s%d", m.IpAddr, sep, port),
			"address": m.IpAddr,
			"svctype": m.SvcType,
		}
		for k, v := range attrs {
			member[k] = v
		}
		members = append(members, member)
	}
	return members
}

// TODO: add ipv6 member test: test pool member and ipv6 bgp or flannel connection.
func formatPoolArps(ctx context.Context, matchport int, attrs map[string]interface{}, sembs SvcEpsMembers) (map[string]string, map[string]string) {
	marps := map[string]string{}
	mndps := map[string]string{}
	filtered := filterMembersWithPort(ctx, matchport, sembs)
	for _, m := range filtered {
		if m.SvcType != string(v1.ServiceTypeClusterIP) {
			continue
		}
		if m.MacAddr == "" {
			continue
		}

		if utils.IsIpv6(m.IpAddr) {
			mndps[m.IpAddr] = m.MacAddr
		} else {
			marps[m.IpAddr] = m.MacAddr
		}
	}
	return marps, mndps
}

func genPoolMemberCmds(ctx context.Context, old, cur *LTMResources, origs, newas map[string]SvcEpsMembers) (map[string]interface{}, map[string]interface{}, error) {
	bc := newBIGIPContext(ctx)

	ocfgs := map[string]interface{}{}
	ncfgs := map[string]interface{}{}

	opools, npools := poolsInCfg(old), poolsInCfg(cur)
	for pfn, sedata := range origs {
		if !utils.Contains(opools, pfn) {
			continue
		}
		// poolcfgs, netcfgs := sedata2Cfgs(ctx, k, cfg, sedata)
		poolcfgs := sedata2PoolCfgs(ctx, pfn, old, sedata)

		for k, v := range poolcfgs {
			if _, f := ocfgs[k]; !f {
				ocfgs[k] = map[string]interface{}{}
			}
			for n, r := range v.(map[string]interface{}) {
				ocfgs[k].(map[string]interface{})[n] = r
			}
		}
	}

	for pfn, sedata := range newas {
		if !utils.Contains(npools, pfn) {
			continue
		}
		// poolcfgs, netcfgs := sedata2Cfgs(ctx, k, cfg, sedata)
		poolcfgs := sedata2PoolCfgs(ctx, pfn, cur, sedata)
		pfn := strings.Split(pfn, "/")
		p, f, n := pfn[0], pfn[1], pfn[2]

		// if the very member exists in both ombs and nmbs, it's an update operation
		// , so, we set the very member properties of ombs to that of nmbs
		ombs, err := bc.Members(n, p, f)
		if err != nil {
			return nil, nil, err
		}
		ombmap := map[string]interface{}{}
		for _, om := range ombs {
			name := om.(map[string]interface{})["name"].(string)
			ombmap[name] = om
		}
		nmbs := poolcfgs[f].(map[string]interface{})["ltm/pool/"+n].(map[string]interface{})["members"].([]interface{})
		for _, nm := range nmbs {
			name := nm.(map[string]interface{})["name"].(string)
			if om, f := ombmap[name]; f {
				session := om.(map[string]interface{})["session"].(string)
				if strings.Index(session, "user-") == 0 {
					nm.(map[string]interface{})["session"] = session
				}
				state := om.(map[string]interface{})["state"].(string)
				if strings.Index(state, "user-") == 0 {
					nm.(map[string]interface{})["state"] = state
				}
			}
		}

		for k, v := range poolcfgs {
			if _, f := ncfgs[k]; !f {
				ncfgs[k] = map[string]interface{}{}
			}
			for n, r := range v.(map[string]interface{}) {
				ncfgs[k].(map[string]interface{})[n] = r
			}
		}
	}
	return ocfgs, ncfgs, nil
}

func genPoolNetCmds(ctx context.Context, old, cur *LTMResources, origs, newas map[string]SvcEpsMembers) (map[string]interface{}, map[string]interface{}, error) {

	ocfgs := map[string]interface{}{}
	ncfgs := map[string]interface{}{}

	for pfn, sedata := range origs {

		netcfgs := sedata2NetCfgs(ctx, pfn, old, sedata)

		for k, v := range netcfgs {
			if _, f := ocfgs[k]; !f {
				ocfgs[k] = map[string]interface{}{}
			}
			for n, r := range v.(map[string]interface{}) {
				ocfgs[k].(map[string]interface{})[n] = r
			}
		}
	}

	for pfn, sedata := range newas {

		netcfgs := sedata2NetCfgs(ctx, pfn, cur, sedata)

		for k, v := range netcfgs {
			if _, f := ncfgs[k]; !f {
				ncfgs[k] = map[string]interface{}{}
			}
			for n, r := range v.(map[string]interface{}) {
				ncfgs[k].(map[string]interface{})[n] = r
			}
		}
	}
	return ocfgs, ncfgs, nil
}

func sedata2PoolCfgs(ctx context.Context, keyname string, cfg *LTMResources, sembs SvcEpsMembers) map[string]interface{} {
	matchPort, attrs := memberPortAndAttrsFromCfg(ctx, keyname, cfg.Raw)
	mbs := formatPoolMembers(ctx, matchPort, attrs, sembs)

	pfn := strings.Split(keyname, "/")
	partition, subfolder, name := pfn[0], pfn[1], pfn[2]

	poolcfgs := generatePoolCfgs(partition, subfolder, name, mbs)

	return poolcfgs
}

func sedata2NetCfgs(ctx context.Context, keyname string, cfg *LTMResources, sembs SvcEpsMembers) map[string]interface{} {
	matchPort, attrs := memberPortAndAttrsFromCfg(ctx, keyname, cfg.Raw)
	marps, mndps := formatPoolArps(ctx, matchPort, attrs, sembs)

	netcfgs := generateNetCfgs(marps, mndps)

	return netcfgs
}

func parseDataGroup(dg map[string]interface{}) ([]byte, error) {
	records, f := dg["records"]
	if !f {
		return nil, fmt.Errorf("no records found in data-group")
	}
	if dg["type"] != "string" {
		return nil, fmt.Errorf("data group type is not string")
	}
	b64bytes := []byte{}
	for _, record := range records.([]interface{}) {
		data := record.(map[string]interface{})["data"].(string)
		b64bytes = append(b64bytes, []byte(data)...)
	}

	return base64.StdEncoding.DecodeString(string(b64bytes))
}

func generateNetCfgs(marps, mndps map[string]string) map[string]interface{} {

	netcfgs := map[string]interface{}{}
	prefix := "k8s-"

	// don't do arp deletion during deploying pool members, because
	// 	we need to confirm the arp deletion is after pod deletion

	for k, v := range marps {
		n := fmt.Sprintf("net/arp/%s%s", prefix, k)
		netcfgs[n] = map[string]interface{}{
			"name":       prefix + k,
			"ipAddress":  k,
			"macAddress": v,
		}
	}

	for k, v := range mndps {
		n := fmt.Sprintf("net/ndp/%s%s", prefix, k)
		netcfgs[n] = map[string]interface{}{
			"name":       prefix + k,
			"ipAddress":  k,
			"macAddress": v,
		}
	}

	return map[string]interface{}{"": netcfgs}

}

func generatePoolCfgs(partition, subfolder, name string, mbs []interface{}) map[string]interface{} {
	pcfgs := map[string]interface{}{}
	ncfgs := map[string]interface{}{}

	poolname := fmt.Sprintf("ltm/pool/%s", name)

	for _, m := range mbs {
		svctype := m.(map[string]interface{})["svctype"].(string)
		if svctype == string(v1.ServiceTypeNodePort) {
			continue
		}
		addr := m.(map[string]interface{})["address"].(string)
		n := fmt.Sprintf("ltm/node/%s", addr)
		ncfgs[n] = map[string]interface{}{
			"name":      addr,
			"partition": partition,
			"address":   addr,
			"monitor":   "default",
			"session":   "user-enabled",
		}
		// for k, v := range attrs {
		// 	node[k] = v
		// }
	}

	pcfgs[poolname] = map[string]interface{}{
		"name":    name,
		"members": mbs,
	}

	return map[string]interface{}{subfolder: pcfgs, "": ncfgs}
}

func newContext(cluster string) context.Context {
	reqid := uuid.New().String()
	slog := utils.NewLog().WithLevel(cmdflags.logLevel).WithRequestID(reqid)
	ctxid := context.WithValue(context.TODO(), utils.CtxKey_RequestID, reqid)
	ctxcs := context.WithValue(ctxid, Ctx_Key_cluster, cluster)
	ctx := context.WithValue(ctxcs, utils.CtxKey_Logger, slog)
	return ctx
}

func newBIGIPContext(ctx context.Context) *f5_bigip.BIGIPContext {
	return &f5_bigip.BIGIPContext{BIGIP: *bip, Context: ctx}
}

func struct2string(r any, skipFields []string) string {
	t := reflect.TypeOf(r)
	v := reflect.ValueOf(r)

	kv := []string{}
	for i := 0; i < t.NumField(); i++ {
		if !utils.Contains(skipFields, t.Field(i).Name) {
			kv = append(kv, fmt.Sprintf("%s: %v", t.Field(i).Name, v.Field(i)))
		}
	}

	return strings.Join(kv, ", ")

}

func (r *DeployRequest) String() string {
	structName := reflect.TypeOf(r).Name()
	body := struct2string(*r, []string{"Context", "ReportIt", "Retries", "NewaRef", "OrigRef", "ObjRefs", "Timestamp", "Relevants"})
	reqID := r.Context.Value(utils.CtxKey_RequestID)
	return fmt.Sprintf("%s {%s, requestID: %s}", structName, body, reqID)
}

func (r *DeployRequest) Merge(rs ...DeployRequest) {
	for _, n := range rs {
		slog := utils.LogFromContext(n.Context)
		slog.Infof("request %s was merged into %s", n.String(), r.Context.Value(utils.CtxKey_RequestID))
		r.NewaRef = n.NewaRef
		r.ObjRefs = append(r.ObjRefs, n.ObjRefs...)
		r.Relevants = append(r.Relevants, &n)
	}
}

// Enum enumulate all DeployRequests in r.Relevants recursively.
func (r *DeployRequest) Enum() []*DeployRequest {
	rlt := []*DeployRequest{r}
	m := map[*DeployRequest]bool{
		r: true,
	}
	for _, relev := range r.Relevants {
		if _, ok := m[relev]; !ok {
			m[relev] = true
			rlt = append(rlt, relev)
			subs := relev.Enum()
			for _, sub := range subs {
				if _, ok := m[sub]; !ok {
					m[sub] = true
					rlt = append(rlt, sub)
				}
			}
		}
	}

	return rlt
}

func syncCoreV1Resources(name string) error {
	defer utils.TimeItToPrometheus()()

	ctx := newContext(name)
	slog := utils.LogFromContext(ctx)
	kubeClient := clusters[name].Clientset
	localCache := clusterCache[name]

	if nList, err := kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{}); err != nil {
		return err
	} else {
		for _, n := range nList.Items {
			slog.Debugf("found node %s", n.Name)
			_packNode(ctx, n.DeepCopy())
		}
	}

	if nsList, err := kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{}); err != nil {
		return nil
	} else {
		for _, ns := range nsList.Items {
			slog.Debugf("found ns: %s", ns.Name)
			localCache.SetNamespace(ns.DeepCopy())
		}
	}

	if epsList, err := kubeClient.CoreV1().Endpoints(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{}); err != nil {
		return err
	} else {
		for _, eps := range epsList.Items {
			if !localCache.NSIncluded(ctx, eps.Namespace) && !cmdflags.hubmode {
				continue
			}
			if svcLabeled(eps.Labels) {
				slog.Debugf("found eps %s", utils.Keyname(eps.Namespace, eps.Name))
				localCache.SetEndpoints(eps.DeepCopy())
			}
		}
	}

	if svcList, err := kubeClient.CoreV1().Services(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{}); err != nil {
		return err
	} else {
		for _, svc := range svcList.Items {
			if !localCache.NSIncluded(ctx, svc.Namespace) && !cmdflags.hubmode {
				continue
			}
			if svcLabeled(svc.Labels) {
				slog.Debugf("found svc %s", utils.Keyname(svc.Namespace, svc.Name))
				localCache.SetService(svc.DeepCopy())
			}
		}
	}

	if cmList, err := kubeClient.CoreV1().ConfigMaps(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{}); err != nil {
		return err
	} else {
		for _, cm := range cmList.Items {
			if !localCache.NSIncluded(ctx, cm.Namespace) {
				continue
			}
			slog.Debugf("found cm %s", utils.Keyname(cm.Namespace, cm.Name))
			localCache.SetConfigMap(cm.DeepCopy())
		}
	}

	return nil
}

func discoverClusters() {
	clusters = map[string]ClusterSet{
		Cluster_main: {
			Clientset:  newKubeClient(cmdflags.kubeConfig),
			Config:     newRestConfig(cmdflags.kubeConfig),
			KubeConfig: cmdflags.kubeConfig,
		},
	}

	if cmdflags.extendedClusters == "" {
		return
	}

	clientset := clusters[Cluster_main].Clientset
	nsn := strings.Split(cmdflags.extendedClusters, "/")
	if len(nsn) != 2 {
		panic(fmt.Errorf("invalid --extended-clusters setting: %s", cmdflags.extendedClusters))
	}
	scrt, err := clientset.CoreV1().Secrets(nsn[0]).Get(context.TODO(), nsn[1], metav1.GetOptions{})
	if err != nil {
		panic(err)
	}

	for k, v := range scrt.Data {
		clusters[k] = ClusterSet{
			KubeConfig: string(v),
			Config:     newRestConfig(string(v)),
			Clientset:  newKubeClient(string(v)),
		}
	}
}

func timeRequest() func(r *DeployRequest) {
	tf := utils.TimeIt(clog)
	return func(r *DeployRequest) {
		tc := tf("")
		collectors.DeployRequestTimeCostCount.WithLabelValues(r.Kind, r.Operation).Inc()
		collectors.DeployRequestTimeCostTotal.WithLabelValues(r.Kind, r.Operation).Add(float64(tc))
	}
}

func loopTask(name string, intervalSeconds *int, f func()) {
	if intervalSeconds == nil || *intervalSeconds <= 0 {
		clog.Errorf("periodic task's interval cannot be 0 or negative, ignored task")
		return
	}
	go func() {
		for {
			select {
			case <-stopCtx.Done():
				clog.Infof("loop task '%s' stopped", name)
				return
			case <-time.After(time.Duration(*intervalSeconds) * time.Second):
				f()
			}
		}
	}()
}

func dgnameOfCfg(partition string) string {
	return "f5_kic_cfgs_" + partition
}

func dgnameOfPSMap(name, partition, subfolder string) string {
	return fmt.Sprintf("f5_kic_psmap_%s-%s-%s", partition, subfolder, name)
}

func dumpDataGroup(name, partition string, bytes []byte) map[string]interface{} {
	records := []interface{}{}

	b64bytes := base64.StdEncoding.EncodeToString(bytes)
	u := 1024
	c := int(len(b64bytes) / u)
	m := int(len(b64bytes) % u)
	for i := 0; i < c; i++ {
		records = append(records, map[string]string{
			"name": fmt.Sprintf("%d", i),
			"data": string(b64bytes[i*u : (i+1)*u]),
		})
	}
	if m > 0 {
		records = append(records, map[string]string{
			"name": fmt.Sprintf("%d", c),
			"data": string(b64bytes[c*u:]),
		})
	}

	return map[string]interface{}{
		"name":      name,
		"type":      "string",
		"partition": partition,
		"records":   records,
	}
}

func genRestCommands(ctx context.Context, partition string, ocfg, ncfg *map[string]interface{}) (*[]f5_bigip.RestRequest, error) {
	defer utils.TimeItToPrometheus()()

	bc := newBIGIPContext(ctx)
	slog := utils.LogFromContext(ctx)
	kinds := f5_bigip.GatherKinds(ocfg, ncfg)

	atomicFunc := func(l *sync.Mutex, f func()) {
		l.Lock()
		defer l.Unlock()
		f()
	}
	wg := sync.WaitGroup{}
	lk := sync.Mutex{}

	errs := []error{}
	existings := map[string]map[string]interface{}{}

	for _, kind := range kinds {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			slog.Tracef("gathering %s resources for partition: %s", kind, partition)
			ek, err := bc.GetExistingResources(partition, []string{kind})
			atomicFunc(&lk, func() {
				errs = append(errs, err)
				if err == nil && ek != nil {
					for k, v := range *ek {
						existings[k] = v
					}
				}
			})
		}(kind)
	}
	wg.Wait()
	slog.Tracef("done of gathering %s resources for partition %s", kinds, partition)

	if err := utils.MergeErrors(errs); err != nil {
		return &[]f5_bigip.RestRequest{}, err
	} else {
		return bc.GenRestRequests(partition, ocfg, ncfg, &existings)
	}
}

// sembKey return the identifer of a SvcEpsMember
// The calculation method is the full path of SvcEpsMember:
//
//	Cluster -> IpAddr -> ports(combines TargetPort and NodePort)
func sembKey(m SvcEpsMember) string {
	return strings.Join([]string{
		m.Cluster,
		m.IpAddr,
		fmt.Sprintf("%d", m.TargetPort),
		fmt.Sprintf("%d", m.NodePort),
	}, "/")
}

func manipulateRestObjects(restObjs map[string]interface{}) error {

	poolMap := map[string][]string{}
	ruleMap := map[string]string{}

	for partition, folders := range restObjs {
		cfg := LTMResources{
			Partition: partition,
			Json:      folders.(map[string]interface{}),
		}
		virtuals := virtualsInCfg(&cfg)
		for _, virtual := range virtuals {
			pfn := pfnsWithKind(virtual, "ltm/virtual")
			rules := jsonGet(restObjs, append(pfn, "rules"))
			if rules == nil {
				err := jsonSet(restObjs, append(pfn, "rules"), []interface{}{})
				if err != nil {
					return err
				}
			}
		}
	}

	if len(clusters) == 1 {
		return nil
	}

	for partition, folders := range restObjs {
		cfg := LTMResources{
			Partition: partition,
			Json:      folders.(map[string]interface{}),
		}
		opools := poolsInCfg(&cfg)

		// add pool names for each cluster
		for _, opool := range opools {
			opobj := jsonGet(restObjs, pfnsWithKind(opool, "ltm/pool"))
			for cluster := range clusters {
				npool := utils.Keyname(pfnWithCluster(opool, cluster)...)
				npobj, err := utils.DeepCopy(opobj)
				if err != nil {
					return err
				}
				npobj.(map[string]interface{})["name"] = strings.Split(npool, "/")[2]
				err = jsonSet(restObjs, pfnsWithKind(npool, "ltm/pool"), npobj)
				if err != nil {
					return err
				}
				if _, f := poolMap[opool]; !f {
					poolMap[opool] = []string{}
				}
				poolMap[opool] = append(poolMap[opool], npool)
			}
		}

	}

	// add irule
	for opool, npools := range poolMap {
		poolWeights := []string{}
		for _, npool := range npools {
			pfn := strings.Split(npool, "/")
			mbs := psmap.get(pfn[2], pfn[0], pfn[1])
			if len(mbs) > 0 {
				poolWeights = append(poolWeights, fmt.Sprintf("/%s %d", npool, mbs[0].Weight))
			} else {
				poolWeights = append(poolWeights, fmt.Sprintf("/%s %d", npool, 0))
			}
		}
		// TODO: check virtual type to determine the EVENT string.
		ruleContent := fmt.Sprintf(`
when RULE_INIT {

	array unset weights *
	array unset static::pools *
	set index 0

	# for pool %s
	array set weights { 
		%s 
	}
	foreach name [array names weights] {
		for { set i 0 }  { $i < $weights($name) }  { incr i } {
			set static::pools($index) $name
			incr index
		}
	}
	set static::pools_size [array size static::pools]
}

when HTTP_REQUEST {
	if { $static::pools_size != 0 }{
		set pool $static::pools([expr {int(rand()*$static::pools_size)}])
		pool $pool
	}
}
				`, opool, strings.Join(poolWeights, "\n		"))

		opfn := strings.Split(opool, "/")
		ruleName := "traffic-rule-with-weights-" + opfn[2]

		err := jsonSet(restObjs, []string{opfn[0], opfn[1], "ltm/rule/" + ruleName}, map[string]interface{}{
			"apiAnonymous": ruleContent,
			"name":         ruleName,
		})
		if err != nil {
			return err
		}

		ruleMap[opool] = "/" + utils.Keyname(opfn[0], opfn[1], ruleName)
	}

	for partition, folders := range restObjs {
		// change virtual 'irule' key to add the rule

		cfg := LTMResources{
			Partition: partition,
			Json:      folders.(map[string]interface{}),
		}
		virtuals := virtualsInCfg(&cfg)

		// set all virtuals
		for _, virtual := range virtuals {
			vpfn := pfnsWithKind(virtual, "ltm/virtual")
			vobj := jsonGet(restObjs, vpfn)
			if vobj == nil {
				return fmt.Errorf("failed to get virtual object from key '%s'", virtual)
			}
			if poolName, f1 := vobj.(map[string]interface{})["pool"]; f1 {
				pool := utils.Keyname(vpfn[0], vpfn[1], poolName.(string))
				if strings.HasPrefix(poolName.(string), "/") {
					pool = poolName.(string)
				}
				rrefs := jsonGet(restObjs, append(vpfn, "rules"))
				if rule, f2 := ruleMap[pool]; f2 {
					if rrefs == nil {
						rrefs = []string{rule}
					} else {
						rrefs = append(rrefs.([]interface{}), rule)
					}
					if err := jsonSet(restObjs, append(vpfn, "rules"), rrefs); err != nil {
						return err
					}
					if err := jsonSet(restObjs, append(vpfn, "pool"), ""); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func keynameAS3Labels(labels map[string]string) (string, error) {
	partition, f1 := labels[Label_Tenant]
	subfolder, f2 := labels[Label_App]
	name, f3 := labels[Label_Pool]
	if !f1 || !f2 || !f3 {
		return "", fmt.Errorf("missing as3 labels check: %s", labels)
	}
	return utils.Keyname(partition, subfolder, name), nil
}

func jsonGet(jsonbody map[string]interface{}, pathto []string) interface{} {
	if len(pathto) == 0 {
		return nil
	}
	k := pathto[0]
	if len(pathto) == 1 {
		if _, f := jsonbody[k]; f {
			return jsonbody[k]
		} else {
			return nil
		}
	}
	subtype := reflect.TypeOf(jsonbody[k]).Kind().String()
	if subtype == "map" {
		return jsonGet(jsonbody[pathto[0]].(map[string]interface{}), pathto[1:])
	} else {
		return nil
	}
}

func jsonSet(jsonbody map[string]interface{}, pathto []string, value interface{}) error {
	if len(pathto) == 0 {
		return fmt.Errorf("invalid path: <empty>")
	}
	k := pathto[0]
	if len(pathto) == 1 {
		jsonbody[k] = value
		return nil
	}

	subtype := reflect.TypeOf(jsonbody[k]).Kind().String()
	if subtype == "map" {
		return jsonSet(jsonbody[pathto[0]].(map[string]interface{}), pathto[1:], value)
	} else {
		return fmt.Errorf("invalid kind for m[%s], actual type: %s", k, subtype)
	}
}

func pfnsWithKind(kn string, kind string) []string {
	pfn := strings.Split(kn, "/")
	return []string{pfn[0], pfn[1], utils.Keyname(kind, pfn[2])}
}

func pfnWithCluster(pool string, cluster string) []string {
	pfn := strings.Split(pool, "/")
	if cluster == Cluster_main {
		return pfn
	} else {
		return []string{pfn[0], pfn[1], pfn[2] + "-" + cluster}
	}
}

// mergeCfgs merge items from n to cfg, by property to property
func mergeCfgs(cfg, n map[string]interface{}) {
	for k, v := range n {
		if _, f := cfg[k]; !f {
			cfg[k] = map[string]interface{}{}
		}
		for r, res := range v.(map[string]interface{}) {
			if _, f := cfg[k].(map[string]interface{})[r]; !f {
				cfg[k].(map[string]interface{})[r] = map[string]interface{}{}
			}
			for p, pv := range res.(map[string]interface{}) {
				cfg[k].(map[string]interface{})[r].(map[string]interface{})[p] = pv
			}
		}
	}
}

func nilLoop() {
	for {
		select {
		case <-stopCtx.Done():
			clog.Infof("saving cfgs and psmap to bigip...")
			saveCfgsPSMap(DeployRequest{
				Operation: Operation_save,
				Kind:      Kind_config_dg,
				Context:   newContext(Cluster_main),
			})

			os.Exit(0)
		case <-time.After(1 * time.Second):
			// do nothing
		}
	}
}
