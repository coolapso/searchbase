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
the generator uses the Docker Engine stats API as a memory-only fallback. The
generator mounts `/var/run/docker.sock` read-only and receives the host Docker
group ID only for this purpose. A read-only socket mount does not make the
Docker API read-only, so run this suite only from this trusted checkout on a
host you control; never add that mount to a production workload.

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

## Capacity workflow

Use `smoke` first to verify the local Docker/Chromium/exporter setup. Then use
`discover` on a quiet target VM; it warms up, doubles the scenario rate through
60-second stages, stops on persistent saturation, and stores the last stable
rate. `soak` holds the explicit `RATE` supplied from the discovery result for
30 minutes.

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

Read `summary.md` in the newest `loadtest/results/run-*/` directory. The answer
to “how many requests before it crumbles?” is the first unstable target rate;
use the reported `safe_capacity` rather than the last stable rate for a
production starting point. This test must be repeated from a separate generator
host before treating it as an instance-sizing decision.

For credible instance sizing, run the gateway and worker on the VM being sized
and run the same generator image from a second VM on its private network. Give
the runner the target URLs through `LOADTEST_GATEWAY`,
`LOADTEST_ISOLATED_GATEWAY`, and `LOADTEST_WORKER`, then use a non-`co-located`
`INSTANCE_LABEL` and `--comparable` when invoking the generator directly. Do
not treat a report labelled `co-located` as a capacity recommendation.

The runner marks a stage unstable after two sample intervals when unexpected
failures exceed 1%, p95 doubles from baseline, a scenario SLO is exceeded,
memory is above 85% of a limit, CPU throttling exceeds 5%, a target restarts or
OOMs, or the generator misses more than 1% of scheduled requests. The report's
safe sustained capacity is 70% of the last stable achieved rate. Use the first
limiting signal and its resource utilisation as the conservative scale-out
trigger; never infer cloud prices from these reports.

## Scenarios and reports

Scenario definitions live in `loadtest/scenarios/` and are JSON-compatible YAML
so they remain dependency-free and reviewable. Available core scenarios include
gateway REST search/fetch, direct static and JavaScript worker fetches, combined
REST fetches, Streamable HTTP MCP search/fetch, active and idle SSE MCP,
payload sweep, mixed traffic, and controlled failures. `core-mixed` is a
starting point for tuning; adjust a copied scenario to represent a known
production operation mix before using it for a scaling decision.

Compare reports from separate, compatible runs with no price estimation:

```bash
task loadtest:compare -- loadtest/results/run-a/report.json loadtest/results/run-b/report.json
```

Each result directory has:

- `report.json` — versioned `searchbase-load-report/v1` machine-readable data;
- `samples.csv` — periodic throughput, latency, active work, errors, and misses;
- `summary.md` — a concise human hand-off.

The report samples working-set memory every five seconds. `summary.md` shows
the peak for the gateway, worker (including Chromium child processes), fixture,
generator, exporters, and host; `samples.csv` includes gateway, worker, and
host memory columns. Fixture/exporter/generator readings diagnose benchmark
interference and are not treated as application capacity totals.

An interrupted, exporter-incomplete, reset, OOM/restart, or generator-overload
run is retained but must not be used for sizing.
