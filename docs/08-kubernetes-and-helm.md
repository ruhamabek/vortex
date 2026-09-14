# Kubernetes and Helm Deployment

This document covers multi-stage container optimization, declarative Kubernetes manifests, and Helm 3 chart packaging.

---

## Multi-Stage Container Packaging

Each microservice utilizes a two-stage build process to ensure minimal attack surfaces and tiny runtime image sizes.

### API Container (Dockerfile.api):
- Stage 1 (Builder): Uses golang:alpine. Downloads modules, compiles statically with CGO disabled (CGO_ENABLED=0), and strips symbol tables and debug information (-ldflags="-s -w").
- Stage 2 (Runtime): Uses minimal alpine:3.20. Adds CA certificates, creates an unprivileged non-root user (appuser:10001), and copies only the static binary.
- Result: Image size is reduced to ~15MB compressed (down from >800MB in standard Go images).

### Worker Container (Dockerfile.worker):
- Packages the static worker binary alongside the native ffmpeg package inside a secure, non-root Alpine runtime.
- Resulting image size is ~62MB compressed, including all encoding libraries.

---

## Horizontal Pod Autoscaler (HPA)

Because video transcoding is CPU-intensive, workers must scale dynamically based on cluster workload:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: vortex-worker-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: vortex-worker
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

When transcoding jobs flood the NATS queue, worker CPU spikes. When average CPU exceeds 70%, the Kubernetes metrics-server triggers the HPA to spawn additional worker pods (up to a ceiling of 10 replicas). When the queue clears, the HPA scales the pool back down to 2 pods.

---

## Helm Chart Parameters (deploy/helm/vortex/values.yaml)

The Helm chart parameterizes all deployment variables into a single configuration file:

```yaml
config:
  environment: "production"
  minioEndpoint: "host.minikube.internal:9000"
  natsUrl: "nats://host.minikube.internal:4222"
  redisAddr: "host.minikube.internal:6379"

api:
  replicaCount: 2
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi

worker:
  replicaCount: 2
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70

caddy:
  service:
    type: NodePort
    port: 80
    nodePort: 30080
  maxUploadSize: 500MB
```

### Essential Helm Commands:

```bash
# Lint the chart for syntax errors
helm lint deploy/helm/vortex

# Preview the rendered YAML templates
helm template vortex deploy/helm/vortex

# Install the release
helm install vortex deploy/helm/vortex -n vortex --create-namespace

# Perform a zero-downtime rolling upgrade
helm upgrade vortex deploy/helm/vortex -n vortex

# Inspect revision history
helm history vortex -n vortex

# Roll back to a previous revision
helm rollback vortex 1 -n vortex
```
