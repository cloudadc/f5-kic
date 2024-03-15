package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitee.com/zongzw/f5-as3-parsing/as3parsing"
	f5_bigip "github.com/f5devcentral/f5-bigip-rest-go/bigip"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	ctrl "sigs.k8s.io/controller-runtime"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	utils "github.com/f5devcentral/f5-bigip-rest-go/utils"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func init() {
	opts := zap.Options{
		Development: true,
		DestWriter:  io.Discard,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	stopCtx = ctrl.SetupSignalHandler()
	scheme = runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	mgrs = map[string]manager.Manager{}

	nodes = Nodes{
		Items: map[string]*K8Node{},
		mutex: make(chan bool, 1),
	}
	cfgs = Configs{
		Items: map[string]LTMResources{},
		mutex: make(chan bool, 1),
	}

	psmap = PSMap{
		Items: map[string]SvcEpsMembers{},
		mutex: make(chan bool, 1),
	}

	clog = utils.NewLog().WithLevel("debug").WithRequestID(uuid.New().String())
	pendingDeploys = utils.NewDeployQueue()
	clusterCache = map[string]*KicCache{}

	defaultCompare := func(a, b interface{}) bool { return false }
	defaultStop := func(a, b interface{}) bool {
		// stop when operation is different
		x, y := a.(DeployRequest), b.(DeployRequest)
		b1 := x.Kind == y.Kind
		b2 := x.Partition == y.Partition &&
			x.Subfolder == y.Subfolder &&
			x.Name == y.Name

		if b1 && b2 {
			return x.Operation != y.Operation
		} else {
			return false
		}
	}

	compareFuncs = map[string]compareStruct{
		Kind_service: {cmpFunc: defaultCompare, stopFunc: defaultStop},
		Kind_node: {
			cmpFunc: func(a, b interface{}) bool {
				x, y := a.(DeployRequest), b.(DeployRequest)
				b1 := x.Kind == y.Kind && x.Kind == Kind_node
				b2 := x.Operation == y.Operation
				return b1 && b2
			},
			stopFunc: func(a, b interface{}) bool {
				x, y := a.(DeployRequest), b.(DeployRequest)
				b1 := x.Kind == y.Kind && x.Kind == Kind_node

				if b1 {
					return x.Operation != y.Operation
				} else {
					return false
				}
			},
		},
		Kind_endpoints: {
			cmpFunc: func(a, b interface{}) bool {
				x, y := a.(DeployRequest), b.(DeployRequest)
				c1, c2 := x.Context.Value(Ctx_Key_cluster).(string), y.Context.Value(Ctx_Key_cluster).(string)
				b0 := c1 == c2
				b1 := x.Kind == y.Kind && x.Kind == Kind_endpoints
				b2 := x.Operation == y.Operation

				b3 := x.Partition == y.Partition &&
					x.Subfolder == y.Subfolder &&
					x.Name == y.Name
				return b0 && b1 && b2 && b3
			},
			// headOnly: false, // there would be potential issue if mix creat/delet the same ns/n, but good for performance.
			stopFunc: defaultStop,
		},
		Kind_ltm_node: {
			cmpFunc: func(a, b interface{}) bool {
				x, y := a.(DeployRequest), b.(DeployRequest)
				b1 := x.Kind == y.Kind && x.Kind == Kind_ltm_node
				b2 := x.Operation == y.Operation
				return b1 && b2
			},
			stopFunc: defaultStop,
		},
	}

	requestHandlers = map[string]map[string]func(r DeployRequest) error{
		Kind_service: {
			Operation_pack:   packService,
			Operation_unpack: unpackService,
		},
		Kind_endpoints: {
			Operation_pack:   packEndpoints,
			Operation_unpack: unpackEndpoints,
		},
		Kind_configmap: {
			Operation_pack:   packConfigMap,
			Operation_unpack: unpackConfigMap,
		},
		Kind_node: {
			Operation_pack:   packNode,
			Operation_unpack: unpackNode,
		},
		Kind_ltm_pool: {
			Operation_deploy: deployPoolMembers,
			Operation_delete: deletePoolMembers,
		},
		Kind_ltm_virtual: {
			Operation_deploy: deployVirtualPool,
			Operation_delete: deleteVirtualPool,
		},
		Kind_ltm_node: {
			Operation_deploy: deployCommonNode,
			Operation_delete: deleteCommonNode,
		},
		Kind_ltm_member: {
			Operation_update: updatePoolMembers,
		},
		Kind_net_fdb: {
			Operation_deploy: updateNetFdbs,
			Operation_delete: updateNetFdbs,
		},
		Kind_net_route: {
			Operation_deploy: updateNetRoute,
			Operation_delete: updateNetRoute,
		},
		Kind_config_dg: {
			Operation_save: saveCfgsPSMap,
		},
		Kind_config_sys: {
			Operation_save: saveSysConfig,
		},
	}
}

func setupCmdFlags() {
	cmdflags = CmdFlags{}

	flag.StringVar(&cmdflags.kubeConfig, "kube-config", "",
		"Required if runs as non-inCluster mode, kubernetes configuration i.e. ~/.kube/config")
	flag.StringVar(&cmdflags.bigipURL, "bigip-url", "",
		"Required, BIG-IP url, i.e. https://enr.yuxn.com:8443")
	flag.StringVar(&cmdflags.bigipPassword, "bigip-password", "",
		"Required, BIG-IP password for connection, i.e. admin")
	flag.StringVar(&cmdflags.bigipUsername, "bigip-username", "admin",
		"Optional, BIG-IP username for connection, i.e. admin")
	flag.StringVar(&cmdflags.flannelName, "flannel-name", "",
		"Optional, if not default, BigIP Flannel VxLAN Tunnel name, i.e. fl-tunnel")
	flag.StringVar(&cmdflags.flannelNameV6, "flannel-name-v6", "",
		"Optional, default is empty. BigIP Flannel VxLAN Tunnel name for ipv6, i.e. fl-tunnel-v6")
	flag.StringVar(&cmdflags.as3Service, "as3-service", "http://localhost:8081",
		"Optional, as3 service for AS3 declaration parsing")
	flag.StringVar(&cmdflags.logLevel, "log-level", "info",
		"Optional, logging level: trace debug info warn error")
	flag.Var(&cmdflags.namespaces, "namespace",
		"Optional, namespace to watch, can be used multiple times, all namespaces would be watched if neither namespaces nor namespace-label is specified")
	flag.StringVar(&cmdflags.namespaceLabel, "namespace-label", "",
		"Optional, namespace labels to select for watching, all namespaces would be watched if neither namespaces nor namespace-label is specified")
	flag.BoolVar(&cmdflags.hubmode, "hub-mode", false,
		"Optional, specify whether or not to manage ConfigMap resources in hub-mode")
	flag.BoolVar(&cmdflags.ignoreSvcPort, "ignore-service-port", false,
		"Optional, use the 1st targetPort when Pool's servicePort matching fails")
	flag.StringVar(&cmdflags.credsDir, "credentials-directory", "",
		"Optional, directory that contains the BIG-IP username, password, and/or"+
			"url files. To be used instead of username, password, and/or url arguments.")
	flag.IntVar(&cmdflags.saveInterval, "sys-save-interval", 3600,
		"Optional, the interval to run 'tmsh save sys config' for resource persistence, in seconds")
	flag.IntVar(&cmdflags.checkInterval, "health-check-interval", 15,
		"Optional, the health check interval, in seconds")
	flag.StringVar(&cmdflags.poolMemberType, "pool-member-type", "cluster",
		"Optional, the pool member type if v1.Service's 'type' is 'NodePort', valid values: cluster|nodeport")
	flag.BoolVar(&cmdflags.dryRun, "dry-run", false,
		"Optional, run with dry-run mode to skip the startup event handling, used in CIS to CIS-C migration usecase")
	flag.StringVar(&cmdflags.extendedClusters, "extended-clusters", "",
		"Optional, the secret's namespace/name which contains extended clusters' information")
	flag.BoolVar(&cmdflags.leaderElection, "leader-election", false,
		"Optional, determines whether or not to use leader election when starting the manager, used only in High Availability(HA) mode")

	flag.Parse()

	if len(cmdflags.flannelName) > 0 && !strings.HasPrefix(cmdflags.flannelName, "/") {
		cmdflags.flannelName = "/Common/" + cmdflags.flannelName
	}

	if len(cmdflags.flannelNameV6) > 0 && !strings.HasPrefix(cmdflags.flannelNameV6, "/") {
		cmdflags.flannelNameV6 = "/Common/" + cmdflags.flannelNameV6
	}

	if !utils.Contains([]string{"cluster", "nodeport"}, cmdflags.poolMemberType) {
		panic(fmt.Errorf("invalid pool-member-type: %s", cmdflags.poolMemberType))
	}
}

func setupGlobals() {
	discoverClusters()
	for k := range clusters {
		clusterCache[k] = &KicCache{
			Namespace: map[string]*v1.Namespace{},
			Endpoints: map[string]*v1.Endpoints{},
			Service:   map[string]*v1.Service{},
			ConfigMap: map[string]*v1.ConfigMap{},
			mutex:     sync.RWMutex{},
		}
		syncCoreV1Resources(k)
		setupManager(k)
	}

	if (len(cmdflags.bigipURL) == 0 || len(cmdflags.bigipUsername) == 0 ||
		len(cmdflags.bigipPassword) == 0) && len(cmdflags.credsDir) == 0 {
		clog.Errorf("Missing BIG-IP credentials info.")
		err := fmt.Errorf("missing BIG-IP credentials info")
		panic(err)
	}

	if err := getCredentials(); err != nil {
		panic(err)
	}

	bip = f5_bigip.New(cmdflags.bigipURL, cmdflags.bigipUsername, cmdflags.bigipPassword)

	if err := as3parsing.Initialize(bip, cmdflags.as3Service, cmdflags.logLevel); err != nil {
		panic(err)
	}

	if err := loadExistingConfigs(); err != nil {
		panic(err)
	}
}

func setupCallbacks() {
	httpResponse := func(ctx context.Context, w http.ResponseWriter, codeMsg string) {
		slog := utils.LogFromContext(ctx)
		reqId := utils.RequestIdFromContext(ctx)

		pieces := strings.Split(codeMsg, "|")
		statusCode := 200
		message := codeMsg
		if len(pieces) < 2 {
			statusCode = 500
			message = "'|' missing in message, " + message
		} else {
			var err error
			statusCode, err = strconv.Atoi(pieces[0])
			if err != nil {
				statusCode = 500
				message = fmt.Sprintf("invalid code '%s'", pieces[0])
			} else {
				message = strings.Join(pieces[1:], ",")
			}
		}
		switch statusCode {
		case 200:
			slog.Infof(message)
		case 404:
			slog.Warnf(message)
		default:
			slog.Errorf(message)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprintf(w, "{\"request_id\": \"%s\",\"message\": \"%s\"}\n", reqId, message)
	}
	prestopHook := func(w http.ResponseWriter, r *http.Request) {
		// TODO: need to add one more parameter to indicate which cluster it comes from.
		ctx := newContext("TODO")
		slog := utils.LogFromContext(ctx)

		slog.Infof("prestop hook is triggered with %v", r.Header)
		msg := "200|done"

		if r.Method != http.MethodGet {
			msg = fmt.Sprintf("400|the method should be %s", http.MethodGet)
			httpResponse(ctx, w, msg)
			return
		}
		if _, ok := r.Header["Pod-Address"]; !ok {
			msg = "400|header pod-address is missing"
			httpResponse(ctx, w, msg)
			return
		}

		podAddr := r.Header["Pod-Address"]
		if len(podAddr) != 1 {
			msg = fmt.Sprintf("400|invalid pod-address: %s, must be 1 item", podAddr)
			httpResponse(ctx, w, msg)
			return
		}
		if len(strings.Split(podAddr[0], " ")) != 1 {
			msg = fmt.Sprintf("400|invalid pod-address: %s, multiple ip addresses are passed in", podAddr)
			httpResponse(ctx, w, msg)
			return
		}
		if strings.Index(r.RemoteAddr, podAddr[0]) != 0 {
			msg = fmt.Sprintf("400|invalid pod-address: %s, must be equal to remote-addr", podAddr)
			httpResponse(ctx, w, msg)
			return
		}

		action := map[string]interface{}{
			"state":   "user-up",
			"session": "user-enabled",
		}
		ah, f := r.Header["Action"]
		if f {
			if len(ah) != 1 {
				msg = fmt.Sprintf("400|action header is invalid: %s, should be only 1 item", ah)
				httpResponse(ctx, w, msg)
				return
			}
			switch ah[0] {
			case "disable":
				action = map[string]interface{}{
					"state":   "user-up",
					"session": "user-disabled",
				}
			case "force-offline":
				action = map[string]interface{}{
					"state":   "user-down",
					"session": "user-disabled",
				}
			default:
				msg = fmt.Sprintf("400|unsupported action: %s", ah[0])
				httpResponse(ctx, w, msg)
				return
			}
		}

		lctx := context.WithValue(ctx, Ctx_Key_action, action)

		// there may be retryings happening in background
		// 	but will not be responsed to caller.
		chResp := make(chan string, MaxRetries+1)
		insertDeployRequest(DeployRequest{
			Operation: Operation_update,
			Kind:      Kind_ltm_member,
			Name:      podAddr[0],
			Context:   lctx,
			NewaRef:   chResp,
		})

		resp := <-chResp
		httpResponse(ctx, w, resp)
	}

	settingsHook := func(w http.ResponseWriter, r *http.Request) {
		ctx := newContext(Cluster_main)
		slog := utils.LogFromContext(ctx)

		slog.Infof("setting hook is triggered with %v", r.URL.Query())
		msg := "200|settings are updated: "

		if r.Method != http.MethodOptions {
			msg = fmt.Sprintf("400|the method should be %s", http.MethodOptions)
			httpResponse(ctx, w, msg)
			return
		}
		for k, v := range r.URL.Query() {
			if len(v) != 1 {
				msg = fmt.Sprintf("400|invalid value, should be length of 1: %s, actually %v", k, v)
				httpResponse(ctx, w, msg)
				return
			}
			if k == "log-level" {
				loglevels := []string{
					utils.LogLevel_Type_TRACE, utils.LogLevel_Type_DEBUG,
					utils.LogLevel_Type_INFO,
					utils.LogLevel_Type_WARN, utils.LogLevel_Type_ERROR}
				if !utils.Contains(loglevels, v[0]) {
					msg = fmt.Sprintf("400|invalid loglevel %s, should be in [%s]", v[0], loglevels)
					httpResponse(ctx, w, msg)
					return
				}
				msg += " log-level "
				cmdflags.logLevel = v[0]
			}
			if k == "sys-save-interval" {
				var err error
				if cmdflags.saveInterval, err = strconv.Atoi(v[0]); err != nil {
					msg = fmt.Sprintf("400|invalid %s value: %s", k, err.Error())
					httpResponse(ctx, w, msg)
					return
				}
				msg += " sys-save-interval "
			}
			if k == "health-check-interval" {
				var err error
				if cmdflags.checkInterval, err = strconv.Atoi(v[0]); err != nil {
					msg = fmt.Sprintf("400|invalid %s value: %s", k, err.Error())
					httpResponse(ctx, w, msg)
					return
				}
				msg += " health-check-interval "
			}
			if k == "dry-run" {
				dr := strings.ToLower(v[0])
				if dr == "false" {
					slog.Infof("quit the dr-run mode per user request")
					sendDeployRequest(DeployRequest{
						Operation: Operation_save,
						Kind:      Kind_config_dg,
						Context:   newContext(Cluster_main),
					})
					cmdflags.dryRun = false
					msg += " dry-run=false "
				} else {
					msg = fmt.Sprintf("400|%s", "not support dynamically entering into dry-run mode")
				}

			}
		}
		httpResponse(ctx, w, msg)
	}
	http.HandleFunc("/hook/prestop", prestopHook)
	http.HandleFunc("/hook/settings", settingsHook)
	// mgr.AddMetricsExtraHandler("/hook/prestop", func() http.HandlerFunc { return prestopHook }())
	// mgr.AddMetricsExtraHandler("/hook/settings", func() http.HandlerFunc { return settingsHook }())
	// TODO: add setting help response.
}

func setupPrometheus() {
	collectors = PrometheusCollectors{
		DeployRequestTimeCostCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "deploy_request_timecost_count",
				Help: "numbers of deploy requests grouped by kind and operation",
			},
			[]string{"kind", "operation"},
		),
		DeployRequestTimeCostTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "deploy_request_timecost_total",
				Help: "time cost total(in milliseconds) of deploy requests grouped by kind and operation",
			},
			[]string{"kind", "operation"},
		),
	}

	prometheus.MustRegister(collectors.DeployRequestTimeCostCount)
	prometheus.MustRegister(collectors.DeployRequestTimeCostTotal)

	prometheus.MustRegister(f5_bigip.BIGIPiControlTimeCostCount)
	prometheus.MustRegister(f5_bigip.BIGIPiControlTimeCostTotal)

	prometheus.MustRegister(utils.FunctionDurationTimeCostCount)
	prometheus.MustRegister(utils.FunctionDurationTimeCostTotal)

	http.HandleFunc("/metrics", promhttp.Handler().ServeHTTP)
	http.HandleFunc("/stats", promhttp.Handler().ServeHTTP) // for legacy use
	// mgr.AddMetricsExtraHandler("/stats", promhttp.Handler())
}

func setupRevealer() {
	if cmdflags.logLevel != "debug" && cmdflags.logLevel != "trace" {
		return
	}
	allcfgs := func() map[string]interface{} {
		rlt := map[string]interface{}{}
		for _, k := range cfgs.allKeys() {
			res := cfgs.get(k)
			rlt[k] = *res
		}
		return rlt
	}
	allparams := func() map[string]interface{} {
		return map[string]interface{}{
			// TODO: reflect cannot access private fields, rename it to upper case.
			"kubeConfig":       cmdflags.kubeConfig,
			"bigipURL":         cmdflags.bigipURL,
			"bigipUsername":    cmdflags.bigipUsername,
			"bigipPassword":    cmdflags.bigipPassword,
			"flannelName":      cmdflags.flannelName,
			"flannelNameV6":    cmdflags.flannelNameV6,
			"as3Service":       cmdflags.as3Service,
			"logLevel":         cmdflags.logLevel,
			"namespaces":       cmdflags.namespaces,
			"namespaceLabel":   cmdflags.namespaceLabel,
			"hubmode":          cmdflags.hubmode,
			"ignoreSvcPort":    cmdflags.ignoreSvcPort,
			"saveInterval":     cmdflags.saveInterval,
			"checkInterval":    cmdflags.checkInterval,
			"dryRunMode":       cmdflags.dryRun,
			"poolMemberType":   cmdflags.poolMemberType,
			"extendedClusters": cmdflags.extendedClusters,
			"leaderElection":   cmdflags.leaderElection,
		}
	}
	started := time.Now().Format(time.RFC3339)
	build := func() string {
		bf := "/build"
		_, err := os.Stat(bf)
		if err != nil {
			if os.IsNotExist(err) {
				return "developing"
			} else {
				return "unknown because " + err.Error()
			}
		} else {
			f, err := os.Open(bf)
			if err != nil {
				return "unknown because " + err.Error()
			} else {
				b, err := io.ReadAll(f)
				if err != nil {
					return "unknown because " + err.Error()
				} else {
					return strings.Trim(string(b), "\n")
				}
			}
		}
	}
	dumpStructs := func(w http.ResponseWriter, r *http.Request) {
		cache := map[string]interface{}{}
		for k := range clusters {
			cache[k] = clusterCache[k].All()
		}
		all := map[string]interface{}{
			"start":    started,
			"build":    build(),
			"cfgs":     allcfgs(),
			"psmap":    psmap.dumps(),
			"nodes":    nodes.all(),
			"cmdflags": allparams(),
			"cache":    cache,
			"queue":    pendingDeploys.Dumps(),
		}
		paths := strings.Split(r.URL.Path, "/")

		var p interface{}
		if len(paths) < 3 {
			p = map[string]interface{}{"error": "invalid path: " + r.URL.Path}
			ball, _ := json.Marshal(p)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(ball)
		} else {
			if _, f := all[paths[2]]; f {
				p = all[paths[2]]
			} else if paths[2] == "" {
				p = all
			} else {
				p = map[string]interface{}{"error": "not found key: " + paths[2]}
			}

			ball, _ := json.Marshal(p)
			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(ball)
		}
	}
	// mgr.AddMetricsExtraHandler("/dumps", func() http.HandlerFunc { return dumpStructs }())
	http.HandleFunc(`/dumps/`, dumpStructs)
}

func ltmWorker() {
	startLoopTasks()

	for {
		select {
		case <-stopCtx.Done():
			clog.Debugf("ltm worker quit")
			return
		default:
			waitAndHandle()
		}
	}
}

func startLoopTasks() {
	loopTask("save sys config", &cmdflags.saveInterval,
		func() {
			sendDeployRequest(DeployRequest{
				Operation: Operation_save,
				Kind:      Kind_config_sys,
				Context:   newContext(Cluster_main),
			})
		})
	loopTask("checking pendingDeploys", &cmdflags.checkInterval, func() {
		ctx := newContext(Cluster_main)
		slog := utils.LogFromContext(ctx)
		slog.Debugf("pendingDeploys queue length: %d", pendingDeploys.Len())
	})

	dgSaveInterval := 600
	loopTask("persist to datagroup", &dgSaveInterval, func() {
		sendDeployRequest(DeployRequest{
			Operation: Operation_save,
			Kind:      Kind_config_dg,
			Context:   newContext(Cluster_main),
		})
	})
}

func waitAndHandle() {
	r := pendingDeploys.Get().(DeployRequest)
	slog := utils.LogFromContext(r.Context)
	defer timeRequest()(&r)

	l := pendingDeploys.Len()
	cmp, f := compareFuncs[r.Kind]
	if l > 0 && f {
		fs := pendingDeploys.Filter(r, cmp.cmpFunc, cmp.stopFunc)
		for _, f := range fs {
			r.Merge(f.(DeployRequest))
		}
	}

	slog.Infof("started handling request %s %s for %s %s", r.Operation, r.Kind, r.Partition, r.Name)
	defer slog.Infof("done handling request %s %s for %s %s", r.Operation, r.Kind, r.Partition, r.Name)

	var err error = nil
	if kh, f := requestHandlers[r.Kind]; !f {
		slog.Errorf("invalid kind '%s', not support", r.Kind)
	} else if oh, f := kh[r.Operation]; !f {
		slog.Errorf("invalid operation '%s', not support", r.Operation)
	} else {
		err = oh(r)
	}

	if err != nil {
		if utils.NeedRetry(err) {
			slog.Errorf("failed to do ltm request %s: %s, retrying", r.String(), err.Error())
			retryDeployRequest(r)
		} else {
			slog.Errorf("end handling ltm request %s: %s", r.String(), err.Error())
		}
	}
}

func setupManager(name string) {
	var err error

	mgr, err := ctrl.NewManager(clusters[name].Config, ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		clog.Errorf("unable to start manager for %s: %s", name, err.Error())
		os.Exit(1)
	}

	clog.Infof("add controller-runtime reconcilers")
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.Pod{}).
		Complete(&PodReconciler{Client: mgr.GetClient(), Cluster: name}); err != nil {
		os.Exit(1)
	}
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.Endpoints{}).
		Complete(&EndpointsReconciler{Client: mgr.GetClient(), Cluster: name}); err != nil {
		os.Exit(1)
	}
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.Node{}).
		Complete(&NodeReconciler{Client: mgr.GetClient(), Cluster: name}); err != nil {
		os.Exit(1)
	}
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.Service{}).
		Complete(&ServiceReconciler{Client: mgr.GetClient(), Cluster: name}); err != nil {
		os.Exit(1)
	}
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.ConfigMap{}).
		Complete(&ConfigmapReconciler{Client: mgr.GetClient(), Cluster: name}); err != nil {
		os.Exit(1)
	}
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.Namespace{}).
		Complete(&NamespaceReconciler{Client: mgr.GetClient(), Cluster: name}); err != nil {
		os.Exit(1)
	}
	mgrs[name] = mgr
}

func startManagers() {
	for k := range clusters {
		go func(name string) {
			clog.Infof("starting runtime manager for cluster %s", name)
			if err := mgrs[name].Start(stopCtx); err != nil {
				clog.Errorf("problem running manager %s: %s", name, err.Error())
				os.Exit(1)
			}
			clog.Debugf("manager for %s stopped", name)
		}(k)
	}
}

func httpListenServe() {
	http.ListenAndServe(":8080", nil)
}

func electAsLeader() {
	emgr, err := ctrl.NewManager(newRestConfig(cmdflags.kubeConfig), ctrl.Options{
		LeaderElection:          cmdflags.leaderElection,
		LeaderElectionID:        "cis-c.f5.com",
		LeaderElectionNamespace: "kube-system",
		Metrics:                 metricsserver.Options{BindAddress: "0"},
		Logger:                  ctrl.Log, // TODO: align this logging with slog for ease of bug finding.
	})
	if err != nil {
		clog.Errorf("unable to start manager for %s: %s", Cluster_main, err.Error())
		os.Exit(1)
	}

	go func() {
		if err := emgr.Start(stopCtx); err != nil {
			clog.Errorf("failed to start electleader manager: %s", err.Error())
			os.Exit(1)
		}
		clog.Infof("elect leader manager quit.")
	}()

	select {
	case <-emgr.Elected():
		clog.Infof("elected as leader for now.")
	case <-stopCtx.Done():
		clog.Infof("quit program ...")
	}
}
