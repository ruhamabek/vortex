# Inngest Durable Workflows

This document explains the asynchronous, durable post-processing pipeline that executes once transcoding finishes.

---

## The Need for Durable Execution

After a video finishes HLS transcoding, several secondary tasks must run:
1. Extract a high-resolution JPEG poster thumbnail.
2. Generate a 3-second animated GIF preview for hover states in web players.
3. Send a webhook to the customer's callback URL.

If all three tasks are executed in a single Go goroutine:
- What happens if the server crashes while generating the animated GIF?
- What happens if the customer's webhook endpoint returns HTTP 500?

In traditional systems, the entire process must restart, re-doing expensive video processing. With Inngest Durable Execution, each step is an independently checkpointed, idempotent unit of work.

---

## Workflow Implementation (internal/workflow/post_processing.go)

The workflow triggers on the event: video/transcoding.completed.

```go
inngestClient.CreateFunction(
    inngestgo.FunctionOpts{
        ID:   "post-process-video",
        Name: "Post-Process Video (Thumbnails & Webhooks)",
    },
    inngestgo.EventTrigger("video/transcoding.completed", nil),
    func(ctx context.Context, input inngestgo.Input) (any, error) {
        // Step 1: Generate high-resolution poster
        step1, err := inngestgo.Step(ctx, "generate-poster-thumbnail", func(ctx context.Context) (any, error) {
            return generatePoster(ctx, input.Event)
        })

        // Step 2: Generate animated GIF preview
        step2, err := inngestgo.Step(ctx, "generate-animated-preview", func(ctx context.Context) (any, error) {
            return generateAnimatedPreview(ctx, input.Event)
        })

        // Step 3: Dispatch customer webhook with exponential backoff retries
        step3, err := inngestgo.Step(ctx, "dispatch-customer-webhook", func(ctx context.Context) (any, error) {
            return dispatchWebhook(ctx, input.Event)
        })

        return map[string]string{"status": "post-processing complete"}, nil
    },
)
```

### Checkpoint and Recovery Guarantees:
- If Step 3 fails due to a network glitch, Inngest automatically retries Step 3 using exponential backoff.
- Steps 1 and 2 are NOT re-executed. Inngest remembers their previous successful outputs and skips straight to the failed step.
