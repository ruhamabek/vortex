# Vortex: Cloud-Native Distributed Video Infrastructure Engine

Vortex is a distributed video processing, transcoding, and delivery platform written in pure Go. It ingests raw video streams, stores them in object storage, coordinates distributed workers using asynchronous message brokers, segments video into multi-bitrate HTTP Live Streaming (HLS) playlists using FFmpeg, tracks real-time progress over WebSockets, executes durable post-processing workflows, and exports distributed telemetry across all services.

The system is deployed on Kubernetes using custom Helm charts, fronted by a reverse-proxy API gateway, and auto-scaled using Horizontal Pod Autoscalers (HPA).

---

## High-Level System Architecture

```
                                [ External Clients ]
                       (Web Browsers, Mobile Apps, Third Parties)
                                         │
                                         ▼ :30080
                     ┌───────────────────────────────────────┐
                     │          Caddy Edge Gateway           │
                     │  - Reverse Proxy & L7 Load Balancer   │
                     │  - HTTP/2 Cleartext (h2c) for gRPC    │
                     │  - 500MB Unbuffered Video Streaming   │
                     └───────────────────┬───────────────────┘
                                         │
                 ┌───────────────────────┴───────────────────────┐
                 │                                               │
                 ▼ :8080                                         ▼ :50055
  ┌─────────────────────────────┐                 ┌─────────────────────────────┐
  │         vortex-api          │                 │         vortex-grpc         │
  │  - Multipart Upload Stream  │                 │  - Protobuf Client Stream   │
  │  - WebSockets (Gorilla)     │                 │  - High Throughput Chunks   │
  │  - Redis Sliding Window RL  │                 │  - Reflection Enabled       │
  └──────────────┬──────────────┘                 └──────────────┬──────────────┘
                 │                                               │
                 └───────────────────────┬───────────────────────┘
                                         │
          ┌──────────────────────────────┼──────────────────────────────┐
          ▼                              ▼                              ▼
  ┌───────────────┐              ┌───────────────┐              ┌───────────────┐
  │  PostgreSQL   │              │   MinIO S3    │              │ NATS Broker   │
  │  (pgx/v5)     │              │ (Object Store)│              │ (JetStream)   │
  │ Video State   │              │ Raw Videos    │              │ Subject:      │
  │ Metadata      │              │ HLS Playlists │              │ videos.upload │
  └───────────────┘              └───────┬───────┘              └───────┬───────┘
                                         │                              │
                                         │      Job Dispatched          │
                                         │ ◄────────────────────────────┘
                                         ▼
                         ┌───────────────────────────────┐
                         │         vortex-worker         │
                         │  - NATS JetStream Consumer    │
                         │  - FFmpeg Multi-Bitrate HLS   │
                         │  - Progress Tracker (Redis)   │
                         │  - Auto-Scaled by K8s HPA     │
                         └───────────────┬───────────────┘
                                         │
                                         ├──────────────────────────────┐
                                         ▼                              ▼
                         ┌───────────────────────────────┐      ┌───────────────┐
                         │   Inngest Durable Workflow    │      │  Redis Cache  │
                         │  - Generate Poster Thumbnail  │      │ Progress Hash │
                         │  - Generate Animated GIF      │      │ Pub/Sub Event │
                         │  - Dispatch Customer Webhook  │      └───────────────┘
                         └───────────────────────────────┘
```

---

## Core Technologies and Rationale

| Component | Technology | Rationale and Tradeoffs |
| :--- | :--- | :--- |
| Language | Go 1.22+ / 1.26 | High concurrency via lightweight goroutines, static binary compilation, minimal memory overhead under heavy I/O workloads. |
| Architecture | Clean Architecture / Hexagonal | Decouples business rules from database adapters, file systems, and transport protocols. Simplifies test-driven development. |
| Database | PostgreSQL 16 (pgx/v5) | ACID compliance for video state transitions. High-performance connection pooling via pgxpool. |
| Object Storage | MinIO S3 SDK | S3-compliant object store. Allows local development while maintaining seamless compatibility with AWS S3, Google Cloud Storage, or Cloudflare R2. |
| Message Broker | NATS JetStream 2.10 | High-throughput, low-latency streaming platform. Guarantees at-least-once delivery with persistent consumer groups. |
| Cache & Pub/Sub | Redis 7 | In-memory key-value store for atomic sliding-window rate limiting, progress state caching, and Pub/Sub WebSocket fanout. |
| Transcoding | FFmpeg 6+ / HLS | Industry-standard media processing. Segments source MP4 files into HLS master playlists and chunked transport streams (.ts). |
| Protocols | REST, WebSockets, gRPC | Multipart REST for web clients, WebSockets for live progress tracking, and gRPC with client streaming for high-throughput mobile ingestion. |
| Workflows | Inngest SDK | Durable execution engine. Handles asynchronous, multi-step post-processing with automatic retries and checkpointing. |
| Observability | OpenTelemetry, Prometheus, Tempo, Loki, Grafana | Unified telemetry. Distributed W3C trace context propagated across HTTP, NATS, and FFmpeg workers into a Grafana cockpit. |
| Orchestration | Kubernetes, Minikube, Helm 3 | Declarative cloud deployments, automated rollouts, secret isolation, and CPU-driven Horizontal Pod Autoscaling. |
| Gateway | Caddy 2 | Reverse proxy with zero-build declarative ConfigMap mounts, unbuffered 500MB payload streaming, and HTTP/2 cleartext (h2c) proxying. |

---

## Directory Structure

```
vortex/
├── api/                     # API definitions (OpenAPI 3.0 specs)
├── build/                   # Packaging and Dockerfiles
│   └── package/
│       ├── Dockerfile.api
│       ├── Dockerfile.grpc
│       └── Dockerfile.worker
├── cmd/                     # Application composition roots
│   ├── api/                 # REST & WebSocket HTTP daemon
│   ├── grpc/                # Protobuf gRPC daemon
│   └── worker/              # Transcoding worker daemon
├── deploy/                  # Infrastructure configurations
│   ├── grafana/             # Grafana datasources and pre-provisioned dashboards
│   ├── helm/                # Parameterized Helm 3 chart for Kubernetes
│   ├── k8s/                 # Raw declarative Kubernetes manifests
│   ├── loki/                # Loki log aggregation config
│   ├── prometheus/          # Prometheus scraping rules
│   └── tempo/               # Tempo distributed tracing config
├── docs/                    # Deep-dive architectural documentation
├── internal/                # Private application packages
│   ├── api/v1/              # HTTP and WebSocket route handlers
│   ├── domain/              # Entities, state machines, and repository interfaces
│   ├── event/nats/          # NATS JetStream event publisher
│   ├── grpc/v1/             # gRPC server implementation
│   ├── service/             # Application use cases and business logic
│   ├── storage/             # Database and storage adapters (Postgres, MinIO, Redis)
│   ├── transcoder/          # FFmpeg execution and stdout progress parser
│   ├── worker/              # Transcoding worker orchestration
│   └── workflow/            # Inngest durable post-processing functions
├── pkg/                     # Reusable shared packages
│   ├── config/              # 12-Factor App unified environment loader
│   ├── logger/              # Structured Uber Zap logger
│   ├── middleware/          # Rate limiting, logging, metrics, CORS, tracing
│   └── telemetry/           # OpenTelemetry tracer provider and propagators
├── proto/                   # Protobuf definitions and generated Go code
├── docker-compose.yaml      # Local infrastructure composition (8 services)
└── go.mod                   # Dependency definitions
```

---

## Getting Started

### Prerequisites
- Go 1.22+
- Docker & Docker Compose
- Minikube & Helm 3 (for Kubernetes deployments)
- FFmpeg (for local non-container testing)
- grpcurl (optional, for gRPC manual testing)

### 1. Launch Local Infrastructure
Start PostgreSQL, Redis, NATS, MinIO, Prometheus, Loki, Tempo, and Grafana:
```bash
docker compose up -d
```

Verify that all containers are healthy:
```bash
docker compose ps
```

### 2. Run Test Suite
The codebase follows strict Test-Driven Development (TDD). Run all unit and integration tests:
```bash
go test -v ./...
```

### 3. Run Locally (Development Mode)
Start the API server:
```bash
go run cmd/api/main.go
```

Start the Transcoding Worker in a second terminal:
```bash
go run cmd/worker/main.go
```

Start the gRPC service in a third terminal:
```bash
go run cmd/grpc/main.go
```

### 4. Deploy to Kubernetes (Production Mode via Helm)
Build the container images:
```bash
docker build -f Dockerfile.api -t vortex-api:latest .
docker build -f Dockerfile.worker -t vortex-worker:latest .
docker build -f Dockerfile.grpc -t vortex-grpc:latest .
```

Load the images into your Minikube cluster:
```bash
minikube image load vortex-api:latest vortex-worker:latest vortex-grpc:latest
```

Deploy the entire system using Helm:
```bash
helm install vortex deploy/helm/vortex -n vortex --create-namespace
```

Verify deployment status:
```bash
helm list -n vortex
kubectl get pods -n vortex
```

---

## Detailed Documentation Modules

For in-depth explanations of individual subsystems, refer to the guides in the `docs/` directory:

1. [Architecture & Domain Model](docs/01-architecture-and-domain.md)
2. [REST API & Real-Time WebSockets](docs/02-rest-and-websockets.md)
3. [High-Performance gRPC Streaming](docs/03-grpc-streaming.md)
4. [Transcoder Engine & Worker Daemon](docs/04-transcoder-worker.md)
5. [Inngest Durable Workflows](docs/05-inngest-workflows.md)
6. [Rate Limiting & Edge Gateway](docs/06-rate-limiting-and-gateway.md)
7. [Observability Suite (Metrics, Logs, Traces)](docs/07-observability.md)
8. [Kubernetes & Helm Deployment](docs/08-kubernetes-and-helm.md)
