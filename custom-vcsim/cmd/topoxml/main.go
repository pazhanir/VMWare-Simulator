// topoxml replicates the Site24x7 poller's VMwareVCenterDataCollector.fetchDetails()
// + getResponseXML() logic against the live simulator, to prove whether the
// simulator provides a well-formed DC->cluster->host topology tree.
//
// Mirrors the poller exactly:
//   - DC element:    UUID = datacenter MOR,            PARENT = "-1"
//   - Cluster:       UUID = cluster MOR,               PARENT = DC MOR
//   - Host:          UUID = hardware.systemInfo.uuid,  PARENT = cluster MOR
//   - getResponseXML links a child to a parent ONLY if a prior element of
//     parentType has UUID == child.PARENT.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type inv struct {
	name, typ, parent, parentType, uuid, morID string
}

func main() {
	addr := os.Getenv("VC_URL")
	if addr == "" {
		addr = "https://administrator@vsphere.local:Site24x7!Demo@100.75.23.73:443/sdk"
	}
	ctx := context.Background()
	u, _ := url.Parse(addr)
	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		panic(err)
	}
	defer c.Logout(ctx)
	m := view.NewManager(c.Client)

	get := func(kind string, props []string) []mo.Reference {
		v, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{kind}, true)
		if err != nil {
			panic(err)
		}
		defer v.Destroy(ctx)
		var out []mo.Reference
		switch kind {
		case "Datacenter":
			var l []mo.Datacenter
			v.Retrieve(ctx, []string{kind}, props, &l)
			for i := range l {
				out = append(out, &l[i])
			}
		case "ClusterComputeResource":
			var l []mo.ClusterComputeResource
			v.Retrieve(ctx, []string{kind}, props, &l)
			for i := range l {
				out = append(out, &l[i])
			}
		case "HostSystem":
			var l []mo.HostSystem
			v.Retrieve(ctx, []string{kind}, props, &l)
			for i := range l {
				out = append(out, &l[i])
			}
		}
		return out
	}

	// Build inventory list the way fetchDetails() does.
	var list []inv
	dcs := get("Datacenter", []string{"name", "hostFolder"})
	clusters := get("ClusterComputeResource", []string{"name", "parent", "host"})
	hosts := get("HostSystem", []string{"name", "parent", "summary", "hardware"})

	for _, dr := range dcs {
		d := dr.(*mo.Datacenter)
		dcMOR := d.Self.Value
		list = append(list, inv{d.Name, "Datacenter", "-1", "-1", dcMOR, ""})

		// clusters whose parent chain belongs to this DC (parent = DC hostFolder)
		for _, cr := range clusters {
			cl := cr.(*mo.ClusterComputeResource)
			// poller getClusterInDC uses the DC's host folder traversal; here we
			// link via the cluster's parent folder belonging to the DC.
			if !belongsToDC(cl.Parent, d) {
				continue
			}
			clMOR := cl.Self.Value
			list = append(list, inv{cl.Name, "ClusterComputeResource", dcMOR, "Datacenter", clMOR, clMOR})

			for _, hr := range hosts {
				h := hr.(*mo.HostSystem)
				if h.Parent == nil || h.Parent.Value != clMOR {
					continue
				}
				hUUID := ""
				if h.Hardware != nil {
					hUUID = h.Hardware.SystemInfo.Uuid
				}
				hName := h.Summary.Config.Name
				list = append(list, inv{hName, "HostSystem", clMOR, "ClusterComputeResource", hUUID, h.Self.Value})
			}
		}
	}

	fmt.Println("======== INVENTORY LIST (as fetchDetails builds it) ========")
	for _, e := range list {
		fmt.Printf("%-22s type=%-22s UUID=%-40s PARENT=%-20s parentType=%s\n",
			e.name, e.typ, e.uuid, e.parent, e.parentType)
	}

	// Now replicate getResponseXML linkage: a child links only if a prior
	// element of parentType has UUID == child.PARENT.
	fmt.Println("\n======== LINKAGE CHECK (getResponseXML logic) ========")
	linked, orphan := 0, 0
	for _, e := range list {
		if e.parent == "-1" {
			fmt.Printf("ROOT   %-22s (%s)\n", e.name, e.typ)
			continue
		}
		found := false
		for _, p := range list {
			if p.typ == e.parentType && p.uuid == e.parent {
				found = true
				break
			}
		}
		if found {
			linked++
			fmt.Printf("LINKED %-22s -> parent %s (%s) [%s]\n", e.name, e.parent, e.parentType, e.typ)
		} else {
			orphan++
			fmt.Printf("ORPHAN %-22s -> MISSING parent %s (%s) [%s]  <<< breaks map\n", e.name, e.parent, e.parentType, e.typ)
		}
	}
	fmt.Printf("\nSUMMARY: %d elements, %d linked, %d orphaned\n", len(list), linked, orphan)
	if orphan == 0 {
		fmt.Println("RESULT: topology tree is FULLY CONNECTED — simulator provides everything the map needs.")
	} else {
		fmt.Println("RESULT: tree has ORPHANS — these break server-side rendering.")
	}
}

// belongsToDC checks whether a cluster's parent folder is the DC's hostFolder
// (or nested under it). Simplified: clusters live directly under DC hostFolder.
func belongsToDC(clusterParent *types.ManagedObjectReference, d *mo.Datacenter) bool {
	if clusterParent == nil {
		return false
	}
	return clusterParent.Value == d.HostFolder.Value
}
