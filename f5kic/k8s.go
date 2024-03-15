package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/f5devcentral/f5-bigip-rest-go/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (ns *Nodes) set(n *K8Node) {
	ns.mutex <- true
	defer func() { <-ns.mutex }()
	ns.Items[n.Name] = n
}

func (ns *Nodes) unset(name string) {
	ns.mutex <- true
	defer func() { <-ns.mutex }()

	delete(ns.Items, name)
}

func (ns *Nodes) get(name string) *K8Node {
	ns.mutex <- true
	defer func() { <-ns.mutex }()
	if n, f := nodes.Items[name]; f {
		return n
	} else {
		return nil
	}
}

func (ns *Nodes) all() map[string]K8Node {
	ns.mutex <- true
	defer func() { <-ns.mutex }()

	rlt := map[string]K8Node{}
	for k, n := range ns.Items {
		rlt[k] = *n
	}
	return rlt
}

func (c *Configs) set(partition string, cfg LTMResources) {
	c.mutex <- true
	defer func() { <-c.mutex }()

	cfg.Partition = partition
	c.Items[partition] = cfg
}

func (c *Configs) unset(partition string) {
	c.mutex <- true
	defer func() { <-c.mutex }()

	delete(c.Items, partition)
}

func (c *Configs) keysBy(cmkey string) []string {
	c.mutex <- true
	defer func() { <-c.mutex }()

	ks := []string{}
	for k, res := range c.Items {
		if res.Cmkey == cmkey {
			ks = append(ks, k)
		}
	}
	return ks
}

func (c *Configs) allKeys() []string {
	c.mutex <- true
	defer func() { <-c.mutex }()

	ks := []string{}
	for k := range c.Items {
		ks = append(ks, k)
	}
	return ks
}

func (c *Configs) get(partition string) *LTMResources {
	c.mutex <- true
	defer func() { <-c.mutex }()

	if cfg, f := c.Items[partition]; f {
		return &cfg
	} else {
		return nil
	}
}

// set saves SvcEpsMembers into PSMap
// This function replaces the SvcEpsMembers of ".Cluster == cluster" with sembs
// if len(sembs) == 0, the function behaves as .unset(name, partition, subfolder, cluster)
// if cluster == "", no deletion happens, members in the sembs are appended or merged.
// if "Cluster IpAddr ports" matches, replace it, otherwise, append it.
// The orders of SvcEpsMembers are kept to not change as possible.
func (ps *PSMap) set(name, partition, subfolder string, cluster string, sembs SvcEpsMembers) {
	ps.mutex <- true
	defer func() { <-ps.mutex }()

	mbsmap := map[string]SvcEpsMember{}
	for _, m := range sembs {
		mbsmap[sembKey(m)] = m
	}
	newa := SvcEpsMembers{}
	pfn := utils.Keyname(partition, subfolder, name)

	if _, f := ps.Items[pfn]; !f {
		ps.Items[pfn] = sembs
		return
	}

	for _, o := range ps.Items[pfn] {
		if n, f := mbsmap[sembKey(o)]; f {
			newa = append(newa, n)
			delete(mbsmap, sembKey(o))
			continue
		}
		if o.Cluster != cluster {
			newa = append(newa, o)
		}
	}
	for _, n := range sembs {
		if _, f := mbsmap[sembKey(n)]; f {
			newa = append(newa, n)
		}
	}

	ps.Items[pfn] = newa
}

// unset removes all SvcEpsMembers with Cluster == cluster
// the pfn key would be removed if len(PSMap.Items[pfn]) == 0
func (ps *PSMap) unset(name, partition, subfolder string, cluster string) {
	ps.mutex <- true
	defer func() { <-ps.mutex }()
	// delete(ps.Items, utils.Keyname(partition, subfolder, name))
	newa := SvcEpsMembers{}
	pfn := utils.Keyname(partition, subfolder, name)
	if v, f := ps.Items[pfn]; f {
		for _, m := range v {
			if m.Cluster != cluster {
				newa = append(newa, m)
			}
		}
	}
	if len(newa) > 0 {
		ps.Items[pfn] = newa
	} else {
		delete(ps.Items, pfn)
	}
}

func (ps *PSMap) get(name, partition, subfolder string) SvcEpsMembers {
	ps.mutex <- true
	defer func() { <-ps.mutex }()

	pfn := utils.Keyname(partition, subfolder, name)
	rlt := SvcEpsMembers{}
	if svc, f := ps.Items[pfn]; f {
		rlt = append(rlt, svc...)
	}

	return rlt
}

// getByPartition return the map of SvcEpsMembers with pfn as the key
func (ps *PSMap) getByPartition(partition string) map[string]SvcEpsMembers {
	ps.mutex <- true
	defer func() { <-ps.mutex }()

	rlt := map[string]SvcEpsMembers{}
	for k, v := range ps.Items {
		if strings.Index(k, partition+"/") == 0 {
			rlt[k] = v
		}
	}
	return rlt
}

func (ps *PSMap) dumps() map[string]interface{} {
	ps.mutex <- true
	defer func() { <-ps.mutex }()

	rlt := map[string]interface{}{}

	for k, v := range ps.Items {
		// sedata, _ := utils.MarshalJson(v) // SvcEpsMembers are not Json
		// sedata, _ := json.Marshal(v)  // base64 return..
		members := []map[string]interface{}{}
		for _, semb := range v {
			mb, _ := utils.MarshalJson(semb)
			members = append(members, mb)
		}

		rlt[k] = members
	}

	return rlt
}

func (ps *PSMap) pfnBySvcKey(cluster, svckey string) *string {
	ps.mutex <- true
	defer func() { <-ps.mutex }()

	for k, item := range ps.Items {
		for _, m := range item {
			if cluster == m.Cluster && m.SvcKey == svckey {
				return &k
			}
		}
	}
	return nil
}

func (ps *PSMap) allKeys() []string {
	ps.mutex <- true
	defer func() { <-ps.mutex }()

	rlt := []string{}
	for k := range ps.Items {
		rlt = append(rlt, k)
	}
	return rlt
}

func (ps *PSMap) memberLinks(ip string) []string {
	ps.mutex <- true
	defer func() { <-ps.mutex }()

	rlt := []string{}
	for kn, members := range ps.Items {
		for _, m := range members {
			if m.IpAddr == ip {
				port := 0
				if m.SvcType == string(v1.ServiceTypeNodePort) {
					port = m.NodePort
				} else {
					port = m.TargetPort
				}
				pfn := strings.Split(kn, "/")
				ipport := fmt.Sprintf("~%s~%s:%d", pfn[0], ip, port)
				if utils.IsIpv6(ip) {
					ipport = fmt.Sprintf("~%s~%s.%d", pfn[1], ip, port)
				}
				link := fmt.Sprintf("/mgmt/tm/ltm/pool/%s/members/%s", utils.Refname(pfn[0], pfn[1], pfn[2]), ipport)
				rlt = append(rlt, link)
			}
		}
	}
	return utils.Unified(rlt)
}

func (ms *SvcEpsMembers) getSvcKeyOf(cluster string) string {
	for _, m := range *ms {
		if m.Cluster == cluster {
			return m.SvcKey
		}
	}
	return ""
}

func (c *KicCache) SetNamespace(obj *v1.Namespace) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if obj != nil {
		c.Namespace[obj.Name] = obj
	}
}

func (c *KicCache) UnsetNamespace(keyname string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.Namespace, keyname)
}

func (c *KicCache) GetNamespace(keyname string) *v1.Namespace {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.Namespace[keyname]
}

func (c *KicCache) NSIncluded(ctx context.Context, name string) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	slog := utils.LogFromContext(ctx)
	if len(cmdflags.namespaces) == 0 && cmdflags.namespaceLabel == "" {
		return true
	} else {
		ns := c.Namespace[name]
		if ns == nil {
			slog.Errorf("namespace %s not found", name)
			return false
		}

		if utils.Contains(cmdflags.namespaces, name) {
			return true
		}
		if cmdflags.namespaceLabel != "" {
			requiredLabels, err := labels.Parse(cmdflags.namespaceLabel)
			if err != nil {
				slog.Errorf("failed to parse label %s, labelSelector was set to Nothing", cmdflags.namespaceLabel)
				requiredLabels = labels.Nothing()
			}
			return requiredLabels.Matches(labels.Set(ns.Labels))
		}
	}

	return false
}

func (c *KicCache) GetEndpoints(keyname string) *v1.Endpoints {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.Endpoints[keyname]
}

func (c *KicCache) GetEndpointsWithLabels(labels map[string]string) []*v1.Endpoints {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	rlt := []*v1.Endpoints{}
	for _, ep := range c.Endpoints {
		if svcLabeled(ep.Labels) && reflect.DeepEqual(ep.Labels, labels) {
			rlt = append(rlt, ep)
		}
	}
	return rlt
}

func (c *KicCache) SetEndpoints(eps *v1.Endpoints) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if eps != nil {
		c.Endpoints[utils.Keyname(eps.Namespace, eps.Name)] = eps
	}
}

func (c *KicCache) UnsetEndpoints(keyname string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.Endpoints, keyname)
}

func (c *KicCache) GetConfigMap(keyname string) *v1.ConfigMap {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.ConfigMap[keyname]
}

func (c *KicCache) SetConfigMap(cm *v1.ConfigMap) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if cm != nil {
		c.ConfigMap[utils.Keyname(cm.Namespace, cm.Name)] = cm
	}
}

func (c *KicCache) UnsetConfigMap(keyname string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.ConfigMap, keyname)
}

func (c *KicCache) GetService(keyname string) *v1.Service {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.Service[keyname]
}

func (c *KicCache) SetService(svc *v1.Service) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if svc != nil {
		c.Service[utils.Keyname(svc.Namespace, svc.Name)] = svc
	}
}
func (c *KicCache) UnsetService(keyname string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.Service, keyname)
}

func (c *KicCache) All() map[string]interface{} {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	bc, _ := json.Marshal(c)
	var rlt map[string]interface{}
	json.Unmarshal(bc, &rlt)
	return rlt
}

func packConfigMap(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx, cm := r.Context, r.NewaRef.(*v1.ConfigMap)
	if cm == nil {
		return fmt.Errorf("invalid configmap reference: nil")
	}
	if _, found := cm.Data["template"]; !found {
		return fmt.Errorf("invalid configmap, missing template")
	}
	template := cm.Data["template"]
	cmKey := utils.Keyname(cm.Namespace, cm.Name)

	return _packConfigmap(ctx, template, cmKey)
}

func _packConfigmap(ctx context.Context, template, cmKey string) error {
	slog := utils.LogFromContext(ctx)
	var restjson map[string]interface{}
	if err := as3ToRestJson(ctx, template, &restjson); err != nil {
		return err
	}

	origpf := cfgs.keysBy(cmKey)
	newapf := []string{}
	newas := map[string]LTMResources{}

	for partition, subfolders := range restjson {
		// business no replacement:
		// configmap association: the very partition cannot be replaced by the declaration in other configmap
		if old := cfgs.get(partition); old != nil && old.Cmkey != "" && old.Cmkey != cmKey {
			msg := fmt.Sprintf("partition %s was defined in %s, cannot be redefined by %s",
				partition, old.Cmkey, cmKey)
			slog.Warnf(msg)
			continue
		}
		newapf = append(newapf, partition)
		newas[partition] = LTMResources{
			Cmkey:     cmKey,
			Raw:       template,
			Json:      subfolders.(map[string]interface{}),
			Partition: partition,
		}
	}

	c, d, u := utils.Diff(origpf, newapf)

	for _, pt := range d {
		orig := cfgs.get(pt)
		cfgs.unset(pt)
		sendDeployRequest(DeployRequest{
			Operation: Operation_delete,
			Kind:      Kind_ltm_virtual,
			Partition: pt,
			Name:      cmKey,
			OrigRef:   orig,
			ObjRefs:   []interface{}{psmap.getByPartition(pt)},
			Context:   ctx,
			// ReportIt:  true,
		})
	}

	for _, pt := range c {
		cfgs.set(pt, newas[pt])
		cur := newas[pt]
		sendDeployRequest(DeployRequest{
			Operation: Operation_deploy,
			Kind:      Kind_ltm_virtual,
			Partition: pt,
			Name:      cmKey,
			Context:   ctx,
			OrigRef:   nil,
			NewaRef:   &cur,
			ObjRefs:   []interface{}{psmap.getByPartition(pt)},
		})
	}

	for _, pt := range u {

		cur, old := newas[pt], cfgs.get(pt)
		cfgs.set(pt, cur)

		if !reflect.DeepEqual(old.Raw, cur.Raw) ||
			!utils.DeepEqual(old.Json, cur.Json) {
			sendDeployRequest(DeployRequest{
				Operation: Operation_deploy,
				Kind:      Kind_ltm_virtual,
				Partition: pt,
				Name:      cmKey,
				Context:   ctx,
				OrigRef:   old,
				NewaRef:   &cur,
				ObjRefs:   []interface{}{psmap.getByPartition(pt)},
			})
		} else {
			slog.Warnf("no changes to configmap for partition: %s", pt)
		}
	}

	return nil
}

func unpackConfigMap(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx, cm := r.Context, r.OrigRef.(*v1.ConfigMap)
	slog := utils.LogFromContext(ctx)

	cfgkey := utils.Keyname(cm.Namespace, cm.Name)
	ks := cfgs.keysBy(cfgkey)
	slog.Debugf("all partitions for deletion: %s", ks)

	for _, k := range ks {
		orig := cfgs.get(k)
		cfgs.unset(k)
		sendDeployRequest(DeployRequest{
			Operation: Operation_delete,
			Kind:      Kind_ltm_virtual,
			Partition: k,
			Name:      cfgkey,
			OrigRef:   orig,
			ObjRefs:   []interface{}{psmap.getByPartition(k)},
			Context:   ctx,
			// ReportIt:  true,
		})
	}

	return nil
}

func packService(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx := r.Context
	cur := r.NewaRef.(*v1.Service)
	slog := utils.LogFromContext(ctx)
	localCache := clusterCache[ctx.Value(Ctx_Key_cluster).(string)]

	// 	when ClusterIP is changed to NodePort, or reverse, there's no endpoints update event, thus,
	//	the pool members would not be updated to <node ip:port> or <endpoints ip:port> correspondingly.
	// only call _packEndpoints if the svcType change
	if r.OrigRef == nil || r.OrigRef.(*v1.Service).Spec.Type == cur.Spec.Type {
		slog.Debugf("no action for packing service, no svcType changes: %s",
			utils.Keyname(cur.Namespace, cur.Name))
		return nil
	}

	nsvcKey := utils.Keyname(cur.Namespace, cur.Name)
	neps := localCache.GetEndpoints(nsvcKey)
	if neps == nil {
		slog.Warnf("endpoints %s not found", nsvcKey)
		return nil
	}
	if svcLabeled(neps.Labels) {
		return _packEndpoints(ctx, neps)
	} else {
		slog.Warnf("endpoints %s missing labels, endpoints update event will fix it.", nsvcKey)
		return nil
	}
}

func unpackService(r DeployRequest) error {
	// ctx, svc := r.Context, r.ObjRef.(*v1.Service)
	return nil
}

func packEndpoints(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()
	ctx, neps := r.Context, r.NewaRef.(*v1.Endpoints)
	cluster := ctx.Value(Ctx_Key_cluster).(string)

	// if label weight changes...
	if len(clusters) > 1 {
		f1, f2 := false, false
		ov, nv := "", ""
		if r.OrigRef != nil {
			oeps := r.OrigRef.(*v1.Endpoints)
			ov, f1 = oeps.Labels[Label_multicluster_pool_weight]
		}

		nv, f2 = neps.Labels[Label_multicluster_pool_weight]
		pool, err := keynameAS3Labels(neps.Labels)
		if err != nil {
			// TODO: check all "return err": should have more info before return.
			return err
		}
		pfn := pfnWithCluster(pool, cluster)
		orig := psmap.get(pfn[2], pfn[0], pfn[1])
		if r.OrigRef == nil /*new endpoints created*/ ||
			len(orig) == 0 /*origin psmap is empty*/ ||
			f1 == !f2 /*label existance changes*/ ||
			(ov != nv) /*label value changes*/ {
			if cfg := cfgs.get(neps.Labels[Label_Tenant]); cfg != nil {
				if cm := clusterCache[Cluster_main].GetConfigMap(cfg.Cmkey); cm != nil {
					sendDeployRequest(DeployRequest{
						Operation: Operation_pack,
						Kind:      Kind_configmap,
						Name:      cfg.Cmkey,
						NewaRef:   cm,
						Context:   ctx,
					})
				}
			}
		}
	}

	return _packEndpoints(ctx, neps)
}

func _packEndpoints(ctx context.Context, cur *v1.Endpoints) error {
	slog := utils.LogFromContext(ctx)
	nsvcKey := utils.Keyname(cur.Namespace, cur.Name)
	labels := cur.GetObjectMeta().GetLabels()
	cluster := ctx.Value(Ctx_Key_cluster).(string)
	localCache := clusterCache[cluster]

	partition, f1 := labels[Label_Tenant]
	subfolder, f2 := labels[Label_App]
	name, f3 := labels[Label_Pool]
	if !f1 || !f2 || !f3 {
		return fmt.Errorf("ignore endpoints %s because of missing labels", nsvcKey)
	}
	if len(clusters) > 1 && cluster != Cluster_main {
		name = name + "-" + cluster
	}

	// business no replacement:
	// Service association cannot be replaced by other services with the same labels
	orig := psmap.get(name, partition, subfolder)
	// if orig != nil && orig.SvcKey != nsvcKey {
	if orig != nil {
		osvcKey := orig.getSvcKeyOf(cluster)
		if osvcKey != "" && osvcKey != nsvcKey {
			msg := fmt.Sprintf("Service %s was being associated with %s, service %s cannot be assigned",
				osvcKey, utils.Keyname(partition, subfolder, name), nsvcKey)
			return fmt.Errorf(msg)
		}
	}

	origs := psmap.getByPartition(partition)
	pfn := psmap.pfnBySvcKey(cluster, nsvcKey)
	if pfn != nil && *pfn != utils.Keyname(partition, subfolder, name) { // labels are changed
		slog.Debugf("pool mapping changed: %s %s", *pfn, utils.Keyname(partition, subfolder, name))
		arr := strings.Split(*pfn, "/")
		p, f, n := arr[0], arr[1], arr[2]
		// orig := psmap.get(n, p, f)
		origs := psmap.getByPartition(p)
		psmap.unset(n, p, f, cluster)
		// newa := psmap.get(n, p, f)
		newas := psmap.getByPartition(p)
		sendDeployRequest(DeployRequest{
			Operation: Operation_delete,
			Kind:      Kind_ltm_pool,
			Name:      n,
			Partition: p,
			Subfolder: f,
			OrigRef:   origs,
			NewaRef:   newas,
			ObjRefs:   []interface{}{cfgs.get(p)},
			Context:   ctx,
			// ReportIt:  true,
		})
	}

	svc := localCache.GetService(nsvcKey)
	if svc == nil {
		slog.Warnf("not found related v1.service, quit..: %s", nsvcKey)
		return nil
	}
	svctype := string(svc.Spec.Type)
	if cmdflags.poolMemberType == "cluster" {
		svctype = "ClusterIP"
	}
	var members SvcEpsMembers
	var err error

	switch svctype {
	case string(v1.ServiceTypeNodePort):
		if members, err = nodePortMembers(ctx, svc); err != nil {
			return err
		}
	case string(v1.ServiceTypeClusterIP):
		if members, err = clusterMembers(ctx, cur); err != nil {
			return err
		}
	case string(v1.ServiceTypeLoadBalancer):
		return fmt.Errorf("not supported service type: %s", svctype)
	case string(v1.ServiceTypeExternalName):
		return fmt.Errorf("not supported service type: %s", svctype)
	default:
		return fmt.Errorf("unknown service type: %s", svctype)
	}

	psmap.set(name, partition, subfolder, cluster, members)
	newas := psmap.getByPartition(partition)

	cfg := cfgs.get(partition)
	if cfg == nil || !utils.Contains(poolsInCfg(cfg), utils.Keyname(partition, subfolder, name)) {
		slog.Warnf("there is no reference to the pool %s", utils.Keyname(partition, subfolder, name))
		return nil
	}

	epns := strings.Split(nsvcKey, "/")[0]
	cmns := strings.Split(cfg.Cmkey, "/")[0]
	if cmns != epns && !cmdflags.hubmode {
		return fmt.Errorf("while packendpoint, with hubmode '%v', ignore the service association of cm %s <-> ep %s", cmdflags.hubmode, cmns, epns)
	}

	sendDeployRequest(DeployRequest{
		Operation: Operation_deploy,
		Kind:      Kind_ltm_pool,
		Partition: partition,
		Subfolder: subfolder,
		Name:      name,
		Context:   ctx,
		OrigRef:   origs,
		NewaRef:   newas,
	})

	return nil
}

func unpackEndpoints(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx, ep := r.Context, r.OrigRef.(*v1.Endpoints)
	slog := utils.LogFromContext(ctx)
	cluster := ctx.Value(Ctx_Key_cluster).(string)

	labels := ep.GetObjectMeta().GetLabels()
	if !svcLabeled(labels) {
		return fmt.Errorf("endpoint labels not matched: %v", labels)
	}
	partition, subfolder, name := labels[Label_Tenant], labels[Label_App], labels[Label_Pool]
	if len(clusters) > 1 && cluster != Cluster_main {
		name = name + "-" + cluster
	}
	if len(clusters) > 1 {
		if cfg := cfgs.get(ep.Labels[Label_Tenant]); cfg != nil {
			cm := clusterCache[Cluster_main].GetConfigMap(cfg.Cmkey)
			sendDeployRequest(DeployRequest{
				Operation: Operation_pack,
				Kind:      Kind_configmap,
				Name:      cfg.Cmkey,
				NewaRef:   cm,
				Context:   ctx,
			})
		}
	}

	// business no replacement:
	// If the service association is invalid(not setup before), skip ltm member deletion caused by service deletion
	nsvcKey := utils.Keyname(ep.Namespace, ep.Name)
	orig := psmap.get(name, partition, subfolder)
	osvcKey := orig.getSvcKeyOf(cluster)
	if osvcKey != "" && osvcKey != nsvcKey {
		msg := fmt.Sprintf("Service %s was being associated with %s, service %s cannot be assigned",
			osvcKey, utils.Keyname(partition, subfolder, name), nsvcKey)
		return fmt.Errorf(msg)
	}

	slog.Infof("remove pool members because of association: %#v", labels)
	origs := psmap.getByPartition(partition)
	psmap.unset(name, partition, subfolder, cluster)
	// newa := psmap.get(name, partition, subfolder)
	newas := psmap.getByPartition(partition)
	sendDeployRequest(DeployRequest{
		Operation: Operation_delete,
		Kind:      Kind_ltm_pool,
		Name:      name,
		Partition: partition,
		Subfolder: subfolder,
		OrigRef:   origs,
		NewaRef:   newas,
		ObjRefs:   []interface{}{cfgs.get(partition)},
		Context:   ctx,
		// ReportIt:  true,
	})
	return nil
}

func packNode(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx := r.Context
	nodes := map[string]*v1.Node{}

	drs := r.Enum()
	for _, r := range drs {
		if r.NewaRef != nil {
			n, ok := r.NewaRef.(*v1.Node)
			if ok {
				nodes[n.Name] = n
			}
		}
	}
	errs := []error{}
	for _, n := range nodes {
		err := _packNode(ctx, n)
		errs = append(errs, err)
	}

	return utils.MergeErrors(errs)
}

func _packNode(ctx context.Context, n *v1.Node) error {
	slog := utils.LogFromContext(ctx)

	for _, taint := range n.Spec.Taints {
		if taint.Key == "node.kubernetes.io/unreachable" && taint.Effect == "NoSchedule" {
			nodes.unset(n.Name)
			return nil
		}
	}

	node := K8Node{
		Name: n.Name,
	}

	cnitype := detectCNIType(n)
	switch cnitype {
	case "flannel":
		if _, ok := n.Annotations["flannel.alpha.coreos.com/backend-data"]; ok && n.Annotations["flannel.alpha.coreos.com/backend-data"] != "null" {
			macStr := n.Annotations["flannel.alpha.coreos.com/backend-data"]
			var v map[string]interface{}
			err := json.Unmarshal([]byte(macStr), &v)
			if err != nil {
				slog.Errorf("failed to unmarshal v4 macStr %s: %s", macStr, err.Error())
				return err
			}

			node.IpAddr = n.Annotations["flannel.alpha.coreos.com/public-ip"]
			node.MacAddr = v["VtepMAC"].(string)
			node.NetType = n.Annotations["flannel.alpha.coreos.com/backend-type"]
		}

		if _, ok := n.Annotations["flannel.alpha.coreos.com/backend-v6-data"]; ok && n.Annotations["flannel.alpha.coreos.com/backend-v6-data"] != "null" {
			macStr := n.Annotations["flannel.alpha.coreos.com/backend-v6-data"]
			var v map[string]interface{}
			err := json.Unmarshal([]byte(macStr), &v)
			if err != nil {
				slog.Errorf("failed to unmarshal v6 macStr %s: %s", macStr, err.Error())
				return err
			}

			node.NetType = n.Annotations["flannel.alpha.coreos.com/backend-type"]
			node.IpAddrV6 = n.Annotations["flannel.alpha.coreos.com/public-ipv6"]
			node.MacAddrV6 = v["VtepMAC"].(string)

		}
		if _, ok := n.Annotations["flannel.alpha.coreos.com/backend-type"]; ok {
			backendType := n.Annotations["flannel.alpha.coreos.com/backend-type"]
			if backendType == "host-gw" {
				node.NetType = "host-gw"
				node.MacAddr = ""
				if _, ok := n.Annotations["flannel.alpha.coreos.com/public-ip"]; ok {
					node.IpAddr = n.Annotations["flannel.alpha.coreos.com/public-ip"]
				}

				if _, ok := n.Annotations["flannel.alpha.coreos.com/public-ipv6"]; ok {
					node.IpAddrV6 = n.Annotations["flannel.alpha.coreos.com/public-ipv6"]
				}

				if len(n.Spec.PodCIDRs) > 0 {
					podCidrs := n.Spec.PodCIDRs
					for _, each := range podCidrs {
						if strings.Contains(each, ":") {
							node.Dest6 = each
						} else {
							node.Dest = each
						}
					}
				}
			}
		}
	case "calico":
		// TODO: ipv6?
		if _, ok := n.Annotations["projectcalico.org/IPv4Address"]; ok {
			ipmask := n.Annotations["projectcalico.org/IPv4Address"]
			ipaddr := strings.Split(ipmask, "/")[0]
			node.IpAddr = ipaddr
			node.NetType = "calico-underlay"
			node.MacAddr = ""
		}

	case "cilium":
		addrs := n.Status.Addresses
		ipaddr := ""
		for _, addr := range addrs {
			if addr.Type == v1.NodeInternalIP {
				ipaddr = addr.Address
				break
			}
		}
		node.Name = n.Name
		node.IpAddr = ipaddr
		node.NetType = ""
		node.MacAddr = ipv4ToMac(ipaddr)

	default:
		slog.Errorf("unknown cni type: %s", cnitype)
	}

	o := nodes.get(n.Name)
	nodes.set(&node)

	if o == nil || !reflect.DeepEqual(*o, node) {
		if len(node.MacAddr) > 0 && len(node.IpAddr) > 0 && len(cmdflags.flannelName) > 0 ||
			len(node.MacAddrV6) > 0 && len(node.IpAddrV6) > 0 && len(cmdflags.flannelNameV6) > 0 {
			sendDeployRequest(DeployRequest{
				Operation: Operation_deploy,
				Kind:      Kind_net_fdb,
				Partition: "Common",
				Name:      node.Name,
				Context:   ctx,
				NewaRef:   &node,
			})
		}

		if len(node.IpAddr) > 0 && len(node.Dest) > 0 ||
			len(node.IpAddrV6) > 0 && len(node.Dest6) > 0 {
			sendDeployRequest(DeployRequest{
				Kind:      Kind_net_route,
				Operation: Operation_deploy,
				Partition: "Common",
				Name:      node.Name,
				Context:   ctx,
				NewaRef:   &node,
			})
		}

		sendDeployRequest(DeployRequest{
			Kind:      Kind_ltm_node,
			Operation: Operation_deploy,
			Partition: "Common",
			Name:      node.Name,
			Context:   ctx,
			OrigRef:   o,
			NewaRef:   &node,
		})
	}
	return nil
}

func unpackNode(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()
	ctx, node := r.Context, r.OrigRef.(*K8Node)

	if node != nil {
		nodes.unset(node.Name)
		if node.IpAddr != "" || node.IpAddrV6 != "" {
			sendDeployRequest(DeployRequest{
				Kind:      Kind_net_fdb,
				Operation: Operation_delete,
				Partition: "Common",
				Name:      node.Name,
				Context:   ctx,
				OrigRef:   &node,
			})
			sendDeployRequest(DeployRequest{
				Kind:      Kind_ltm_node,
				Operation: Operation_delete,
				Partition: "Common",
				Name:      node.Name,
				Context:   ctx,
				OrigRef:   &node,
			})
		}
		if node.Dest != "" || node.Dest6 != "" {
			sendDeployRequest(DeployRequest{
				Kind:      Kind_net_route,
				Operation: Operation_delete,
				Partition: "Common",
				Name:      node.Name,
				Context:   ctx,
				OrigRef:   &node,
			})
		}
	}
	return nil
}

// func packIngress(old, cur *netv1beta1.Ingress) error {
// 	return nil
// }

// func unpackIngress(ing *netv1beta1.Ingress) error {
// 	return nil
// }

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *EndpointsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// TODO: continue to use ctx is a better idea
	lctx := newContext(r.Cluster)
	slog := utils.LogFromContext(lctx)
	keyname := req.NamespacedName.String()
	localCache := clusterCache[r.Cluster]
	if !localCache.NSIncluded(lctx, req.Namespace) && !cmdflags.hubmode {
		return ctrl.Result{}, nil
	}

	var obj v1.Endpoints
	if err := r.Client.Get(ctx, req.NamespacedName, &obj, &client.GetOptions{}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		slog.Debugf("deleting endpoints: %s", req.NamespacedName)
		orig := localCache.GetEndpoints(keyname)
		if orig == nil {
			return ctrl.Result{}, nil
		}
		localCache.UnsetEndpoints(keyname)

		labels := orig.GetObjectMeta().GetLabels()
		if svcLabeled(labels) {
			sendDeployRequest(DeployRequest{
				Operation: Operation_unpack,
				Kind:      Kind_endpoints,
				Name:      keyname,
				OrigRef:   orig,
				Context:   lctx,
			})
		}
	} else {
		slog.Debugf("upserting endpoints: %s", req.NamespacedName)
		orig := localCache.GetEndpoints(keyname)
		newa := obj.DeepCopy()
		localCache.SetEndpoints(newa)
		if orig == nil {
			labels := newa.GetObjectMeta().GetLabels()
			if svcLabeled(labels) {
				sendDeployRequest(DeployRequest{
					Operation: Operation_pack,
					Kind:      Kind_endpoints,
					Name:      keyname,
					NewaRef:   newa,
					Context:   lctx,
				})
			}
		} else {
			olabels := orig.GetObjectMeta().GetLabels()
			nlabels := newa.GetObjectMeta().GetLabels()
			ok := svcLabeled(olabels)
			nk := svcLabeled(nlabels)
			if nk {
				sendDeployRequest(DeployRequest{
					Operation: Operation_pack,
					Kind:      Kind_endpoints,
					Name:      keyname,
					OrigRef:   orig,
					NewaRef:   newa,
					Context:   lctx,
				})
			} else if ok && !nk {
				sendDeployRequest(DeployRequest{
					Operation: Operation_unpack,
					Kind:      Kind_endpoints,
					Name:      keyname,
					OrigRef:   orig,
					Context:   lctx,
				})
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lctx := newContext(r.Cluster)
	slog := utils.LogFromContext(lctx)
	keyname := req.NamespacedName.String()
	localCache := clusterCache[r.Cluster]
	if !localCache.NSIncluded(lctx, req.Namespace) && !cmdflags.hubmode {
		return ctrl.Result{}, nil
	}

	var obj v1.Service
	if err := r.Client.Get(lctx, req.NamespacedName, &obj, &client.GetOptions{}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		slog.Debugf("deleting service: %s", req.NamespacedName)
		orig := localCache.GetService(keyname)
		if orig == nil {
			return ctrl.Result{}, nil
		}
		localCache.UnsetService(keyname)

		labels := orig.GetObjectMeta().GetLabels()
		if svcLabeled(labels) {
			sendDeployRequest(DeployRequest{
				Operation: Operation_unpack,
				Kind:      Kind_service,
				Name:      keyname,
				OrigRef:   orig,
				Context:   lctx,
			})
		}

	} else {
		slog.Debugf("upserting service: %s", req.NamespacedName)
		orig := localCache.GetService(keyname)
		newa := obj.DeepCopy()
		localCache.SetService(newa)
		if orig == nil {
			labels := newa.GetObjectMeta().GetLabels()
			if svcLabeled(labels) {
				sendDeployRequest(DeployRequest{
					Operation: Operation_pack,
					Kind:      Kind_service,
					Name:      keyname,
					NewaRef:   newa,
					Context:   lctx,
				})
			}
		} else {
			olabels := orig.GetObjectMeta().GetLabels()
			nlabels := newa.GetObjectMeta().GetLabels()
			ok := svcLabeled(olabels)
			nk := svcLabeled(nlabels)
			if nk {
				sendDeployRequest(DeployRequest{
					Operation: Operation_pack,
					Kind:      Kind_service,
					Name:      keyname,
					OrigRef:   orig,
					NewaRef:   newa,
					Context:   lctx,
				})
			} else if ok && !nk {
				sendDeployRequest(DeployRequest{
					Operation: Operation_unpack,
					Kind:      Kind_service,
					Name:      keyname,
					OrigRef:   orig,
					Context:   lctx,
				})
			}
		}

	}
	return ctrl.Result{}, nil
}

func (r *ConfigmapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lctx := newContext(r.Cluster)
	slog := utils.LogFromContext(lctx)
	keyname := req.NamespacedName.String()
	if r.Cluster != Cluster_main { // only main cluster handle configmap deployment.
		slog.Debugf("skip configmap reconciling %s for %s", keyname, r.Cluster)
		return ctrl.Result{}, nil
	}
	localCache := clusterCache[r.Cluster]
	if !localCache.NSIncluded(lctx, req.Namespace) {
		return ctrl.Result{}, nil
	}
	var obj v1.ConfigMap
	if err := r.Client.Get(lctx, req.NamespacedName, &obj, &client.GetOptions{}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		slog.Debugf("deleting configmap: %s", req.NamespacedName)
		orig := localCache.GetConfigMap(keyname)
		if orig == nil {
			return ctrl.Result{}, nil
		}
		localCache.UnsetConfigMap(keyname)
		labels := orig.GetObjectMeta().GetLabels()
		if cmLabeled(labels) {
			sendDeployRequest(DeployRequest{
				Operation: Operation_unpack,
				Kind:      Kind_configmap,
				Name:      keyname,
				OrigRef:   orig,
				Context:   lctx,
			})
		}
	} else {
		slog.Debugf("upserting configmap: %s", req.NamespacedName)
		orig := localCache.GetConfigMap(keyname)
		newa := obj.DeepCopy()
		localCache.SetConfigMap(newa)
		if orig == nil {
			labels := newa.GetObjectMeta().GetLabels()
			if cmLabeled(labels) {
				sendDeployRequest(DeployRequest{
					Operation: Operation_pack,
					Kind:      Kind_configmap,
					Name:      keyname,
					Context:   lctx,
					NewaRef:   newa,
				})
			}
		} else {
			olabels := orig.GetObjectMeta().GetLabels()
			nlabels := newa.GetObjectMeta().GetLabels()
			ok, nk := cmLabeled(olabels), cmLabeled(nlabels)
			if nk {
				if ok {
					// remove this check because we need to handle the event at the very start.
					// if orig.Data["template"] != newa.Data["template"] {
					sendDeployRequest(DeployRequest{
						Operation: Operation_pack,
						Kind:      Kind_configmap,
						Name:      keyname,
						NewaRef:   newa,
						Context:   lctx,
					})
					// }
				} else {
					sendDeployRequest(DeployRequest{
						Operation: Operation_pack,
						Kind:      Kind_configmap,
						Name:      keyname,
						NewaRef:   newa,
						Context:   lctx,
					})
				}
			} else if ok && !nk {
				sendDeployRequest(DeployRequest{
					Operation: Operation_unpack,
					Kind:      Kind_configmap,
					Name:      keyname,
					OrigRef:   orig,
					Context:   lctx,
				})
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lctx := newContext(r.Cluster)
	slog := utils.LogFromContext(lctx)

	var obj v1.Node
	if err := r.Client.Get(lctx, req.NamespacedName, &obj, &client.GetOptions{}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		slog.Debugf("deleting node: %s %s", Kind_node, req.Name)
		if orig := nodes.get(req.Name); orig != nil {
			sendDeployRequest(DeployRequest{
				Operation: Operation_unpack,
				Kind:      Kind_node,
				Name:      req.Name,
				Partition: "cis-c-tenant",
				OrigRef:   orig,
				Context:   lctx,
			})
		}
	} else {
		slog.Debugf("upserting node: %s %s", Kind_node, req.Name)
		newa := obj.DeepCopy()
		sendDeployRequest(DeployRequest{
			Operation: Operation_pack,
			Kind:      Kind_node,
			Partition: "cis-c-tenant",
			Name:      req.Name,
			NewaRef:   newa,
			Context:   lctx,
		})
	}
	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lctx := newContext(r.Cluster)
	slog := utils.LogFromContext(lctx)
	localCache := clusterCache[r.Cluster]

	var obj v1.Namespace
	if err := r.Client.Get(lctx, req.NamespacedName, &obj, &client.GetOptions{}); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		slog.Debugf("deleting namespace: %s", req.Name)
		// localCache.UnsetNamespace(req.Name)
	} else {
		// TODO: ?, do not handle the case of label-change which leads to changes of resources.
		slog.Debugf("upserting namespace: %s", req.Name)
		localCache.SetNamespace(obj.DeepCopy())
	}
	return ctrl.Result{}, nil
}
