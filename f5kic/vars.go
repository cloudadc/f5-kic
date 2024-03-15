package main

import (
	"context"

	f5_bigip "github.com/f5devcentral/f5-bigip-rest-go/bigip"
	"github.com/f5devcentral/f5-bigip-rest-go/utils"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"
)

var (
	stopCtx         context.Context
	cmdflags        CmdFlags
	clog            *utils.SLOG
	clusters        map[string]ClusterSet
	bip             *f5_bigip.BIGIP
	nodes           Nodes
	cfgs            Configs
	psmap           PSMap
	clusterCache    map[string]*KicCache
	requestHandlers map[string]map[string]func(r DeployRequest) error
	compareFuncs    map[string]compareStruct
	pendingDeploys  *utils.DeployQueue

	collectors PrometheusCollectors

	scheme *runtime.Scheme
	mgrs   map[string]manager.Manager
)

const (
	Label_Tenant                                    = "cis.f5.com/as3-tenant"
	Label_App                                       = "cis.f5.com/as3-app"
	Label_Pool                                      = "cis.f5.com/as3-pool"
	Label_f5type                                    = "f5type"
	Label_as3                                       = "as3"
	Label_multicluster_pool_weight                  = "cis.f5.com/multi-cluster.pool-weight"
	Ctx_Key_action                 utils.CtxKeyType = "action"
	Ctx_Key_cluster                utils.CtxKeyType = "cluster"
	MaxRetries                                      = 10
	Cluster_main                                    = "____main-cluster-internal____"
)

const (
	Kind_configmap   = "configmap"
	Kind_endpoints   = "endpoints"
	Kind_service     = "service"
	Kind_node        = "node"
	Kind_net_arps    = "net.arps"
	Kind_net_fdb     = "net.fdb"
	Kind_net_route   = "net.route"
	Kind_config_sys  = "config.sys"
	Kind_config_dg   = "config.dg"
	Kind_ltm_virtual = "ltm.virtual"
	Kind_ltm_pool    = "ltm.poolmembers"
	Kind_ltm_node    = "ltm.node"
	Kind_ltm_member  = "ltm.member"

	Operation_deploy = "deploy"
	Operation_delete = "delete"
	Operation_pack   = "pack"
	Operation_unpack = "unpack"
	Operation_save   = "save"
	Operation_update = "update"
)
