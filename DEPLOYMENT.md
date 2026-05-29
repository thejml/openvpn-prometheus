# OpenVPN Exporter - Kubernetes Deployment Guide

This guide covers deploying the OpenVPN Prometheus Exporter to Kubernetes.

## Prerequisites

- Kubernetes cluster (1.24+)
- Docker buildx for multi-architecture builds
- kubectl configured for your cluster
- OpenVPN running on your node(s) with a status file at `/var/log/openvpn/openvpn-status.log`

## Building the Docker Image

The GitHub Actions workflow automatically builds and pushes images for both `amd64` and `arm64` architectures to GitHub Container Registry.

### Manual Build

```bash
# Build for current architecture
docker build -t openvpn-exporter:latest .

# Build for specific architecture
docker buildx build --platform linux/arm64 -t openvpn-exporter:arm64 .
docker buildx build --platform linux/amd64 -t openvpn-exporter:amd64 .

# Build and push multi-arch image
docker buildx build --platform linux/amd64,linux/arm64 \
  -t ghcr.io/yourusername/openvpn-exporter:latest \
  --push .
```

## Pre-Deployment Configuration

### Configure PersistentVolume

Before deploying, update `kubernetes/persistentVolume.yaml` with your NFS server details:

```yaml
nfs:
  path: /var/log/openvpn           # Path on NFS server where OpenVPN status file is located
  server: your-nfs-server.local    # NFS server hostname or IP address
```

Example for common NFS setups:
```yaml
nfs:
  path: /mnt/storage/openvpn
  server: 192.168.1.100
```

Or for a different storage backend, modify the volume section in `persistentVolume.yaml` to use:

**Local Volume:**
```yaml
local:
  path: /var/lib/openvpn-data
---
nodeAffinity:
  required:
    nodeSelectorTerms:
    - matchExpressions:
      - key: kubernetes.io/hostname
        operator: In
        values:
        - node-name
```

**Cloud Storage (AWS EBS):**
```yaml
awsElasticBlockStore:
  volumeID: vol-xxxxxx
  fsType: ext4
```

**Azure Disk:**
```yaml
azureDisk:
  diskName: openvpn-disk
  diskURI: /subscriptions/.../resourceGroups/.../providers/.../disks/openvpn-disk
```

## Deployment Options

### Option 1: Using Kustomize

```bash
# Deploy with kustomize
kubectl apply -k kubernetes/

# Check deployment status
kubectl get deployment openvpn-exporter
kubectl logs -f deployment/openvpn-exporter
```

### Option 2: Using kubectl directly

```bash
# Apply all manifests in order
kubectl apply -f kubernetes/persistentVolume.yaml
kubectl apply -f kubernetes/serviceaccount.yaml
kubectl apply -f kubernetes/service.yaml
kubectl apply -f kubernetes/deployment.yaml

# Or apply all at once
kubectl apply -f kubernetes/
```

### Option 3: Using Helm (future enhancement)

Coming soon...

## Configuration

The exporter is configured via environment variables in the Deployment manifest:

- `OPENVPN_STATUS_FILE`: Path to OpenVPN status file (default: `/etc/openvpn/openvpn-status.log`)
- `PORT`: HTTP server port (default: `8080`)
- `LISTEN_ADDR`: Listen address (default: `0.0.0.0`)

### Important Notes

1. **NFS PersistentVolume**: The deployment uses an NFS-based PersistentVolume to access the OpenVPN status file. This requires:
   - NFS server configured with the OpenVPN status file exported
   - Update `persistentVolume.yaml` with your NFS server details:
     ```yaml
     nfs:
       path: /var/log/openvpn              # NFS export path
       server: kanzaki.thejml.info         # NFS server hostname/IP
       readOnly: false
     ```

2. **Alternative Storage**: You can modify the `persistentVolume.yaml` to use:
   - Local persistent volumes
   - Cloud storage (EBS, AzureDisk, GCE persistent disks)
   - CSI drivers

3. **Node Affinity**: If you have multiple nodes, you can add node selectors to ensure the pod runs on the correct node:

```yaml
nodeSelector:
  node-role.kubernetes.io/openvpn: "true"
```

4. **Prometheus Integration**: The deployment includes annotations for automatic Prometheus scraping:
   - `prometheus.io/scrape: "true"`
   - `prometheus.io/port: "8080"`
   - `prometheus.io/path: "/metrics"`

## Verification

Check if the exporter is running correctly:

```bash
# Port forward to access metrics
kubectl port-forward service/openvpn-exporter 8080:8080

# In another terminal, access the metrics
curl http://localhost:8080/metrics

# Check health endpoint
curl http://localhost:8080/health
```

## Monitoring Metrics

Once deployed, Prometheus metrics are available at:

```
http://openvpn-exporter:8080/metrics
```

Key metrics exposed:

- `openvpn_up` - OpenVPN status file reachability
- `openvpn_connected_clients` - Number of connected clients
- `openvpn_client_bytes_received{common_name="..."}` - Bytes received per client
- `openvpn_client_bytes_sent{common_name="..."}` - Bytes sent per client
- `openvpn_client_connected_since_timestamp{common_name="..."}` - Client connection time
- `openvpn_status_file_age_seconds` - Age of status file
- `openvpn_exporter_duration_seconds` - Metric collection duration

## Troubleshooting

### Pod won't start

```bash
# Check pod status
kubectl describe pod -l app=openvpn-exporter

# Check logs
kubectl logs deployment/openvpn-exporter
```

### PersistentVolumeClaim pending

```bash
# Check PVC status
kubectl get pvc openvpn-status-pvc

# Get detailed PVC info
kubectl describe pvc openvpn-status-pvc

# Check PersistentVolume
kubectl get pv openvpn-status-pv
kubectl describe pv openvpn-status-pv
```

**Common causes:**
- NFS server unreachable: Verify NFS server IP/hostname and network connectivity
- NFS export path doesn't exist: Verify the export path exists on the NFS server
- NFS permissions: Ensure the NFS export is world-accessible or check export permissions

**Test NFS connectivity:**
```bash
# From any pod with nfs-utils installed
showmount -e <nfs-server>
```

### Metrics not appearing

1. Check PVC is mounted correctly:
   ```bash
   kubectl exec -it deployment/openvpn-exporter -- ls -la /etc/openvpn/
   ```

2. Verify OpenVPN status file exists:
   ```bash
   kubectl exec -it deployment/openvpn-exporter -- cat /etc/openvpn/openvpn-status.log
   ```

3. Check deployment environment variables:
   ```bash
   kubectl set env deployment/openvpn-exporter --list
   ```

4. Port-forward and test the endpoint directly:
   ```bash
   kubectl port-forward pod/<pod-name> 8080:8080
   curl http://localhost:8080/metrics
   ```

5. Check pod logs for errors:
   ```bash
   kubectl logs deployment/openvpn-exporter --tail=50
   ```

## Cleanup

```bash
# Remove all resources
kubectl delete -f kubernetes/

# Or with kustomize
kubectl delete -k kubernetes/
```

## GitHub Actions Workflow

The `.github/workflows/build-multiarch.yaml` workflow:

- Triggers on push to main/master branches and version tags
- Builds images for both `amd64` and `arm64` architectures
- Pushes to GitHub Container Registry (GHCR)
- Uses cache for faster builds

### First-time setup

1. Ensure your GitHub repository has Actions enabled
2. The workflow uses `GITHUB_TOKEN` for authentication (automatically available)
3. Images are pushed to `ghcr.io/<username>/<repo>`

### Updating the image in the Deployment

After a new image is built and pushed, update the deployment:

```bash
# Using kubectl
kubectl set image deployment/openvpn-exporter \
  openvpn-exporter=ghcr.io/yourusername/openvpn-exporter:latest

# Or edit the kustomization.yaml and reapply
kubectl apply -k kubernetes/
```

## Security Considerations

- Pod runs as non-root user (UID 1000)
- Read-only root filesystem
- No special capabilities granted
- Pod anti-affinity to spread replicas across nodes
- Security context enforces constraints

## Next Steps

1. Update the image registry URL in `kubernetes/kustomization.yaml` to your registry
2. Customize node selectors if running on specific nodes
3. Integrate with your Prometheus configuration for scraping
4. Set up Grafana dashboards for visualization
