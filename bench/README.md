# Benchmarks

Start a simple upstream:

```sh
go run ./cmd/gateway -config configs/example.yaml
```

In another shell, run an upstream service on `:8081`, then use any common HTTP load generator:

```sh
hey -z 30s -c 100 http://127.0.0.1:8080/
vegeta attack -duration=30s -rate=1000 http://127.0.0.1:8080/ | vegeta report
wrk -t4 -c100 -d30s http://127.0.0.1:8080/
```
