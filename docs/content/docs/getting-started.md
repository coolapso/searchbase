---
title: "Getting Started"
weight: 10
---

## 🚀 Installation

### 1. The Easy Way (Docker Compose)
The fastest way to test Searchbase locally is using the native DuckDuckGo demo Compose example:

```bash
git clone https://github.com/coolapso/searchbase.git
cd searchbase
docker compose -f examples/compose/docker-compose.native.yml up
```
The Gateway will be available at `http://localhost:8080`.

{{< hint warning >}}
The Compose files are demonstration examples only and are not intended for production deployments. See [Self Hosting](/docs/self-hosting/) for backend-specific examples and deployment notes.
{{< /hint >}}

### 2. Try the Kubernetes examples

The example manifests in `examples/k8s/` deploy the gateway with either
Crawl4AI or the experimental Lightpanda worker to the `default` namespace.
They are a starting point for evaluating Searchbase on Kubernetes, not a
production deployment. From the repository root, apply the Crawl4AI example:

```bash
kubectl apply -f examples/k8s/crawl-worker.yaml \
  -f examples/k8s/search-gateway.yaml
```

To use Lightpanda instead, apply this pair in the `default` namespace without
applying the Crawl4AI worker manifest:

```bash
kubectl apply -f examples/k8s/lightpanda-worker.yaml \
  -f examples/k8s/search-gateway.yaml
```

For either choice, access the internal gateway Service locally with:

```bash
kubectl port-forward svc/search-gateway 8080:8080
```

Both worker examples expose the `crawl-worker` Service expected by the gateway.
The gateway Service is internal; the port-forward exposes it on your machine
for testing. `examples/k8s/ddgs.yaml` is a commented placeholder and does not
deploy DDGS. For backend choices and deployment notes, see
[Self Hosting](/docs/self-hosting/).
