---
title: "Load testing"
weight: 65
---

# Load testing and capacity discovery

Searchbase includes a deterministic, disposable capacity suite. It measures the
real `search-gateway`, `crawl-worker`, REST, and MCP paths without sending
traffic to a search provider or arbitrary website. It is deliberately separate
from the development Compose project.

{{< hint warning >}}
Run only one measured suite per host. The single-host setup is a functional
smoke test, not an instance-sizing result: its generator, exporters, and
application share CPU, memory, storage, and network resources.
{{< /hint >}}

## What the suite starts

`docker-compose.loadtest.yml` creates a project named `searchbase-loadtest`:

- the checkout's gateway and real Crawl4AI/Chromium worker;
- a fixture service that supplies DDGS-shaped search results, a mock extractor,
  and static/JavaScript pages only on the private benchmark network;
- cAdvisor and node exporter for direct Prometheus-format resource sampling;
- an optional generator container, used by the task commands.

The fixture rejects paths outside its fixed local pages. The private benchmark
network has no Internet route. Queries, fetched URLs, request bodies, and
response bodies are never written to reports.

The cAdvisor and node-exporter host mounts are necessary for resource readings.
Their published ports bind to `127.0.0.1` by default (`18081` and `19100`),
never a public interface. Set `LOADTEST_BIND_ADDRESS` only to a private,
firewalled interface when a remote observer is required.

On Docker Engine versions where cAdvisor cannot resolve container layer metadata,
the fixture acts as a target-side collector. It reads Docker's stats/inspect API
and exposes only aggregate CPU, CPU quota/throttling, working-set memory,
memory limit, restart, and OOM metrics at `/docker-metrics` (port `18082`).
The fixture mounts `/var/run/docker.sock` read-only, which **does not** make
Docker API operations read-only. Run this trusted test-only project on a host
you control, restrict port `18082` to your private generator VM, and never put
the socket or collector in a production workload. The generator has no Docker
socket. cAdvisor is still used for network and block-I/O counters when its
container metrics are available.

## First run

Install Docker Engine with the Compose plugin and [Task](https://taskfile.dev/).
The worker image builds Chromium and Crawl4AI dependencies, so the first build
can take some time and needs enough disk space.

```bash
# From the repository root. This is a 15-second low-load functional check.
task loadtest:run

# Select any named scenario and retain generated reports under loadtest/results/.
task loadtest:run SCENARIO=core-mcp-http-search PROFILE=smoke
```

The command recreates only `searchbase-loadtest`, runs the generator, writes
`report.json`, `samples.csv`, and `summary.md`, then removes its containers and
volumes. It does not start, stop, or reuse `docker-compose.yml` development
services. If a run is interrupted, clean up its exact project with:

```bash
task loadtest:down
```

The runner shows a live aggregate-only dashboard with throughput and latency
sparklines plus one resource row per component when attached to a terminal.
Piped/non-interactive output is plain periodic text. Both omit request inputs.

To test the optional Lightpanda worker against the same private fixtures,
select its build context. The `crawl-worker` service name stays the same so
gateway routing and resource sampling remain compatible. Distinguish this
backend in `INSTANCE_LABEL` when running one scenario directly:

```bash
LOADTEST_WORKER_CONTEXT=./lightpanda-worker \
  task loadtest:run SCENARIO=worker-javascript PROFILE=discover \
  INSTANCE_LABEL=co-located-lightpanda-4cpu-8g
```

Set the same CPU/RAM environment limits as for Crawl4AI when comparing them.
For direct worker scenarios, the generator counts JSON `success:false` as a
failure even when the worker returns HTTP 200. Lightpanda always executes
JavaScript, but the `worker-static` and `worker-javascript` scenarios still use
different fixture pages; do not treat them as an isolated `js_render` toggle.
Older direct-worker reports may have counted such HTTP-200 extraction failures
as successes, so rerun Crawl4AI baselines before comparing capacity numbers.

Result directories are named with the UTC start time, scenario, profile, and
instance label, for example
`20260924-151828-178366035-worker-javascript-discover-crawler-worker-javascript-8g-2cpu/`.
They sort by start time. Older `run-<number>/` directories retain their names;
their `summary.md` and `report.json` identify the workload.

## Capacity workflow

Use `smoke` first to verify the local Docker/Chromium/exporter setup. Then use
`discover` on a quiet target VM; it warms up for 30 seconds, doubles the
scenario rate through 60-second stages, then tests up to five binary midpoints
between the last stable and first unstable target. It cools down for five
seconds. `soak` holds the explicit `RATE` supplied from a previous discovery
report for 30 minutes, then cools down. `soak` now rejects a missing `RATE`.
`STAGE_DURATION` overrides the measured stage duration for short development
checks; do not use abbreviated stages for sizing.

```bash
task loadtest:run SCENARIO=core-rest-fetch-static PROFILE=discover INSTANCE_LABEL=vm-4vcpu-8gb
# Use the prior discovery report's safe_capacity as RATE for the soak run.
task loadtest:run SCENARIO=core-rest-fetch-javascript PROFILE=soak RATE=1.25 INSTANCE_LABEL=vm-4vcpu-8gb
task loadtest:suite PROFILE=smoke
```

### Reproducing a constrained 2 GiB host profile

For a first-pass estimate where the worker receives 1 GiB and the gateway 512
MiB, run the end-to-end JavaScript path. The remaining host capacity is for the
operating system, Docker, fixture, generator, and observers; it is not a
production reservation.

```bash
LOADTEST_WORKER_MEMORY=1g LOADTEST_GATEWAY_MEMORY=512m \
  task loadtest:run SCENARIO=core-rest-fetch-javascript PROFILE=discover \
  STAGE_DURATION=60s INSTANCE_LABEL=co-located-2gb-profile
```

Read `summary.md` in the newest directory under `loadtest/results/`. The answer
to “how many requests before it crumbles?” is the first unstable target rate;
use the reported `safe_capacity` rather than the last stable rate for a
production starting point. This test must be repeated from a separate generator
host before treating it as an instance-sizing decision.

For credible instance sizing, run the gateway and worker on the target VM and
run the generator from a second quiet VM on the same private network. On the
target, bind the benchmark ports only to its private IP and firewall them to
the generator VM:

```bash
LOADTEST_BIND_ADDRESS=10.0.0.10 task loadtest:up
# When finished: task loadtest:down
```

On the generator VM, use the same checkout and Go toolchain. Replace the
example IP with the target's private IP:

```bash
cd loadtest
LOADTEST_GATEWAY=http://10.0.0.10:18080 \
LOADTEST_CADVISOR=http://10.0.0.10:18081/metrics \
LOADTEST_DOCKER_METRICS=http://10.0.0.10:18082/docker-metrics \
LOADTEST_NODE_EXPORTER=http://10.0.0.10:19100/metrics \
LOADTEST_GIT_REVISION="$(git -C .. rev-parse HEAD)" \
go run ./cmd/loadtest --scenario core-rest-fetch-javascript --profile discover \
  --scenarios ./scenarios --output ./results \
  --instance-label target-4vcpu-8g --comparable
```

The collector and exporters are read-only HTTP endpoints from the generator's
point of view, but the target-side collector still holds the powerful Docker
socket. The runner accepts only fixed Compose service names or private/loopback
IP targets. The standard Compose file publishes the gateway, not the direct
worker or isolated gateway; use the gateway REST/MCP scenarios in this remote
workflow unless you deliberately expose those internal services on a private,
firewalled interface. Do not treat a `co-located` report as a production
capacity recommendation.

The runner marks a stage unstable after two sample intervals when unexpected
failures exceed 1%, p95 doubles from baseline, a scenario SLO is exceeded,
target CPU or memory exceeds 85% of its quota/limit, host memory exceeds 85%,
CPU throttling exceeds 5% of scheduled periods, a target restarts or OOMs, or
the generator misses more than 1% of scheduled requests. The report's
safe sustained capacity is 70% of the last stable achieved rate. Use the first
limiting signal and its resource utilisation as the conservative scale-out
trigger; never infer cloud prices from these reports.

## Scenarios and reports

Scenario definitions live in `loadtest/scenarios/` as strictly validated YAML.
Available core scenarios include
gateway REST search/fetch, direct static and JavaScript worker fetches, combined
REST fetches, Streamable HTTP MCP search/fetch, active and idle SSE MCP,
payload sweep, mixed traffic, and controlled failures. `core-mixed` is a
starting point for tuning; adjust a copied scenario to represent a known
production operation mix before using it for a scaling decision. The supplied
`core-mixed` weights are 40% REST search, 30% MCP HTTP search, 25% static REST
fetch, and 5% JavaScript REST fetch. `payload-sweep` warms the worker for 15
seconds, then runs each of three operation types at small, medium, and large fixture payload sizes; it and
`controlled-failure` are non-sizing scenarios. Idle SSE discovery counts open
sessions (100, 200, 400, etc.), not requests per second.

Run diagnostics separately from the capacity suite with
`task loadtest:diagnostics`.

Compare reports from separate, compatible runs with no price estimation:

```bash
task loadtest:compare -- "loadtest/results/<first-run>/report.json" "loadtest/results/<second-run>/report.json"
```

Each result directory has:

- `report.json` — versioned `searchbase-load-report/v1` machine-readable data;
- `samples.csv` — periodic throughput, latency, active work, errors, and misses;
- `summary.md` — a concise human hand-off.

The report samples working-set memory and CPU counters every five seconds.
Latency uses a bounded logarithmic histogram (at most about 1% bucket width)
so a long soak does not retain every request duration. `summary.md` shows
the peak for the gateway, worker (including Chromium child processes), fixture,
generator, exporters, and host; `samples.csv` includes gateway, worker, and
host memory and gateway/worker CPU columns. Fixture/exporter/generator readings diagnose benchmark
interference and are not treated as application capacity totals.

An interrupted, exporter-incomplete, reset, OOM/restart, or generator-overload
run is retained with `invalid_reasons` and `safe_capacity: 0`; it must not be
used for sizing. A smoke report is functional evidence only, even if it has a
nonzero calculated safe value. Stage `achieved_rate` divides successful
operations by the scheduled stage window, not by resource-sampling time.

## Repeatable CPU and RAM matrix

`task loadtest:matrix` runs the isolated stack sequentially across a worker
CPU/RAM grid, up to 8 CPUs and 16 GiB. It does not change the host's physical
memory allocation: the values are Docker container limits. Its default grid is
1, 2, 3, 4, 6, and 8 worker CPUs × 2, 4, 8, 12, and 16 GiB, for each of
`worker-static`, `worker-javascript`, and `core-rest-fetch-javascript`. That is
90 configurations before optional repeats and can take many hours; start with
a smaller grid. Run only one matrix at a time on an otherwise quiet host.

```bash
# Preview the commands without starting containers.
task loadtest:matrix -- --cpus 2,3,4 --ram-gib 4,8,16 \
  --scenarios worker-javascript,core-rest-fetch-javascript --dry-run

# Run the chosen grid. Every cell starts with discovery, then two fixed-rate
# midpoint probes between the last stable and first unstable discovery rates.
task loadtest:matrix -- --cpus 2,3,4 --ram-gib 4,8,16 \
  --scenarios worker-javascript,core-rest-fetch-javascript \
  --stage-duration 60s --refine-steps 2

# Run the same matrix against the optional Lightpanda worker.
task loadtest:matrix -- --worker-backend lightpanda --cpus 2,3,4 \
  --ram-gib 4,8,16 --scenarios worker-javascript,core-rest-fetch-javascript

# Rebuild a graph from a saved or interrupted matrix without running Docker.
task loadtest:matrix -- --plot-only loadtest/results/matrix-YYYYMMDD-HHMMSS
```

Use `--repeats 3` to run each configuration three times; the plotted value is
the median of valid repeats. The script randomizes cell order with a fixed seed
(`--seed`) to reduce time/order bias. It leaves each raw `report.json` in its
normal named run directory. A separate `loadtest/results/matrix-*/` directory
contains `matrix.json` (progress and links to raw reports), `capacity.csv`,
`summary.md`, and `capacity.svg` (open in a browser). Progress and the graph
are saved after each run, including on interruption. Failed or invalid cells
are shown as missing, not zero.

The graph labels each cell with a conservative safe successful-request rate,
the discovery run's highest sampled worker working set (MiB below 1 GiB),
and marginal gains from the next lower tested CPU (`C`) and RAM (`M`) setting.
The memory figure is sampled every five seconds, so brief browser subprocess
peaks can be missed; do not use it alone for RAM sizing. `capacity.csv` carries
the exact sampled MiB values.
Gains near zero or negative are candidates for diminishing
returns, **not proof** without repeats. The script checks fixed-rate probes
against the low-load p95 baseline, request errors, achieved target rate, and
memory measurements. Discovery still uses doubled rates, so even refined
capacity is an estimate, not an exact cliff or 30-minute endurance result.
Cells marked `≥` reached the scenario's maximum test rate without saturation;
they use neutral coloring and are lower bounds, not measured capacity limits.
They are excluded from marginal-gain and horizontal-proxy
decisions. Raise the scenario's `max_rate` in a copied scenario and rerun if
those cells matter.

Cells marked `H` compare one large worker to an *idealized* pair of smaller
workers with the same total worker CPU and RAM. This assumes linear scaling and
does not measure two replicas, their gateway/host overhead, load balancing,
or any price. Treat it as a candidate for a later real multi-replica test,
not a recommendation to scale horizontally. On this co-located setup, the
generator and observers also contend with the services; repeat promising
configurations with a separate generator VM before making hosting decisions.
