---
title: "Crawl4AI or Lightpanda?"
description: "A practical choice between Searchbase's two fetch workers."
weight: 45
---

## Which should I choose?

**Pick the worker that best fits your deployment.** At 4 worker CPUs / 2 GiB, the gateway completed at least 15.46
fetches/s with Lightpanda at the test ceiling, versus 6.73 with Crawl4AI.
The run used an older Lightpanda build and does not establish production
capacity. Crawl4AI remains the default and supports control over JavaScript
through `js_render`; Lightpanda always runs JavaScript.

The previously reported Lightpanda Markdown omission has been resolved and is
not a current user-facing limitation. Markdown fidelity and token efficiency
remain internal areas for regression testing and future optimization.

## What did the saved test show?

At **4 worker CPUs and a 2 GiB worker limit**, fetching the same local
JavaScript page:

These saved results used the previous pinned nightly build. The current
`0.4.1` release image has not been benchmarked here.

| Path | Crawl4AI | Lightpanda |
| :--- | ---: | ---: |
| Gateway `POST /api/v1/fetch` | 6.73 successful fetches/s | **≥15.46 successful fetches/s**; test capped at 16/s |
| Direct worker `POST /extract` | 7.48 HTTP 2xx responses/s | **≥29.96 HTTP 2xx responses/s**; test capped at 32/s |

The gateway result is the better basis for a choice: the gateway checks the
worker's `success` flag. The older direct-worker test counted HTTP 2xx without
checking that flag, so its ~30/s figure is **response throughput, not verified
successful extraction**. A separate 2 CPU / 2 GiB Lightpanda cell reached
30.94 HTTP 2xx responses/s under the same 32/s target. Crawl4AI did exceed
8/s in larger configurations; for example, its 8 CPU / 2 GiB direct-worker
cell reached 9.37 HTTP 2xx responses/s.

Lightpanda reached the rate cap in every gateway cell plotted below. This
means **its actual maximum was not measured**. The runs used one shared host,
one deterministic page, one run per setting, and short discovery stages. They
do not establish production capacity or content equivalence.

![Gateway fetch rates for Crawl4AI and Lightpanda at 2 and 4 GiB worker limits. Lightpanda reached the 16 requests per second test target in every plotted setting.](/images/worker-comparison.svg)

To rerun the tests on your hardware, see [Load testing](/development/load-testing/).
