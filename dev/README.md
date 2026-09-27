# Development Compose stacks

`compose/` contains checkout-built stacks for development and isolated load
testing. User-facing Compose and Kubernetes demos belong in
`examples/compose/` and `examples/k8s/` at the repository root. Neither set is
a production deployment template.

From the repository root, use `task dev:up` for Lightpanda or
`task dev:upcrawl4ai` for Crawl4AI; `task dev:down` stops either stack. The
development tasks use the `searchbase-dev` Compose project. Load tests use
the separate `searchbase-loadtest` project through `task loadtest:run`.

Direct Compose commands must include `--project-directory .` from the
repository root so build contexts and the load-test result mount resolve
against the checkout. For example:

```bash
docker compose -p searchbase-dev --project-directory . \
  -f dev/compose/docker-compose.lightpanda.yml config
```
