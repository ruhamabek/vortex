# High-Performance gRPC Streaming

This module covers the binary Protocol Buffers interface, client-streaming uploads, and how gRPC enables high-throughput ingestion from mobile and backend clients.

---

## Protobuf Specification (proto/video/v1/video.proto)

```protobuf
syntax = "proto3";

package video.v1;

option go_package = "github.com/ruhamabek/vortex/proto/gen/video/v1;videov1";

enum VideoStatus {
  VIDEO_STATUS_UNSPECIFIED = 0;
  VIDEO_STATUS_PENDING = 1;
  VIDEO_STATUS_UPLOADED = 2;
  VIDEO_STATUS_TRANSCODING = 3;
  VIDEO_STATUS_COMPLETED = 4;
  VIDEO_STATUS_FAILED = 5;
}

message UploadMetadata {
  string title = 1;
  string original_file_name = 2;
  string user_id = 3;
  string content_type = 4;
  int64 file_size = 5;
}

message UploadVideoRequest {
  oneof payload {
    UploadMetadata metadata = 1;
    bytes chunk = 2;
  }
}

message UploadVideoResponse {
  Video video = 1;
}

service VideoService {
  rpc UploadVideo(stream UploadVideoRequest) returns (UploadVideoResponse);
  rpc GetVideo(GetVideoRequest) returns (GetVideoResponse);
  rpc GetPlaybackURL(GetPlaybackURLRequest) returns (GetPlaybackURLResponse);
}
```

---

## Client Streaming Mechanism

In standard HTTP multipart uploads, network drops corrupt the payload and require full re-uploads. With gRPC client streaming, binary data is transferred in discrete frames over an HTTP/2 multiplexed stream.

```
Client                                                  vortex-grpc Server
  │                                                             │
  ├── Frame 1: UploadMetadata (title, user_id, size) ──────────►│ Validate metadata
  │                                                             │
  ├── Frame 2: bytes chunk (64 KB) ────────────────────────────►│ Stream chunk to S3
  ├── Frame 3: bytes chunk (64 KB) ────────────────────────────►│ Stream chunk to S3
  ├── Frame ...: bytes chunk (64 KB) ──────────────────────────►│ Stream chunk to S3
  ├── Frame N: EOF (Stream closed by client) ──────────────────►│ Finalize S3 upload
  │                                                             │ Write DB record
  │                                                             │ Publish NATS event
  │◄── UploadVideoResponse (Video entity JSON/Proto) ───────────┤
```

### The streamReader Adapter
Because MinIO’s S3 SDK expects an io.Reader, we developed a custom adapter (streamReader) that translates incoming gRPC stream frames into a standard byte reader:

```go
type streamReader struct {
    stream videov1.VideoService_UploadVideoServer
    buf    []byte
}

func (r *streamReader) Read(p []byte) (int, error) {
    if len(r.buf) == 0 {
        req, err := r.stream.Recv()
        if err != nil {
            return 0, err // Returns io.EOF when client finishes
        }
        r.buf = req.GetChunk()
    }
    n := copy(p, r.buf)
    r.buf = r.buf[n:]
    return n, nil
}
```

---

## Verification via grpcurl

The gRPC server enables Server Reflection, allowing dynamic inspection and testing without needing client stubs:

```bash
# List available services
grpcurl -plaintext localhost:50055 list

# Inspect VideoService methods
grpcurl -plaintext localhost:50055 describe video.v1.VideoService
```
