// Phase 4: datastore free-space and resource-pool usage correlation.
//
//	datastore.FreeSpace      = Capacity - Σ(committed disk of VMs on it)
//	resourcePool.Runtime.*   = Σ(CPU/mem usage of member VMs)
//
// The project's VMs are created without virtual disks (committed == 0), so we
// first stamp each VM with a synthesized disk size from its LoadState, then sum
// those into the datastores. Resource-pool runtime usage is summed from the
// member VMs' load state (CPU MHz, memory MB).
package correlate

import (
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
)

const mb = int64(1024 * 1024)

// rollupStorage recomputes each datastore's FreeSpace as capacity minus the
// space the VMs stored on it consume (clamped so it never goes negative or above
// capacity).
//
// vcsim reports committed=0 for synthetic thin disks (no real bytes are written
// to its backing store), so we size datastore usage from each VM's provisioned
// disk CAPACITY at a realistic ~70% committed ratio — which is what a monitoring
// tool sees as "used" on a thin-provisioned datastore. The disk capacity itself
// is real (set by the builder), so usage is deterministic and correlated.
func rollupStorage(m *simulator.Registry) {
	const committedRatio = 70 // % of provisioned disk treated as committed

	usedByDS := map[types.ManagedObjectReference]int64{}
	for _, e := range m.All("VirtualMachine") {
		vm, ok := e.(*simulator.VirtualMachine)
		if !ok {
			continue
		}
		capBytes := vmDiskCapacityBytes(vm)
		committed := capBytes * committedRatio / 100
		// Attribute to the VM's datastore(s); use the first if several.
		var ds types.ManagedObjectReference
		if vm.Storage != nil && len(vm.Storage.PerDatastoreUsage) > 0 {
			ds = vm.Storage.PerDatastoreUsage[0].Datastore
		} else if len(vm.Datastore) > 0 {
			ds = vm.Datastore[0]
		} else {
			continue
		}
		usedByDS[ds] += committed
	}
	committedByDS := usedByDS

	for _, e := range m.All("Datastore") {
		ds, ok := e.(*simulator.Datastore)
		if !ok {
			continue
		}
		used := committedByDS[ds.Self]
		cap := ds.Summary.Capacity
		free := cap - used
		if free < 0 {
			free = 0
		}
		if free > cap {
			free = cap
		}
		ds.Summary.FreeSpace = free
		if info := ds.Info.GetDatastoreInfo(); info != nil {
			info.FreeSpace = free
		}
	}
}

// vmDiskCapacityBytes returns the total provisioned capacity of a VM's virtual
// disks (in bytes), read from its hardware device list.
func vmDiskCapacityBytes(vm *simulator.VirtualMachine) int64 {
	if vm.Config == nil {
		return 0
	}
	var total int64
	for _, d := range vm.Config.Hardware.Device {
		if disk, ok := d.(*types.VirtualDisk); ok {
			if disk.CapacityInBytes > 0 {
				total += disk.CapacityInBytes
			} else {
				total += disk.CapacityInKB * 1024
			}
		}
	}
	return total
}

// rollupResourcePools sums each pool's member-VM CPU/memory usage into its
// Runtime usage figures so the pool reflects what its VMs are actually using.
func rollupResourcePools(m *simulator.Registry) {
	for _, e := range m.All("ResourcePool") {
		rp, ok := e.(*simulator.ResourcePool)
		if !ok {
			continue
		}
		var cpuMhz, memMB int64
		for _, vmRef := range rp.Vm {
			if ls, ok := GetState(vmRef); ok {
				cpuMhz += ls.CPUUsageMhz
				memMB += ls.MemUsageMB
			}
		}
		rp.Runtime.Cpu.OverallUsage = cpuMhz
		rp.Runtime.Memory.OverallUsage = memMB * mb // bytes
	}
}
