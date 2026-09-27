import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import capacity_matrix as matrix


def sample(when, memory=100 * matrix.MIB):
    return {"at": when, "latency": {"p95_ms": 500}, "components": {
        "crawl-worker": {"working_set_bytes": memory, "memory_limit_bytes": 2 * 1024**3},
        "search-gateway": {"working_set_bytes": 20 * matrix.MIB,
                           "memory_limit_bytes": 512 * matrix.MIB}}}


class MatrixTests(unittest.TestCase):
    def test_bounds(self):
        self.assertEqual(matrix.positive_grid("4,2,4", 8, "CPU"), [2, 4])
        with self.assertRaises(Exception):
            matrix.positive_grid("9", 8, "CPU")

    def test_probe_rejects_slow_or_memory_bound(self):
        report = {"phases": [{"stable": True, "achieved_rate": 5.8}],
                  "latency": {"count": 60, "p95_ms": 850}, "errors": {},
                  "samples": [sample("2026-09-24T00:00:00Z")]}
        self.assertTrue(matrix.probe_stable(report, 6, 500)[0])
        report["latency"]["p95_ms"] = 1100
        self.assertFalse(matrix.probe_stable(report, 6, 500)[0])
        report["latency"]["p95_ms"] = 850
        report["samples"] = [sample("2026-09-24T00:00:00Z", 1900 * matrix.MIB)]
        self.assertFalse(matrix.probe_stable(report, 6, 500)[0])

    def test_aggregate_and_svg(self):
        entries = []
        for cpu, ram, capacity in ((2, 4, 2.0), (4, 8, 3.0)):
            entries.append({"scenario": "worker-javascript", "cpus": cpu,
                            "ram_gib": ram, "status": "measured", "safe_ops_s": capacity,
                            "peak_worker_mib": 800})
        cells = matrix.aggregate(entries)
        self.assertTrue(cells[1]["horizontal_proxy_wins"])
        self.assertEqual(cells[1]["two_replica_proxy_ops_s"], 4.0)
        manifest = {"entries": entries, "scenarios": ["worker-javascript"],
                    "cpus": [2, 4], "ram_gib": [4, 8]}
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            matrix.render(directory, manifest)
            self.assertIn("3.00 H", (directory / "capacity.svg").read_text())
            self.assertIn("sampled 800 MiB", (directory / "capacity.svg").read_text())
            self.assertIn("3.0", (directory / "capacity.csv").read_text())
            self.assertIn("idealized", (directory / "summary.md").read_text())

    def test_memory_display_uses_mib_until_one_gib(self):
        self.assertEqual(matrix.format_sampled_memory(19), "19 MiB")
        self.assertEqual(matrix.format_sampled_memory(1536), "1.5 GiB")
        self.assertEqual(matrix.format_sampled_memory(None), "memory unavailable")

    def test_unreached_saturation_is_not_a_diminishing_returns_result(self):
        entries = [{"scenario": "worker-static", "cpus": 2, "ram_gib": 4,
                    "status": "lower-bound", "safe_ops_s": 20, "peak_worker_mib": 400},
                   {"scenario": "worker-static", "cpus": 4, "ram_gib": 8,
                    "status": "measured", "safe_ops_s": 21, "peak_worker_mib": 500}]
        cells = matrix.aggregate(entries)
        self.assertTrue(cells[0]["lower_bound"])
        self.assertNotIn("cpu_gain_pct", cells[1])
        self.assertNotIn("horizontal_proxy_wins", cells[1])
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            manifest = {"entries": entries, "scenarios": ["worker-static"],
                        "cpus": [2, 4], "ram_gib": [4, 8]}
            matrix.render(directory, manifest)
            svg = (directory / "capacity.svg").read_text()
            self.assertIn("≥20.00", svg)
            self.assertIn("sampled 400 MiB", svg)
            self.assertIn("rate cap reached", svg)
            self.assertNotIn("0.0 GiB", svg)
            self.assertIn("≥20 | 400 MiB", (directory / "summary.md").read_text())

    def test_baseline_from_first_stage(self):
        report = {"phases": [{"ended_at": "2026-09-24T00:00:10Z"}],
                  "samples": [sample("2026-09-24T00:00:09Z"),
                              sample("2026-09-24T00:00:11Z")]}
        self.assertEqual(matrix.baseline_p95(report), 500)

    def test_midpoint_refinement_keeps_highest_stable_rate(self):
        discovery = {"phases": [
            {"target_rate": .25, "stable": True, "ended_at": "2026-09-24T00:00:10Z"},
            {"target_rate": 4, "stable": True, "ended_at": "2026-09-24T00:01:10Z"},
            {"target_rate": 8, "stable": False, "ended_at": "2026-09-24T00:02:10Z"}],
            "samples": [sample("2026-09-24T00:00:09Z")],
            "last_stable_rate": 3.8, "saturation_reason": "p95 latency above 2x baseline"}
        good = {"phases": [{"stable": True, "achieved_rate": 5.8}],
                "latency": {"count": 60, "p95_ms": 800}, "errors": {},
                "samples": [sample("2026-09-24T00:03:00Z")]}
        slow = {**good, "latency": {"count": 60, "p95_ms": 2000}}
        responses = iter([(("discover/report.json", discovery), None),
                          (("probe-1/report.json", good), None),
                          (("probe-2/report.json", slow), None)])
        entries = []
        args = SimpleNamespace(stage_duration="60s", refine_steps=2,
                               gateway_cpus=1.0, gateway_memory="512m",
                               worker_backend="crawl4ai")
        with patch.object(matrix, "run_task", side_effect=lambda *a, **kw: next(responses)):
            matrix.measure_cell("worker-javascript", 3, 16, 1, args,
                                lambda entry: entries.append(entry.copy()))
        self.assertEqual(entries[-1]["safe_ops_s"], 4.06)
        self.assertEqual(entries[-1]["last_stable_target"], 6)
        self.assertEqual(entries[-1]["first_unstable_target"], 7)


if __name__ == "__main__":
    unittest.main()
