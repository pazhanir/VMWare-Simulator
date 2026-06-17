// Package correlate maintains a single source of truth for per-entity "load"
// (CPU/memory utilization) and keeps the inventory's reported values consistent
// across the hierarchy:
//
//	VM load  -> rolls up into -> Host QuickStats -> rolls up into -> Cluster UsageSummary
//
// Phase 1 establishes a coherent STATIC snapshot: every VM gets a deterministic
// baseline load, hosts report the sum of their VMs (plus overhead), and clusters
// report the sum of their hosts — instead of the independent hash/static values
// the stock simulator and builder produce.
//
// Later phases will feed this same model from QueryPerf (so live metrics match)
// and from chaos scenarios (so a triggered scenario moves the whole tree).
package correlate

import (
	"hash/fnv"

	"github.com/vmware/govmomi/vim25/types"
)

// LoadState is the canonical utilization for one entity. CPU is tracked in MHz
// (absolute) and as a fraction of capacity; memory in MB and as a fraction.
type LoadState struct {
	// Capacity
	CPUCapacityMhz int64 // total = perCoreMHz * cores
	MemCapacityMB  int64

	// Usage (the single input that drives QuickStats + perf metrics)
	CPUUsageMhz int64
	MemUsageMB  int64

	// Derived memory breakdown (only non-zero under pressure)
	MemActiveMB    int64
	MemBalloonedMB int64
	MemSwappedMB   int64
	MemSharedMB    int64
}

// CPUPct returns CPU utilization as a percentage of capacity (0-100).
func (l LoadState) CPUPct() float64 {
	if l.CPUCapacityMhz == 0 {
		return 0
	}
	return 100 * float64(l.CPUUsageMhz) / float64(l.CPUCapacityMhz)
}

// MemPct returns memory utilization as a percentage of capacity (0-100).
func (l LoadState) MemPct() float64 {
	if l.MemCapacityMB == 0 {
		return 0
	}
	return 100 * float64(l.MemUsageMB) / float64(l.MemCapacityMB)
}

// deterministicFraction returns a stable pseudo-random fraction in [lo, hi)
// derived from a seed string, so a given VM always gets the same baseline load
// across restarts (no Math.rand — keeps the simulator reproducible).
func deterministicFraction(seed string, lo, hi float64) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return lo + float64(h.Sum32()%10000)/10000.0*(hi-lo)
}

// BaselineVMLoad assigns a deterministic, realistic working load to a VM given
// its configured size, so the inventory isn't uniform across VMs.
func BaselineVMLoad(name string, numCPU int32, coreMhz int32, memMB int32) LoadState {
	capMhz := int64(numCPU) * int64(coreMhz)
	// CPU baseline 8-35% of the VM's allotment; memory 40-75% consumed.
	cpuFrac := deterministicFraction(name+":cpu", 0.08, 0.35)
	memFrac := deterministicFraction(name+":mem", 0.40, 0.75)

	ls := LoadState{
		CPUCapacityMhz: capMhz,
		MemCapacityMB:  int64(memMB),
		CPUUsageMhz:    int64(cpuFrac * float64(capMhz)),
		MemUsageMB:     int64(memFrac * float64(memMB)),
	}
	// Under normal (non-pressure) conditions: active ~= 60% of consumed,
	// shared ~= 15%, no balloon/swap.
	ls.MemActiveMB = ls.MemUsageMB * 60 / 100
	ls.MemSharedMB = ls.MemUsageMB * 15 / 100
	return ls
}

// ApplyToVMQuickStats writes the load state onto a VM's QuickStats so the
// summary the poller reads matches the load model.
func ApplyToVMQuickStats(qs *types.VirtualMachineQuickStats, l LoadState) {
	qs.OverallCpuUsage = int32(l.CPUUsageMhz)
	qs.OverallCpuDemand = int32(l.CPUUsageMhz)
	qs.GuestMemoryUsage = int32(l.MemActiveMB)
	qs.HostMemoryUsage = int32(l.MemUsageMB)
	qs.GrantedMemory = int32(l.MemUsageMB)
	qs.ActiveMemory = int32(l.MemActiveMB)
	qs.SharedMemory = int32(l.MemSharedMB)
	qs.BalloonedMemory = int32(l.MemBalloonedMB)
	qs.SwappedMemory = int32(l.MemSwappedMB)
}
