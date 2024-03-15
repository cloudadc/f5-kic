package main

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// cfgs[<partition>]: map[string]interface{}
type Configs struct {
	Items map[string]LTMResources
	mutex chan bool
}

type LTMResources struct {
	// Cluster   string
	Cmkey     string
	Raw       string
	Json      map[string]interface{} // json[<folder>]: map[string]interface{}
	Partition string
}

type PSMap struct { // Pool to Service Mapping
	Items map[string]SvcEpsMembers
	mutex chan bool
}

type SvcEpsMembers []SvcEpsMember
type SvcEpsMember struct {
	Cluster    string
	InPool     string
	Weight     int
	SvcKey     string
	SvcType    string
	TargetPort int
	NodePort   int
	IpAddr     string
	MacAddr    string
}

type Nodes struct {
	Items map[string]*K8Node
	mutex chan bool
}

type K8Node struct {
	MacAddr   string `json:"macaddr"`
	IpAddr    string `json:"ipaddr"`
	Dest      string `json:"dest"`
	MacAddrV6 string `json:"macaddrv6"`
	IpAddrV6  string `json:"ipaddrv6"`
	Dest6     string `json:"dest6"`
	Name      string `json:"name"`
	NetType   string `json:"nettype"`
}

// Don't inline struct into this object, or:
// 	panic: runtime error: hash of unhashable type main.QueueObject
// Passing k8s obj reference is not allowed:
// 	Could not load source '': Unsupported command: cannot process "source" request.
// 	ObjRef interface{}

type DeployRequest struct {
	Timestamp time.Time
	// Cluster   string
	Operation string
	Partition string
	Subfolder string
	Name      string
	Kind      string
	Retries   int
	Context   context.Context
	NewaRef   interface{}      // Target data structure for deployment
	OrigRef   interface{}      // Original datas tructure for deployment
	ObjRefs   []interface{}    // Object references for this request, self interpreted
	Relevants []*DeployRequest // Related DeployRequest, used to combine relevant requests
	// ReportIt  bool
}

type CmdFlags struct {
	kubeConfig       string
	bigipURL         string
	bigipUsername    string
	bigipPassword    string
	flannelName      string
	flannelNameV6    string
	as3Service       string
	logLevel         string
	namespaces       arrayFlags
	namespaceLabel   string
	hubmode          bool
	ignoreSvcPort    bool
	poolMemberType   string
	credsDir         string
	saveInterval     int
	checkInterval    int
	dryRun           bool
	extendedClusters string
	leaderElection   bool
}

type PrometheusCollectors struct {
	DeployRequestTimeCostCount *prometheus.GaugeVec
	DeployRequestTimeCostTotal *prometheus.GaugeVec
}

type arrayFlags []string

type PodReconciler struct {
	Client  client.Client
	Cluster string
}

type EndpointsReconciler struct {
	Client  client.Client
	Cluster string
}

type NodeReconciler struct {
	Client  client.Client
	Cluster string
}

type ServiceReconciler struct {
	Client  client.Client
	Cluster string
}

type ConfigmapReconciler struct {
	Client  client.Client
	Cluster string
}

type NamespaceReconciler struct {
	Client  client.Client
	Cluster string
}

type KicCache struct {
	mutex     sync.RWMutex
	Namespace map[string]*v1.Namespace
	Endpoints map[string]*v1.Endpoints
	Service   map[string]*v1.Service
	ConfigMap map[string]*v1.ConfigMap
}

type compareStruct struct {
	cmpFunc  func(a, b interface{}) bool
	stopFunc func(a, b interface{}) bool
}

type ClusterSet struct {
	Clientset  *kubernetes.Clientset
	Config     *rest.Config
	KubeConfig string // possibly can be 1) file path, 2) kubeConfig content 3) "" for inCluster mode
}
