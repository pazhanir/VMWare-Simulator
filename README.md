# VMware Demo Simulator (custom-vcsim)

A customized build of [govmomi](https://github.com/vmware/govmomi)'s `vcsim` that
simulates a **VMware vCenter 8.0.2** server at configurable scale, plus a REST API
for injecting failure scenarios. Built to test Site24x7's VMware monitoring
(discovery, metrics, topology, and alerting) without real ESXi hardware.

## What it does

- Serves the full vSphere SOAP API on **:443** (HTTPS) — a Site24x7 poller (or any
  govmomi/PowerCLI client) connects here as if it were a real vCenter.
- Exposes a **scenario controller REST API** on **:8990** to trigger realistic
  failure scenarios (CPU saturation, memory pressure, disk latency, network loss,
  host/VM events, cascades) against the simulated inventory.
- Builds a realistic, fully-connected inventory tree
  (Datacenter → Cluster → Host → VM, plus Datastores, Resource Pools, DVSwitches)
  with per-host hardware diversity (vendor/model/CPU/RAM profiles).

## Quick start

### Docker (recommended)

```bash
docker run -d --name custom-vcsim \
  -p 443:443 -p 8990:8990 \
  impazhani/custom-vcsim:latest
```

Or with docker-compose (see `docker-compose.yml`):

```bash
docker compose up -d
```

### Build from source

```bash
go build -o vcsim ./cmd/vcsim
./vcsim -l :443 -scenario-addr :8990
```

Default credentials: `administrator@vsphere.local` / `Site24x7!Demo`

## Inventory scaling

By default the simulator builds a large hardcoded 5-datacenter enterprise layout
(~4000 objects). Use the CLI flags to generate a flat, configurable layout instead:

| Flag        | Default | Meaning                              |
|-------------|---------|--------------------------------------|
| `-dc`       | 0       | Datacenters (0 = use the 5-DC layout)|
| `-cluster`  | 3       | Clusters per datacenter              |
| `-host`     | 6       | Hosts per cluster                    |
| `-vm`       | 100     | VMs per cluster                      |
| `-ds`       | 3       | Datastores per datacenter            |
| `-dvs`      | 1       | DVSwitches per datacenter            |

Example — a small but broad layout (2 DCs, 4 clusters, 12 hosts, 120 VMs):

```bash
./vcsim -dc 2 -cluster 2 -host 3 -vm 30 -ds 3 -dvs 1
```

Other flags: `-l` (vSphere listen addr), `-scenario-addr` (scenario API addr),
`-username`, `-password`, `-skip-inventory`.

## Scenario controller API (:8990)

| Method | Endpoint                      | Purpose                          |
|--------|-------------------------------|----------------------------------|
| GET    | `/api/scenarios`              | List available scenarios         |
| GET    | `/api/scenarios/active`       | List currently active scenarios  |
| POST   | `/api/scenario/activate`      | Activate a scenario on targets   |
| POST   | `/api/scenario/deactivate`    | Deactivate a scenario            |
| POST   | `/api/scenario/clear-all`     | Clear all active scenarios       |
| GET    | `/api/health`                 | Health check                     |

Example — saturate CPU on a host:

```bash
curl -X POST http://localhost:8990/api/scenario/activate \
  -H 'Content-Type: application/json' \
  -d '{"id":"cpu_host_saturation","targets":["/DC-1/host/Cluster-1/ESXi-1-1-01"]}'
```

## Project layout

```
cmd/vcsim/      Entry point — boots the VPX simulator + scenario controller
cmd/inspect/    Diagnostic: dumps topology relationships (parent links, UUIDs)
cmd/topoxml/    Diagnostic: replicates the Site24x7 poller's topology-build
                logic and reports whether the tree is fully connected
pkg/inventory/  Inventory builder + per-host hardware profiles
pkg/scenarios/  Failure scenario definitions and manager
pkg/overrides/  Thread-safe metric/property override registry
pkg/api/        Scenario controller REST server
```

## Customizations over stock vcsim

This build adds, on top of upstream govmomi `vcsim`:

- **Configurable, realistic inventory** with hardware diversity per host.
- **Metric override hooks** so `PerformanceManager.QueryPerf` returns injected
  scenario values instead of stock simulated data.
- **Scenario controller** for on-demand failure injection.
- **Compatibility fixes for Java/JAX-WS pollers** (vendored govmomi simulator):
  - Session re-authentication and a cookieless-session fallback.
  - Cluster/host DRS/DAS config defaults to prevent null-unboxing NPEs.
  - vAPI list endpoints return `{"value":[]}` (not `null`) for empty lists.

## Notes

- TLS uses a self-signed certificate — clients must skip verification.
- The vSphere API on :443 is wire-compatible with real vCenter 8.0.2.

## License & attribution

This project is licensed under the **Apache License 2.0** — see [`LICENSE`](LICENSE).

It includes and builds upon a **vendored copy of [govmomi](https://github.com/vmware/govmomi)**
(VMware vSphere API Go bindings and the `vcsim` simulator), copyright Broadcom Inc.
and/or its subsidiaries (formerly VMware, Inc.), also licensed under Apache 2.0.

Several vendored govmomi simulator files have been **modified** from their original
form (under `vendor/github.com/vmware/govmomi/`) to support Java/JAX-WS monitoring
clients:

- `simulator/simulator.go` — SOAP method whitelist, fault logging
- `simulator/session_manager.go` — session re-auth + cookieless fallback
- `simulator/cluster_compute_resource.go` — DRS/DAS config defaults
- `simulator/host_system.go` — cluster resource aggregation
- `vapi/simulator/simulator.go` — empty-list `"value":[]` serialization

Other bundled dependencies: `golang.org/x/text` and `github.com/google/uuid`
(both BSD-3-Clause).
