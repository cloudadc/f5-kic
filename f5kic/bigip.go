package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/f5devcentral/f5-bigip-rest-go/utils"
)

func deployVirtualPool(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx := r.Context
	name, partition := r.Name, r.Partition
	bc := newBIGIPContext(ctx)
	oref, nref := r.OrigRef, r.NewaRef

	slog := utils.LogFromContext(ctx)

	old, cur := LTMResources{}, LTMResources{}
	if oref != nil && oref.(*LTMResources) != nil {
		old = *oref.(*LTMResources)
	}
	if nref != nil && nref.(*LTMResources) != nil {
		cur = *nref.(*LTMResources)
	} else {
		return fmt.Errorf("cfg with key %s not found, quit", partition)
	}

	slog.Debugf("deploying partition %s for configmap %s", partition, name)

	if err := bc.DeployPartition(partition); err != nil {
		return err
	}

	origs := r.ObjRefs[0].(map[string]SvcEpsMembers)
	newas := psmap.getByPartition(partition)

	return _deployVirtualPool(ctx, partition, old, cur, origs, newas)
}
func _deployVirtualPool(ctx context.Context, partition string, old, cur LTMResources, origs, newas map[string]SvcEpsMembers) error {

	slog := utils.LogFromContext(ctx)
	bc := newBIGIPContext(ctx)
	cluster := ctx.Value(Ctx_Key_cluster).(string)

	ocfgs, ncfgs := map[string]interface{}{}, map[string]interface{}{}
	mergeCfgs(ocfgs, old.Json)
	mergeCfgs(ncfgs, cur.Json)

	// TODO: add test for pool deletion.
	// TODO: add test for member user-disabled test

	npools := poolsInCfg(&cur)
	cmns := strings.Split(cur.Cmkey, "/")[0]
	for _, pool := range npools {
		pfn := strings.Split(pool, "/")
		sembs := psmap.get(pfn[2], pfn[0], pfn[1])
		svcKey := sembs.getSvcKeyOf(cluster)
		epns := strings.Split(svcKey, "/")[0]
		if cmns == epns || cmdflags.hubmode {
		} else {
			newas[pool] = SvcEpsMembers{}
			slog.Warnf("while deployvirtualpool, with hubmode '%v', ignore the service association of cm %s <-> ep %s", cmdflags.hubmode, cmns, epns)
		}
	}

	opcfgs, npcfgs, err := genPoolMemberCmds(ctx, &old, &cur, origs, newas)
	if err != nil {
		return err
	}
	mergeCfgs(ocfgs, opcfgs)
	mergeCfgs(ncfgs, npcfgs)

	cmds, err := genRestCommands(ctx, partition, &ocfgs, &ncfgs)
	if err != nil {
		return err
	}

	if err := bc.DoRestRequests(cmds); err != nil {
		return err
	}

	oncfgs, nncfgs, err := genPoolNetCmds(ctx, &old, &cur, origs, newas)
	if err != nil {
		return err
	}
	if cmds, err := genRestCommands(ctx, "cis-c-tenant", &oncfgs, &nncfgs); err != nil {
		return err
	} else if err := bc.DoRestRequests(cmds); err != nil {
		return err
	}
	return nil
}

// TODO: check if there are node leaking issue when switching environment without resource deletions.

func deleteVirtualPool(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx, partition := r.Context, r.Partition
	bc := newBIGIPContext(ctx)
	if r.OrigRef == nil || r.OrigRef.(*LTMResources) == nil {
		return fmt.Errorf("cfg with key %s for deletion not found, quit", partition)
	}

	cfg := r.OrigRef.(*LTMResources)
	origs, newas := r.ObjRefs[0].(map[string]SvcEpsMembers), map[string]SvcEpsMembers{}
	if err := _deployVirtualPool(ctx, partition, *cfg, LTMResources{}, origs, newas); err != nil {
		return err
	}

	if err := bc.DeletePartition(partition); err != nil {
		return err
	}
	return nil
}

func deployPoolMembers(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx := r.Context
	name, partition, subfolder := r.Name, r.Partition, r.Subfolder
	keyname := utils.Keyname(partition, subfolder, name)
	oref, nref := r.OrigRef, r.NewaRef

	slog := utils.LogFromContext(ctx)

	origs, newas := oref.(map[string]SvcEpsMembers), nref.(map[string]SvcEpsMembers)

	cfg := cfgs.get(partition)
	if cfg == nil {
		slog.Warnf("not found cfg for %s", partition)
		return nil
	}

	if reflect.DeepEqual(origs, newas) {
		// there is no change to the pool, including members and arps
		slog.Debugf("no changes to pool %s", utils.Keyname(partition, subfolder, name))
		return nil
	}

	slog.Debugf("generating nodes/nets/pool rest requests for deploying %s", keyname)

	return _deployPoolMembers(ctx, keyname, cfg, origs, newas)
}

func deletePoolMembers(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx := r.Context
	oref, nref := r.OrigRef, r.NewaRef
	keyname := utils.Keyname(r.Partition, r.Subfolder, r.Name)
	// cluster := r.Context.Value(Ctx_Key_cluster).(string)

	slog := utils.LogFromContext(ctx)

	origs, newas := oref.(map[string]SvcEpsMembers), nref.(map[string]SvcEpsMembers)
	newas[keyname] = SvcEpsMembers{}

	var cfg *LTMResources = nil
	if len(r.ObjRefs) > 0 && r.ObjRefs[0] != nil {
		cfg = r.ObjRefs[0].(*LTMResources)
	}
	if cfg == nil {
		slog.Warnf("cfg for %s not found, skip deleting members.", r.Partition)
		return nil
	}

	return _deployPoolMembers(ctx, keyname, cfg, origs, newas)
}

func _deployPoolMembers(ctx context.Context, keyname string, cfg *LTMResources, origs, newas map[string]SvcEpsMembers) error {
	bc := newBIGIPContext(ctx)

	partition := strings.Split(keyname, "/")[0]

	ocfgs, ncfgs, err := genPoolMemberCmds(ctx, cfg, cfg, origs, newas)
	if err != nil {
		return err
	}

	if cmds, err := genRestCommands(ctx, partition, &ocfgs, &ncfgs); err != nil {
		return err
	} else if err := bc.DoRestRequests(cmds); err != nil {
		return err
	}

	ocfgs, ncfgs, err = genPoolNetCmds(ctx, cfg, cfg, origs, newas)
	if err != nil {
		return err
	}
	if cmds, err := genRestCommands(ctx, "cis-c-tenant", &ocfgs, &ncfgs); err != nil {
		return err
	} else if err := bc.DoRestRequests(cmds); err != nil {
		return err
	}
	// TODO: uncomment it, consider the performance carefully.
	// purgeCommonNodes(ctx, ombs)

	return nil
}

func updatePoolMembers(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx, name, objref := r.Context, r.Name, r.NewaRef
	slog := utils.LogFromContext(ctx)
	bc := newBIGIPContext(ctx)
	action := ctx.Value(Ctx_Key_action).(map[string]interface{})
	msg := "200|proceeded " + name
	chResp := objref.(chan string)
	defer func() { chResp <- msg }()

	if action == nil {
		msg = fmt.Sprintf("500|context miss '%s'", Ctx_Key_action)
		return fmt.Errorf(msg)
	}

	memberlinks := psmap.memberLinks(name)
	if len(memberlinks) == 0 {
		msg = fmt.Sprintf("404|member address for %s not found", name)
		slog.Warnf(msg)
		return nil // not consider as an error, the member may have been deleted from the pool
	}
	for _, link := range memberlinks {
		err := bc.Restcall(link, "PATCH", map[string]string{}, action)
		if err != nil {
			msg = fmt.Sprintf("500|internal error while updating member %s: %s", link, err.Error())
			return fmt.Errorf(msg)
		}
	}

	return nil
}

func deployCommonNode(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx := r.Context
	bc := newBIGIPContext(ctx)
	slog := utils.LogFromContext(ctx)

	ncfgs := map[string]interface{}{}
	for _, x := range r.Enum() {
		name := x.Name
		node := nodes.get(name)
		if node == nil {
			slog.Warnf("not found the node: %s", name)
			continue
		}
		if node.IpAddr != "" {
			ncfgs["ltm/node/"+node.IpAddr] = map[string]interface{}{
				"name":      node.IpAddr,
				"partition": "Common",
				"address":   node.IpAddr,
				"monitor":   "default",
				"session":   "user-enabled",
			}
		}
		if node.IpAddrV6 != "" {
			ncfgs["ltm/node/"+node.IpAddrV6] = map[string]interface{}{
				"name":      node.IpAddrV6,
				"partition": "Common",
				"address":   node.IpAddrV6,
				"monitor":   "default",
				"session":   "user-enabled",
			}
		}
	}

	cmds, err := genRestCommands(ctx, "Common", nil, &map[string]interface{}{"": ncfgs})
	if err != nil {
		return nil
	}
	return bc.DoRestRequests(cmds)
}

func deleteCommonNode(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx, objref := r.Context, r.OrigRef
	if objref == nil {
		return fmt.Errorf("the deleted node cannot be nil")
	}

	node := objref.(*K8Node)
	bc := newBIGIPContext(ctx)

	ocfgs := map[string]interface{}{}
	if node.IpAddr != "" {
		ocfgs["ltm/node/"+node.IpAddr] = map[string]interface{}{}
	}
	if node.IpAddrV6 != "" {
		ocfgs["ltm/node/"+node.IpAddrV6] = map[string]interface{}{}
	}

	cmds, err := genRestCommands(ctx, "Common", &map[string]interface{}{"": ocfgs}, nil)
	if err != nil {
		return nil
	}
	return bc.DoRestRequests(cmds)
}

// TODO: see https://indexing2.f5net.com/source/xref/tmos-tier2/tmui/web/tmui/common/icontrolrest/Api.js
// for saving partition to disk.

// saveCfgsPSMap save cfgs and psmap structures into data-group
func saveCfgsPSMap(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx := r.Context
	bc := newBIGIPContext(ctx)
	slog := utils.LogFromContext(ctx)

	existings, err := bc.GetExistingResources("cis-c-tenant", []string{"ltm/data-group/internal"})
	if err != nil {
		return err
	}
	ocfgs := map[string]interface{}{}
	ncfgs := map[string]interface{}{}
	if existings != nil {
		for k, v := range (*existings)["ltm/data-group/internal"] {
			pn := strings.Split(k, "/")
			if len(pn) != 2 {
				slog.Warnf("data-group for persisting should be <partition>/<name>: %s", pn)
				continue
			}

			ocfgs["ltm/data-group/internal/"+pn[1]] = v
		}
	}

	for _, p := range cfgs.allKeys() {
		cfg := cfgs.get(p)
		if cfg == nil {
			slog.Warnf("cfg for %s is not found, cannot persist it", p)
			continue
		}
		dgname := dgnameOfCfg(p)
		bytes, err := json.Marshal(*cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal config data for %s: %s", p, err.Error())
		}

		ncfgs["ltm/data-group/internal/"+dgname] = dumpDataGroup(dgname, "cis-c-tenant", bytes)
	}
	for _, kn := range psmap.allKeys() {
		pfn := strings.Split(kn, "/")
		sembs := psmap.get(pfn[2], pfn[0], pfn[1])
		dgname := dgnameOfPSMap(pfn[2], pfn[0], pfn[1])
		bytes, err := json.Marshal(sembs)
		if err != nil {
			return fmt.Errorf("failed to marshal sedata data for %s: %s", kn, err.Error())
		}

		ncfgs["ltm/data-group/internal/"+dgname] = dumpDataGroup(dgname, "cis-c-tenant", bytes)
	}

	// check if records are exactly same, skip if that for performance.
	for k := range ncfgs {
		if _, f := ocfgs[k]; f {
			if utils.DeepEqual(
				ocfgs[k].(map[string]interface{})["records"],
				ncfgs[k].(map[string]interface{})["records"],
			) {
				delete(ocfgs, k)
				delete(ncfgs, k)
			}
		}
	}
	if cmds, err := bc.GenRestRequests("cis-c-tenant",
		&map[string]interface{}{"": ocfgs},
		&map[string]interface{}{"": ncfgs},
		existings); err != nil {
		return err
	} else {
		return bc.DoRestRequests(cmds)
	}
}

func updateNetFdbs(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	// slog := utils.LogFromContext(ctx)
	ctx := r.Context
	partition, operation := r.Partition, r.Operation
	origObj, newaObj := r.OrigRef, r.NewaRef
	bc := newBIGIPContext(ctx)

	var orig, newa *K8Node = nil, nil
	if origObj != nil {
		orig = origObj.(*K8Node)
	}
	if newaObj != nil {
		newa = newaObj.(*K8Node)
	}

	ocfgs := map[string]interface{}{}
	ncfgs := map[string]interface{}{}
	records := []interface{}{}

	if cmdflags.flannelName == "" && cmdflags.flannelNameV6 == "" {
		return fmt.Errorf("neither --flannel-name or --flannel-name-v6 parameter is set")
	}

	if cmdflags.flannelName != "" {
		rec, err := bc.Fdbs(cmdflags.flannelName)
		if err != nil {
			return fmt.Errorf("failed to do fdb update: %s", err.Error())
		}

		if operation == Operation_delete {
			for n, rc := range *rec {
				if orig != nil && n == orig.MacAddr {
					continue
				}
				records = append(records, map[string]interface{}{
					"name":     n,
					"endpoint": rc,
				})
			}
		} else if operation == Operation_deploy {
			for n, rc := range *rec {
				records = append(records, map[string]interface{}{
					"name":     n,
					"endpoint": rc,
				})
			}
			if newa != nil && newa.MacAddr != "" && newa.IpAddr != "" {
				records = append(records, map[string]interface{}{
					"name":     newa.MacAddr,
					"endpoint": newa.IpAddr,
				})
			}
		}

		fns := strings.Split(cmdflags.flannelName, "/")
		epname := fmt.Sprintf("net/fdb/tunnel/%s", fns[len(fns)-1])
		ncfgs[epname] = map[string]interface{}{
			"name":    cmdflags.flannelName,
			"records": records,
		}
	}

	recordsv6 := []interface{}{}
	if cmdflags.flannelNameV6 != "" {
		rec, err := bc.Fdbs(cmdflags.flannelNameV6)
		if err != nil {
			return fmt.Errorf("failed to do fdbv6 update: %s", err.Error())
		}

		if operation == Operation_delete {
			for n, rc := range *rec {
				if orig != nil && n == orig.MacAddrV6 {
					continue
				}
				recordsv6 = append(recordsv6, map[string]interface{}{
					"name":     n,
					"endpoint": rc,
				})
			}
		} else if operation == Operation_deploy {
			for n, rc := range *rec {
				recordsv6 = append(recordsv6, map[string]interface{}{
					"name":     n,
					"endpoint": rc,
				})
			}
			if newa != nil && newa.MacAddrV6 != "" && newa.IpAddrV6 != "" {
				recordsv6 = append(recordsv6, map[string]interface{}{
					"name":     newa.MacAddrV6,
					"endpoint": newa.IpAddrV6,
				})
			}
		}

		fns := strings.Split(cmdflags.flannelNameV6, "/")
		epname := fmt.Sprintf("net/fdb/tunnel/%s", fns[len(fns)-1])
		ncfgs[epname] = map[string]interface{}{
			"name":    cmdflags.flannelNameV6,
			"records": recordsv6,
		}
	}

	if cmds, err := genRestCommands(ctx, partition,
		&map[string]interface{}{"": ocfgs},
		&map[string]interface{}{"": ncfgs}); err == nil {
		if err := bc.DoRestRequests(cmds); err != nil {
			return err
		}
	} else {
		return err
	}
	return nil
}

func updateNetRoute(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	// slog := utils.LogFromContext(ctx)
	ctx, partition, operation, objref := r.Context, r.Partition, r.Operation, r.NewaRef
	bc := newBIGIPContext(ctx)

	node := objref.(*K8Node)
	if node == nil {
		return fmt.Errorf("cannot do net route update for nil obj")
	}

	ocfgs := map[string]interface{}{}
	ncfgs := map[string]interface{}{}

	if node.IpAddr != "" && node.Dest != "" {
		name := strings.Split(node.Dest, "/")[0]
		epname := fmt.Sprintf("net/route/%s", name)
		if operation == "upsert" {
			ncfgs[epname] = map[string]interface{}{
				"name":    name,
				"network": node.Dest,
				"gw":      node.IpAddr,
			}
		} else if operation == "delete" {
			ocfgs[epname] = map[string]interface{}{
				"name":    name,
				"network": node.Dest,
				"gw":      node.IpAddr,
			}
		}
	}

	if node.IpAddrV6 != "" && node.Dest6 != "" {
		name := strings.Split(node.Dest, "/")[0]
		epname := fmt.Sprintf("net/route/%s", name)
		if operation == "upsert" {
			ncfgs[epname] = map[string]interface{}{
				"name":    name,
				"network": node.Dest6,
				"gw":      node.IpAddrV6,
			}
		} else if operation == "delete" {
			ocfgs[epname] = map[string]interface{}{
				"name":    name,
				"network": node.Dest6,
				"gw":      node.IpAddrV6,
			}
		}
	}

	if cmds, err := genRestCommands(ctx, partition,
		&map[string]interface{}{"": ocfgs},
		&map[string]interface{}{"": ncfgs}); err == nil {
		if err := bc.DoRestRequests(cmds); err != nil {
			return err
		}
	} else {
		return err
	}
	return nil
}

func saveSysConfig(r DeployRequest) error {
	defer utils.TimeItToPrometheus()()

	ctx := r.Context
	bc := newBIGIPContext(ctx)
	return bc.SaveSysConfig([]string{})
}

func loadExistingConfigs() error {
	defer utils.TimeItToPrometheus()()
	slog := utils.LogFromContext(context.TODO())
	bc := newBIGIPContext(context.TODO())
	partitions, err := bc.ListPartitions()
	if err != nil {
		return err
	}

	res, err := bc.All("ltm/data-group/internal?expandSubcollections=true")
	if err != nil {
		return err
	} else if res == nil {
		return nil
	}

	dgs := map[string]interface{}{}
	items := (*res)["items"].([]interface{})
	for _, i := range items {
		mi := i.(map[string]interface{})
		p, n := mi["partition"].(string), mi["name"].(string)
		if p != "cis-c-tenant" {
			continue
		}
		dgs[n] = mi
	}

	slog.Infof("loading data groups for %s", partitions)
	for _, p := range partitions {
		if p == "Common" || p == "cis-c-tenant" {
			continue
		}
		dgname := dgnameOfCfg(p)
		dg, found := dgs[dgname]
		if !found {
			continue
		}

		bytes, err := parseDataGroup(dg.(map[string]interface{}))
		if err != nil {
			slog.Warnf("cannot load data-group for %s %s", p, err.Error())
			continue
		}

		if len(bytes) != 0 {
			var cfg LTMResources
			if err = json.Unmarshal(bytes, &cfg); err != nil {
				slog.Warnf("failed to load data group for partition %s: %s", p, err.Error())
				continue
			}
			cfgs.set(p, cfg)
		} else {
			slog.Warnf("data-group for %s is empty", p)
		}
	}

	slog.Infof("loading data group for psmap: ")
	for _, p := range partitions {
		if p == "Common" || p == "cis-c-tenant" {
			continue
		}

		cfg := cfgs.get(p)
		if cfg != nil {
			for _, pool := range poolsInCfg(cfg) {
				pfn := strings.Split(pool, "/")
				slog.Infof("loading psmap of %s", pool)
				p, f, n := pfn[0], pfn[1], pfn[2]
				dgname := dgnameOfPSMap(n, p, f)

				dg, found := dgs[dgname]
				if !found {
					continue
				}
				slog.Debugf("loading data group %s", dgname)
				bytes, err := parseDataGroup(dg.(map[string]interface{}))
				if err != nil {
					slog.Warnf("failed to load %s: %s", dgname, err.Error())
					continue
				}
				if len(bytes) == 0 {
					slog.Warnf("data-group %s is empty", dgname)
					continue
				}

				var sembs SvcEpsMembers
				if err := json.Unmarshal(bytes, &sembs); err != nil {
					slog.Warnf("failed unmarshal psmap for %s", pfn)
					continue
				}
				psmap.set(n, p, f, "", sembs)
			}
		}
	}

	return nil
}

// // purgeCommonNodes tries to remove 'ombs' listed nodes from Common if no reference.
// // This function is called within 'deployPoolMembers', or 'deletePoolMembers'.
// // It is not suitable to run with asynchronious way, or there will be cocurrency issue.
// // If the partition of the nodes listed in 'ombs' is not Common, the function does nothing.
// // Otherwise, the nodes would be purged one by one..
// // No message reported if error happens because of delete-while-referred, that's as expected.
// // For other error, just warn it, change this behavior if need.
// func purgeCommonNodes(ctx context.Context, ombs []interface{}) {
// 	bc := newBIGIPContext(ctx)
// 	slog := utils.LogFromContext(ctx)

// 	for _, m := range ombs {
// 		partition := m.(map[string]interface{})["partition"].(string)
// 		if partition != "Common" {
// 			continue
// 		}
// 		addr := m.(map[string]interface{})["address"].(string)
// 		err := bc.Delete("ltm/node", addr, "Common", "")
// 		if err != nil && !strings.Contains(err.Error(), "is referenced by a member of pool") {
// 			slog.Warnf("cannot delete node %s: %s", addr, err.Error())
// 		}
// 	}
// }
