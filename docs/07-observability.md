# Observability Suite

Vortex includes an enterprise-grade observability architecture unifying Prometheus Metrics, Loki Logs, and Tempo Distributed Traces into a Grafana dashboard.

---

## Distributed Tracing with W3C Context Propagation

Because video processing spans multiple independent microservices over an asynchronous message broker, standard request logging is insufficient. Vortex implements OpenTelemetry distributed tracing:

```
[ vortex-api ]
  │ Span: POST /api/v1/videos/upload
  │ Duration: 58.4ms
  │ Injects Traceparent into NATS Header
  │
  ▼
[ NATS JetStream ] (videos.uploaded)
  │
  ▼
[ vortex-worker ]
  │ Extracts Traceparent from NATS Header
  │ Span: transcoder_worker.process_video
  │ Duration: 5.8s
  │
  ▼
[ OpenTelemetry Collector / Tempo ] (Port 4317 gRPC)
```

### Trace Context across NATS
When publishing an event, the API serializes the active OpenTelemetry context into NATS message headers:
```go
carrier := propagation.HeaderCarrier(natsMsg.Header)
otel.GetTextMapPropagator().Inject(ctx, carrier)
```
When receiving the job, the worker extracts the context before creating child spans:
```go
parentCtx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.HeaderCarrier(msg.Headers()))
tracer := telemetry.Tracer("vortex-worker")
ctx, span := tracer.Start(parentCtx, "transcoder_worker.process_video")
```

This connects the synchronous API upload span with the asynchronous background transcoding span into a single waterfall visualization in Grafana Tempo.

---

## Prometheus Metrics

The API and Workers export Prometheus metrics on GET /metrics:

- vortex_http_requests_total: Counter tracking requests segmented by method, path, and status.
- vortex_http_request_duration_seconds: Histogram tracking request latency distribution buckets.
- Go Runtime Metrics: go_goroutines, go_memstats_alloc_bytes, process_cpu_seconds_total.

---

## Pre-Provisioned Grafana Dashboard

Grafana is provisioned as infrastructure-as-code:
- Datasources: deploy/grafana/provisioning/datasources/datasources.yaml pre-connects Prometheus, Loki, and Tempo.
- Dashboards: deploy/grafana/provisioning/dashboards/dashboards.yaml auto-loads vortex_overview.json on boot.

Access the dashboard at http://localhost:3000/d/vortex-overview/vortex-engine-overview.
