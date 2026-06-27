# http-gateway

A small, high-performance HTTP gateway foundation written in Go. The first
phase keeps the request path simple and uses mainstream dependencies behind
small internal abstractions:

- reverse proxy based on Go `net/http` and `httputil.ReverseProxy`
- YAML config with route, upstream, and rate-limit settings
- host + path-prefix routing
- round-robin upstream selection
- local token-bucket rate limiting
- structured JSON access logs through a global internal `log` package backed by Zap
- hot config reload through the admin API
- Prometheus-compatible `/metrics` through a global internal `metric` package
- pprof endpoints on the admin server

## Run

Start a test upstream:

```sh
go run ./bench/simple-upstream.go
```

Start the gateway:

```sh
go run ./cmd/gateway -config configs/example.yaml
```

Send traffic:

```sh
curl http://127.0.0.1:8080/
```

Admin endpoints:

```sh
curl http://127.0.0.1:9090/healthz
curl http://127.0.0.1:9090/metrics
curl http://127.0.0.1:9090/admin/config
curl -X POST http://127.0.0.1:9090/admin/reload
go tool pprof http://127.0.0.1:9090/debug/pprof/profile
```

## Config

See `configs/example.yaml`.

Routes are matched by longest `path_prefix`; `host` is optional. Each route has
one or more upstreams and optional local rate limiting.

## Logging

Application code uses the internal global log package directly:

```go
log.Infof("gateway listening addr=%s", addr)
log.Errorf("proxy error: %v", err)
```

The package is backed by Zap and writes through an internal asynchronous buffer.
High-frequency access logs use `log.Access(...)`; dropped async log entries can
be inspected with `log.Dropped()`.

## Metrics

Application code records metrics through the internal global metric package:

```go
metric.BeginRequest()
metric.EndRequest(metric.RequestResult{Route: "api", Status: 200})
metric.ObserveReload(true)
```

The admin server exposes `metric.ServeHTTP` at `/metrics`, so callers do not
need to create or pass a registry.

## Test

```sh
go test ./...
```
