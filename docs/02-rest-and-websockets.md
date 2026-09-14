# REST API and Real-Time WebSockets

This module documents the HTTP transport layer, multipart streaming mechanisms, and real-time progress broadcast via WebSockets.

---

## REST Endpoints

The HTTP API is implemented in internal/api/v1/video_handler.go using standard Go 1.22+ routing patterns.

### 1. Upload Video
- Route: POST /api/v1/videos/upload
- Content-Type: multipart/form-data
- Headers:
  - X-User-ID: (Optional) User identifier. Defaults to anonymous.
  - X-Correlation-ID: (Optional) Distributed tracing correlation ID.
- Form Fields:
  - title: String (Required). Title of the video.
  - file: Binary file stream (Required). Supported formats: MP4, MOV, MKV.
- Response: HTTP 201 Created
```json
{
  "id": "3f82d9a3-f289-4f43-8483-56078f25a8ce",
  "title": "Sample Video",
  "original_file_name": "sample.mp4",
  "original_size": 922536,
  "user_id": "user-42",
  "status": "UPLOADED",
  "source_url": "raw/user-42/3f82d9a3-f289-4f43-8483-56078f25a8ce_sample.mp4",
  "created_at": "2026-09-14T13:05:59.062Z",
  "updated_at": "2026-09-14T13:05:59.095Z"
}
```

### 2. Get Video Details
- Route: GET /api/v1/videos/{id}
- Response: HTTP 200 OK
- Returns metadata, current processing status, and error details (if failed).

### 3. Get Playback URL
- Route: GET /api/v1/videos/{id}/playback
- Response: HTTP 200 OK
```json
{
  "video_id": "3f82d9a3-f289-4f43-8483-56078f25a8ce",
  "playback_url": "http://minio:9000/vortex-hls-videos/hls/user-42/3f82d9a3/master.m3u8",
  "status": "COMPLETED"
}
```

---

## Multipart Streaming Architecture

Naive web servers read the entire uploaded file into server RAM before saving it to disk or object storage. When 100 concurrent users upload 500MB videos, memory consumption reaches 50GB, leading to Out-Of-Memory (OOM) process crashes.

Vortex uses zero-buffer stream piping:

```
[ Client Request Stream ]
           │
           ▼
[ http.MaxBytesReader ] ◄── Caps maximum upload to 500MB
           │
           ▼
[ r.MultipartReader() ] ◄── Reads boundary headers on the fly
           │
           ▼
[ io.Reader (Part) ]
           │
           ▼
[ MinIO PutObject Streaming ] ◄── Pipes directly to S3 via HTTP chunked encoding
```

Memory usage remains constant at less than 32 Kilobytes per upload, regardless of whether the video is 10 Megabytes or 500 Megabytes.

---

## Real-Time WebSockets (/ws/videos/{id}/progress)

Clients connect via WebSockets to receive millisecond-accurate encoding progress during transcoding.

### Connection Handshake
1. Client issues: GET /ws/videos/{id}/progress with standard WebSocket upgrade headers.
2. The server verifies the connection, upgrades the TCP socket using Gorilla WebSocket, and subscribes to the Redis Pub/Sub channel: vortex:channel:progress:{video_id}.

### Progress Frame Format
The worker emits JSON updates as FFmpeg chunks the video:
```json
{
  "video_id": "3f82d9a3-f289-4f43-8483-56078f25a8ce",
  "percent": 68,
  "stage": "Encoding 720p",
  "updated_at": "2026-09-14T13:06:03.120Z"
}
```

When transcoding reaches 100%, the server transmits a final completion frame and gracefully closes the WebSocket connection.
