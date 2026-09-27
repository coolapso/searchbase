---
title: "Crawl4AI or Lightpanda?"
description: "A practical choice between Searchbase's two fetch workers."
weight: 45
---

## Which should I choose?

**Pick Lightpanda when speed matters and its Markdown passes checks on your
pages.** At 4 worker CPUs / 2 GiB, the gateway completed at least 15.46
fetches/s with Lightpanda at the test ceiling, versus 6.73 with Crawl4AI.
**Otherwise keep Crawl4AI**, Searchbase's default, for its cleaned Markdown
and control over JavaScript through `js_render`. Lightpanda always runs
JavaScript.

The condition on Lightpanda matters: [issue #3661](https://github.com/lightpanda-io/browser/issues/3661)
reports that its Markdown dump included only 8 of 10 JavaScript-generated
quotes even though the rendered HTML contained all 10. Our load test checks
request success, **not content completeness**. Neither worker's output quality
was compared across websites.

## What did the saved test show?

At **4 worker CPUs and a 2 GiB worker limit**, fetching the same local
JavaScript page:

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
