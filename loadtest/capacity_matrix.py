#!/usr/bin/env python3
"""Run a bounded Searchbase capacity grid and render dependency-free SVG reports."""

import argparse
import csv
import html
import json
import math
import os
import random
import shutil
import statistics
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
RESULTS = ROOT / "loadtest" / "results"
MIB = 1024 * 1024
SCENARIOS = ("worker-static", "worker-javascript", "core-rest-fetch-javascript")


def positive_grid(value, maximum, name):
    try:
        numbers = sorted(set(int(part) for part in value.split(",")))
    except ValueError as exc:
        raise argparse.ArgumentTypeError(f"{name} must be comma-separated integers") from exc
    if not numbers or any(n < 1 or n > maximum for n in numbers):
        raise argparse.ArgumentTypeError(f"{name} must be between 1 and {maximum}")
    return numbers


def duration(value):
    if not value.endswith("s") or not value[:-1].isdigit() or int(value[:-1]) < 15:
        raise argparse.ArgumentTypeError("stage duration must be at least 15s, e.g. 60s")
    return value


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cpus", default="1,2,3,4,6,8")
    parser.add_argument("--ram-gib", default="2,4,8,12,16")
    parser.add_argument("--scenarios", default=",".join(SCENARIOS))
    parser.add_argument("--worker-backend", choices=("crawl4ai", "lightpanda"),
                        default="crawl4ai")
    parser.add_argument("--stage-duration", type=duration, default="60s")
    parser.add_argument("--refine-steps", type=int, default=2,
                        help="fixed-rate midpoint probes between stable and unstable discovery rates")
    parser.add_argument("--repeats", type=int, default=1)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--gateway-memory", default="512m")
    parser.add_argument("--gateway-cpus", type=float, default=1.0)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--plot-only", type=Path, metavar="MATRIX_DIR")
    args = parser.parse_args(argv)
    args.cpus = positive_grid(args.cpus, 8, "CPUs")
    args.ram_gib = positive_grid(args.ram_gib, 16, "RAM GiB")
    args.scenarios = list(dict.fromkeys(args.scenarios.split(",")))
    available = {p.stem for p in (ROOT / "loadtest" / "scenarios").glob("*.yaml")}
    if not args.scenarios or any(s not in available for s in args.scenarios):
        parser.error(f"scenarios must be names under loadtest/scenarios/: {sorted(available)}")
    if args.refine_steps < 0 or args.refine_steps > 6 or args.repeats < 1 or args.repeats > 10:
        parser.error("refine-steps must be 0-6 and repeats must be 1-10")
    if args.gateway_cpus <= 0 or not args.gateway_memory:
        parser.error("gateway limits must be positive")
    return args


def report_paths():
    return set(RESULTS.glob("*/report.json"))


def preflight():
    for binary in ("task", "docker"):
        if not shutil.which(binary):
            raise RuntimeError(f"{binary} is required")
    check = subprocess.run(
        ["docker", "compose", "-p", "searchbase-loadtest", "--project-directory", ".",
         "-f", "dev/compose/docker-compose.loadtest.yml", "ps", "--status", "running", "-q"],
        cwd=ROOT, text=True, capture_output=True, check=False)
    if check.returncode:
        raise RuntimeError(f"Docker Compose unavailable: {check.stderr.strip()}")
    if check.stdout.strip():
        raise RuntimeError("searchbase-loadtest already has running containers; finish that run first")


def run_task(scenario, cpu, ram, repeat, profile, seconds, label, args, rate=None):
    env = os.environ.copy()
    env.update(LOADTEST_WORKER_CPUS=str(cpu), LOADTEST_WORKER_MEMORY=f"{ram}g",
               LOADTEST_WORKER_CONTEXT=("./lightpanda-worker" if args.worker_backend == "lightpanda"
                                        else "./crawl-worker"),
               LOADTEST_GATEWAY_CPUS=str(args.gateway_cpus),
               LOADTEST_GATEWAY_MEMORY=args.gateway_memory)
    command = ["task", "loadtest:run", f"SCENARIO={scenario}", f"PROFILE={profile}",
               f"STAGE_DURATION={seconds}", f"INSTANCE_LABEL={label}"]
    if rate is not None:
        command.append(f"RATE={rate:.4f}")
    if args.dry_run:
        print(" ".join(f"{key}={env[key]}" for key in (
            "LOADTEST_WORKER_CONTEXT", "LOADTEST_WORKER_CPUS", "LOADTEST_WORKER_MEMORY", "LOADTEST_GATEWAY_CPUS",
            "LOADTEST_GATEWAY_MEMORY")), " ".join(command), flush=True)
        return None, None
    before = report_paths()
    print(f"\n[{scenario} {cpu} CPU {ram} GiB repeat {repeat}] {profile} "
          f"{'' if rate is None else f'{rate:.2f} req/s'}", flush=True)
    status = subprocess.run(command, cwd=ROOT, env=env, check=False).returncode
    candidates = []
    for path in report_paths() - before:
        try:
            report = json.loads(path.read_text())
            if report.get("instance_label") == label and report.get("profile") == profile:
                candidates.append((path, report))
        except (OSError, ValueError):
            pass
    if len(candidates) != 1:
        return None, f"task exit {status}; expected one new report, found {len(candidates)}"
    path, report = candidates[0]
    if status != 0:
        return None, f"task exit {status}; report at {path}"
    if report.get("schema_version") != "searchbase-load-report/v1":
        return None, f"unsupported report schema at {path}"
    return (str(path.relative_to(ROOT)), report), None


def peak_memory(report, service="crawl-worker"):
    values = [s.get("components", {}).get(service, {}) for s in report.get("samples", [])]
    return max((v.get("working_set_bytes", 0) for v in values), default=0)


def format_sampled_memory(mib):
    if mib is None or mib <= 0:
        return "memory unavailable"
    if mib < 1024:
        return f"{mib:g} MiB"
    return f"{mib / 1024:.1f} GiB"


def baseline_p95(report):
    phases = report.get("phases", [])
    if not phases:
        return 0
    end = phases[0]["ended_at"]
    samples = [s for s in report.get("samples", []) if s["at"] <= end]
    return samples[-1].get("latency", {}).get("p95_ms", 0) if samples else 0


def probe_stable(report, target_rate, baseline):
    if report.get("invalid_reasons"):
        return False, "invalid report"
    phases = report.get("phases", [])
    if len(phases) != 1 or not phases[0].get("stable"):
        return False, "runner marked stage unstable"
    phase = phases[0]
    if phase.get("achieved_rate", 0) < .9 * target_rate:
        return False, "achieved less than 90% of target"
    latency = report.get("latency", {})
    count = latency.get("count", 0)
    errors = report.get("errors", {}).get("total", 0)
    if count == 0 or errors / count > .01:
        return False, "errors above 1% or no requests"
    if baseline <= 0 or latency.get("p95_ms", math.inf) > 2 * baseline:
        return False, "p95 above twice low-load baseline"
    samples = report.get("samples", [])
    if not samples or any(s.get("observer_health") for s in samples):
        return False, "missing observer data"
    for service in ("crawl-worker", "search-gateway"):
        parts = [s.get("components", {}).get(service, {}) for s in samples]
        if not all(p.get("working_set_bytes", 0) > 0 and p.get("memory_limit_bytes", 0) > 0 for p in parts):
            return False, f"missing {service} memory measurement"
        if any(p["working_set_bytes"] / p["memory_limit_bytes"] > .85 for p in parts):
            return False, f"{service} memory above 85%"
    return True, "stable under matrix criteria"


def discover_stable(report):
    if report.get("invalid_reasons") or baseline_p95(report) <= 0:
        return False
    return any(p.get("stable") for p in report.get("phases", []))


def measure_cell(scenario, cpu, ram, repeat, args, save):
    base = f"co-located-{args.worker_backend}-{cpu}cpu-{ram}g-r{repeat}"
    entry = {"scenario": scenario, "cpus": cpu, "ram_gib": ram, "repeat": repeat,
             "gateway_cpus": args.gateway_cpus, "gateway_memory": args.gateway_memory,
             "runs": [], "status": "incomplete", "safe_ops_s": None}
    save(entry)
    label = base + "-discover"
    result, error = run_task(scenario, cpu, ram, repeat, "discover", args.stage_duration, label, args)
    entry["runs"].append({"profile": "discover", "report": result[0] if result else None,
                          "error": error})
    save(entry)
    if error or not discover_stable(result[1]):
        entry["status"] = error or "invalid or no stable discovery stage"
        save(entry)
        return
    report = result[1]
    baseline = baseline_p95(report)
    low = max((p["target_rate"] for p in report["phases"] if p.get("stable")), default=0)
    high = min((p["target_rate"] for p in report["phases"] if not p.get("stable")), default=0)
    best_achieved = report["last_stable_rate"]
    best_report = result[0]
    if low <= 0:
        entry["status"] = "no stable discovery rate"
        save(entry)
        return
    if not high:
        entry.update(status="lower-bound", safe_ops_s=round(best_achieved * .7, 4),
                     last_stable_target=round(low, 4), baseline_p95_ms=round(baseline, 2),
                     representative_report=best_report,
                     peak_worker_mib=round(peak_memory(report) / MIB, 1),
                     discovery_saturation="not reached within scenario max_rate")
        save(entry)
        return
    for step in range(args.refine_steps):
        rate = (low + high) / 2
        label = base + f"-probe-{step + 1}"
        probe, error = run_task(scenario, cpu, ram, repeat, "soak", args.stage_duration,
                                label, args, rate)
        stable, reason = probe_stable(probe[1], rate, baseline) if probe else (False, error)
        entry["runs"].append({"profile": "probe", "target_rate": rate,
                              "report": probe[0] if probe else None, "stable": stable,
                              "reason": reason})
        save(entry)
        if stable:
            low = rate
            best_achieved = max(best_achieved, probe[1]["phases"][0]["achieved_rate"])
            best_report = probe[0]
        else:
            high = rate
    entry.update(status="measured", safe_ops_s=round(best_achieved * .7, 4),
                 last_stable_target=round(low, 4), first_unstable_target=round(high, 4),
                 baseline_p95_ms=round(baseline, 2), representative_report=best_report,
                 peak_worker_mib=round(peak_memory(report) / MIB, 1),
                 discovery_saturation=report.get("saturation_reason", ""))
    save(entry)


def svg_chart(cells, scenarios, cpus, ram_gib, path):
    width = 180 + 126 * len(cpus)
    panel_height = 110 + 78 * len(ram_gib)
    height = 95 + panel_height * len(scenarios)
    pieces = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" '
              f'viewBox="0 0 {width} {height}" role="img" aria-label="Searchbase capacity matrix">',
              '<rect width="100%" height="100%" fill="#f8fafc"/>',
              '<style>text{font-family:system-ui,sans-serif;fill:#172033}.title{font-size:20px;font-weight:700}'
              '.head{font-size:13px;font-weight:600}.cell{font-size:15px;font-weight:700}'
              '.small{font-size:10px}.muted{fill:#64748b}</style>',
              '<text x="20" y="30" class="title">Worker capacity: safe successful requests/s</text>',
              '<text x="20" y="49" class="small muted">Co-located estimate. ≥ = test ceiling reached, not a measured limit. H = hypothetical two-worker projection.</text>',
              '<text x="20" y="64" class="small muted">Memory = highest 5-second sampled working set; brief browser-process peaks may be missed.</text>']
    lookup = {(x["scenario"], x["cpus"], x["ram_gib"]): x for x in cells}
    for panel, scenario in enumerate(scenarios):
        top = 85 + panel * panel_height
        scenario_cells = [x for x in cells if x["scenario"] == scenario]
        max_capacity = max((x.get("safe_ops_s") or 0 for x in scenario_cells
                            if not x.get("lower_bound")), default=0)
        all_capped = scenario_cells and all(x.get("lower_bound") for x in scenario_cells)
        suffix = " — all cells reached the test ceiling" if all_capped else ""
        pieces.append(f'<text x="20" y="{top + 15}" class="head">{html.escape(scenario + suffix)}</text>')
        for ci, cpu in enumerate(cpus):
            pieces.append(f'<text x="{158 + ci * 126}" y="{top + 39}" class="head">{cpu} CPU</text>')
        for ri, ram in enumerate(ram_gib):
            y = top + 48 + ri * 78
            pieces.append(f'<text x="20" y="{y + 26}" class="head">{ram} GiB</text>')
            for ci, cpu in enumerate(cpus):
                x = 135 + ci * 126
                cell = lookup.get((scenario, cpu, ram))
                value = cell.get("safe_ops_s") if cell else None
                intensity = value / max_capacity if value is not None and max_capacity else 0
                shade = int(232 - 135 * intensity)
                fill = "#dbeafe" if cell and cell.get("lower_bound") else (
                    f'rgb({shade},{min(248, shade + 12)},{min(253, shade + 30)})'
                    if value is not None else "#e2e8f0")
                border = ' stroke="#60a5fa" stroke-dasharray="4 3"' if cell and cell.get("lower_bound") else ""
                pieces.append(f'<rect x="{x}" y="{y}" width="118" height="70" rx="6" fill="{fill}"{border}/>')
                if value is None:
                    pieces.append(f'<text x="{x + 8}" y="{y + 36}" class="small">no valid result</text>')
                    continue
                hint = " H" if cell.get("horizontal_proxy_wins") else ""
                cpu_gain = cell.get("cpu_gain_pct")
                ram_gain = cell.get("ram_gain_pct")
                gains = f'C:{cpu_gain:+.0f}%' if cpu_gain is not None else 'C:—'
                gains += f' M:{ram_gain:+.0f}%' if ram_gain is not None else ' M:—'
                capacity = f"≥{value:.2f}" if cell.get("lower_bound") else f"{value:.2f}{hint}"
                detail = "rate cap reached" if cell.get("lower_bound") else gains
                memory = format_sampled_memory(cell.get("peak_worker_mib"))
                memory_label = f"sampled {memory}" if memory != "memory unavailable" else memory
                pieces.append(f'<text x="{x + 8}" y="{y + 22}" class="cell">{capacity}</text>')
                pieces.append(f'<text x="{x + 8}" y="{y + 42}" class="small">{memory_label}</text>')
                pieces.append(f'<text x="{x + 8}" y="{y + 60}" class="small">{detail}</text>')
    pieces.append('</svg>')
    path.write_text("\n".join(pieces) + "\n")


def aggregate(entries):
    grouped = {}
    for entry in entries:
        key = (entry["scenario"], entry["cpus"], entry["ram_gib"])
        grouped.setdefault(key, []).append(entry)
    cells = []
    for (scenario, cpu, ram), group in sorted(grouped.items()):
        valid = [e for e in group if e.get("status") in ("measured", "lower-bound")]
        capacities = [e["safe_ops_s"] for e in valid]
        cell = {"scenario": scenario, "cpus": cpu, "ram_gib": ram,
                "valid_repeats": len(valid), "total_repeats": len(group),
                "lower_bound": any(e.get("status") == "lower-bound" for e in valid),
                "safe_ops_s": round(statistics.median(capacities), 4) if capacities else None,
                "peak_worker_mib": max((e.get("peak_worker_mib", 0) for e in valid), default=0) or None}
        cells.append(cell)
    lookup = {(c["scenario"], c["cpus"], c["ram_gib"]): c for c in cells}
    for cell in cells:
        smaller = lookup.get((cell["scenario"], cell["cpus"] // 2, cell["ram_gib"] // 2))
        if cell["cpus"] % 2 == 0 and cell["ram_gib"] % 2 == 0 and smaller:
            capacity = smaller.get("safe_ops_s")
            if capacity is not None and not cell["lower_bound"] and not smaller["lower_bound"]:
                cell["two_replica_proxy_ops_s"] = round(2 * capacity, 4)
                cell["horizontal_proxy_wins"] = (
                    cell["safe_ops_s"] is not None and 2 * capacity >= cell["safe_ops_s"] * 1.15)
        previous_cpu = max((c for c in lookup if c[0] == cell["scenario"] and c[2] == cell["ram_gib"]
                            and c[1] < cell["cpus"]), key=lambda c: c[1], default=None)
        previous_ram = max((c for c in lookup if c[0] == cell["scenario"] and c[1] == cell["cpus"]
                            and c[2] < cell["ram_gib"]), key=lambda c: c[2], default=None)
        for axis, previous in (("cpu", previous_cpu), ("ram", previous_ram)):
            old = lookup[previous].get("safe_ops_s") if previous else None
            if old and cell["safe_ops_s"] is not None and not cell["lower_bound"] \
                    and not lookup[previous]["lower_bound"]:
                cell[f"{axis}_gain_pct"] = round(100 * (cell["safe_ops_s"] / old - 1), 1)
    return cells


def render(directory, manifest):
    entries = manifest["entries"]
    cells = aggregate(entries)
    with (directory / "capacity.csv").open("w", newline="") as stream:
        columns = ("scenario", "cpus", "ram_gib", "valid_repeats", "total_repeats",
                   "safe_ops_s", "lower_bound", "peak_worker_mib", "cpu_gain_pct", "ram_gain_pct",
                   "two_replica_proxy_ops_s", "horizontal_proxy_wins")
        writer = csv.DictWriter(stream, fieldnames=columns)
        writer.writeheader()
        writer.writerows({key: cell.get(key, "") for key in columns} for cell in cells)
    svg_chart(cells, manifest["scenarios"], manifest["cpus"], manifest["ram_gib"],
              directory / "capacity.svg")
    lines = ["# Searchbase capacity matrix", "", "Single-host, co-located estimates, not production sizing.",
             "Each cell is the median of valid repeats of 70% of the highest stable achieved rate.",
             "Missing/invalid cells are excluded, not treated as zero. ≥ marks a lower bound",
             "when discovery reached the scenario's maximum rate without saturation.",
             "Worker memory is the highest sampled working set, shown in MiB below 1 GiB;",
             "five-second sampling can miss brief Lightpanda child-process peaks.",
             "See `capacity.csv` for marginal gains; bounds are not used for gain/proxy decisions.",
             "", "`H` on the SVG means two smaller worker configurations project at least 15% more",
             "safe operations/s at the same total worker CPU and RAM. This is an idealized",
             "linear scale-out reference, **not a measured multi-replica result**. Gateway,",
             "fixture, host overhead and real load-balancing can change the comparison.", "",
             "Discovery doubles rates; midpoint probes narrow the bracket. These are short",
             "tests, not long-term endurance guarantees. Saturation and p95 are in each raw report.", "",
             f"Matrix: {len(cells)} configurations, {len(entries)} attempts, "
             f"{sum(c['safe_ops_s'] is not None for c in cells)} with valid capacity estimates.", "",
             "| Scenario | CPU | RAM GiB | Safe req/s | Sampled worker peak | CPU gain | RAM gain | 2-worker proxy | Valid repeats |",
             "|---|---:|---:|---:|---:|---:|---:|---:|---:|"]
    for c in cells:
        fmt = lambda v, suffix="": "—" if v is None else f"{v}{suffix}"
        capacity = f"≥{c['safe_ops_s']}" if c["lower_bound"] else fmt(c["safe_ops_s"])
        lines.append(f"| {c['scenario']} | {c['cpus']} | {c['ram_gib']} | "
                     f"{capacity} | {format_sampled_memory(c.get('peak_worker_mib'))} | "
                     f"{fmt(c.get('cpu_gain_pct'), '%')} | "
                     f"{fmt(c.get('ram_gain_pct'), '%')} | "
                     f"{fmt(c.get('two_replica_proxy_ops_s'))} | "
                     f"{c['valid_repeats']}/{c['total_repeats']} |")
    (directory / "summary.md").write_text("\n".join(lines) + "\n")


def main(argv=None):
    args = arguments(argv)
    if args.plot_only:
        directory = args.plot_only.resolve()
        manifest = json.loads((directory / "matrix.json").read_text())
        render(directory, manifest)
        print(f"Graph: {directory / 'capacity.svg'}")
        return 0
    configs = [(scenario, cpu, ram, repeat) for scenario in args.scenarios
               for cpu in args.cpus for ram in args.ram_gib
               for repeat in range(1, args.repeats + 1)]
    random.Random(args.seed).shuffle(configs)
    if args.dry_run:
        print(f"{len(configs)} configurations; discovery plus up to {args.refine_steps} probes each")
        for scenario, cpu, ram, repeat in configs:
            label = f"co-located-{args.worker_backend}-{cpu}cpu-{ram}g-r{repeat}-discover"
            run_task(scenario, cpu, ram, repeat, "discover", args.stage_duration, label, args)
        return 0
    try:
        preflight()
    except RuntimeError as exc:
        print(exc, file=sys.stderr)
        return 2
    matrix_id = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S-%f")
    directory = RESULTS / f"matrix-{args.worker_backend}-{matrix_id}"
    directory.mkdir(parents=True, exist_ok=False)
    manifest = {"schema_version": "searchbase-capacity-matrix/v1", "matrix_id": matrix_id,
                "worker_backend": args.worker_backend,
                "scenarios": args.scenarios, "cpus": args.cpus, "ram_gib": args.ram_gib,
                "stage_duration": args.stage_duration, "refine_steps": args.refine_steps,
                "repeats": args.repeats, "seed": args.seed, "entries": []}

    def save(entry):
        if entry not in manifest["entries"]:
            manifest["entries"].append(entry)
        (directory / "matrix.json").write_text(json.dumps(manifest, indent=2) + "\n")
        render(directory, manifest)

    try:
        for scenario, cpu, ram, repeat in configs:
            measure_cell(scenario, cpu, ram, repeat, args, save)
    except KeyboardInterrupt:
        print("Interrupted; partial matrix and graph retained", file=sys.stderr)
        return 130
    print(f"Graph: {directory / 'capacity.svg'}")
    print(f"Summary: {directory / 'summary.md'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
