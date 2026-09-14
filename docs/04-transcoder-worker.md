# Transcoder Engine and Worker Daemon

This module details the FFmpeg media processing pipeline, progress stream parsing, and the NATS JetStream worker architecture.

---

## Distributed Worker Architecture

The transcoder worker (cmd/worker/main.go) operates as a stateless daemon consuming messages from NATS JetStream.

```
                  ┌────────────────────────────────────────┐
                  │          NATS JetStream (Stream: VORTEX)│
                  │          Subject: videos.uploaded      │
                  └───────────────────┬────────────────────┘
                                      │
               ┌──────────────────────┴──────────────────────┐
               ▼ (WorkQueue Group: "transcoder-worker")      ▼
      ┌──────────────────┐                          ┌──────────────────┐
      │  worker-pod-1    │                          │  worker-pod-2    │
      │  - Claims Job    │                          │  - Claims Job    │
      │  - Download S3   │                          │  - Download S3   │
      │  - Run FFmpeg    │                          │  - Run FFmpeg    │
      │  - Upload HLS    │                          │  - Upload HLS    │
      │  - Ack Message   │                          │  - Ack Message   │
      └──────────────────┘                          └──────────────────┘
```

### NATS JetStream Guarantees:
- Consumer Type: Durable Pull Consumer (transcoder-worker).
- Acknowledgment Policy: Explicit Ack. The worker only acknowledges the message after HLS chunks are uploaded to S3 and PostgreSQL is updated to COMPLETED.
- MaxDeliver: 3. If a worker pod crashes mid-transcoding, NATS waits for AckWait (30 minutes) and automatically re-delivers the job to a surviving worker pod.

---

## FFmpeg Multi-Bitrate HLS Generation

The transcoder decomposes the input video into three adaptive bitrate streams conforming to the Apple HLS specification:

1. 360p (Low Bandwidth / Mobile): 640x360, 800 kbps video bitrate, 96 kbps audio bitrate.
2. 720p (Medium Bandwidth / HD): 1280x720, 2500 kbps video bitrate, 128 kbps audio bitrate.
3. 1080p (High Bandwidth / Full HD): 1920x1080, 5000 kbps video bitrate, 192 kbps audio bitrate.

### Generated File Bundle:
```
hls/{user_id}/{video_id}/
├── master.m3u8            # Index playlist pointing to individual stream variants
├── 360p.m3u8             # 360p playlist
├── 360p_000.ts           # 360p video segment (4-second chunk)
├── 360p_001.ts
├── 720p.m3u8             # 720p playlist
├── 720p_000.ts           # 720p video segment
├── 1080p.m3u8            # 1080p playlist
└── 1080p_000.ts          # 1080p video segment
```

---

## Stdout Progress Pipe Parsing

FFmpeg does not have an internal Go API. To extract millisecond progress without polling disk files, Vortex executes FFmpeg with progress logging directed to stdout:

```bash
ffmpeg -i input.mp4 -progress pipe:1 -nostats ...
```

The Go engine reads FFmpeg's stdout stream line by line:
```text
frame=142
fps=28.4
out_time_us=4820000
total_size=184920
progress=continue
```

When out_time_us is extracted, the engine calculates:
```
percentage = (out_time_microseconds / total_duration_microseconds) * 100
```
This percentage is immediately published to Redis Pub/Sub for WebSocket clients.
