# Architecture and Domain Model

This document outlines the software engineering principles, structural boundaries, and business rules governing the Vortex platform.

---

## Clean Architecture Principles

Vortex adheres strictly to Clean Architecture (Hexagonal / Ports and Adapters). The software is organized in concentric layers with a strict dependency rule: inner layers have no knowledge of outer layers.

```
    ┌────────────────────────────────────────────────────────┐
    │  Frameworks & Drivers (Postgres, MinIO, NATS, Caddy)   │
    │  ┌──────────────────────────────────────────────────┐  │
    │  │  Interface Adapters (HTTP, gRPC, Storage Repos)  │  │
    │  │  ┌────────────────────────────────────────────┐  │  │
    │  │  │  Application Business Rules (Services)     │  │  │
    │  │  │  ┌──────────────────────────────────────┐  │  │  │
    │  │  │  │  Enterprise Domain Rules (Entities)  │  │  │  │
    │  │  │  └──────────────────────────────────────┘  │  │  │
    │  │  └────────────────────────────────────────────┘  │  │
    │  └──────────────────────────────────────────────────┘  │
    └────────────────────────────────────────────────────────┘
```

### 1. Domain Layer (internal/domain/)
- Contains pure business entities and invariant validations.
- Contains zero external dependencies. It does not import database drivers, JSON serialization tags, or third-party frameworks.
- Defines storage interfaces ("Ports") that outer layers must implement.

### 2. Service Layer (internal/service/)
- Coordinates business workflows and orchestrates data flow between repositories, object storage, and event publishers.
- Agnostic to transport protocols (does not know whether a request came via HTTP, gRPC, or CLI).

### 3. Adapters Layer (internal/storage/, internal/api/, internal/grpc/, internal/event/)
- Implements the interfaces defined by the domain layer.
- Translates database rows into domain entities and HTTP/gRPC requests into domain commands.

---

## Video Entity and Invariants

The central entity is domain.Video:

```go
type Video struct {
    ID                string
    Title             string
    OriginalFileName  string
    OriginalSize      int64
    UserID            string
    Status            VideoStatus
    SourceURL         string
    MasterPlaylistURL string
    ErrorMessage      string
    CreatedAt         time.Time
    UpdatedAt         time.Time
}
```

### Invariants Enforced at Construction:
- Title Validation: Title cannot be empty and cannot exceed 255 characters.
- File Size Validation: Size must be greater than zero and must not exceed MaxVideoSizeBytes (500MB).
- User Ownership: Every video must be associated with a valid UserID.
- Initial State: Newly created videos must start in the PENDING state.

---

## Finite State Machine (FSM)

A video progresses through a deterministic lifecycle. Invalid state transitions return sentinel errors and are rejected by the system.

```
       [ Upload Initiated ]
                 │
                 ▼
          ┌─────────────┐
          │   PENDING   │
          └──────┬──────┘
                 │ MarkUploaded()
                 ▼
          ┌─────────────┐
          │  UPLOADED   │
          └──────┬──────┘
                 │ MarkTranscoding()
                 ▼
          ┌─────────────┐
          │ TRANSCODING │
          └──────┬──────┘
                 │
         ┌───────┴───────┐
         │               │
         ▼ MarkCompleted()▼ MarkFailed()
  ┌─────────────┐ ┌─────────────┐
  │  COMPLETED  │ │   FAILED    │
  └─────────────┘ └─────────────┘
```

### Permitted Transitions:
1. PENDING -> UPLOADED: When raw binary upload to MinIO S3 succeeds.
2. UPLOADED -> TRANSCODING: When a background worker claims the transcoding task.
3. TRANSCODING -> COMPLETED: When multi-bitrate HLS chunking and master playlist generation succeed.
4. TRANSCODING -> FAILED: If FFmpeg encounters unrecoverable decode/encode errors.

Any attempt to transition from COMPLETED to TRANSCODING or from FAILED to COMPLETED returns domain.ErrInvalidStateTransition.
