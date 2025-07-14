# Deployment Guide

This guide covers deploying tsdnsreflector in various environments.

## Quick Start

### Docker (Recommended)
```bash
docker run -d --name tsdnsreflector \
  -p 53:53/udp \
  -e TS_AUTHKEY=tskey-auth-your-key \
  ghcr.io/rajsinghtech/tsdnsreflector:latest
```

### Pre-built Binaries
```bash
# Download latest release
wget https://github.com/rajsinghtech/tsdnsreflector/releases/latest/download/tsdnsreflector-linux-amd64
chmod +x tsdnsreflector-linux-amd64
./tsdnsreflector-linux-amd64 -config config.hujson
```

## Kubernetes Deployment

### Using Kustomize (Recommended)
```bash
# Apply all manifests
kubectl apply -k deploy/k8s/base/

# Create auth secret
kubectl create secret generic tailscale-auth \
  --from-literal=authkey=tskey-auth-your-key \
  -n tsdnsreflector
```

### OAuth Authentication (Production)
```bash
# Create OAuth secret
kubectl create secret generic tailscale-oauth \
  --from-literal=client_id=tskey-client-your-id \
  --from-literal=client_secret=your-secret \
  -n tsdnsreflector

# Deploy without auth key
kubectl apply -k deploy/k8s/base/
```

### Manual Deployment
```bash
# Create namespace
kubectl create namespace tsdnsreflector

# Apply RBAC
kubectl apply -f deploy/k8s/base/rbac.yaml

# Create config
kubectl apply -f deploy/k8s/base/configmap.yaml

# Deploy StatefulSet
kubectl apply -f deploy/k8s/base/statefulset.yaml
```

## Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'
services:
  tsdnsreflector:
    image: ghcr.io/rajsinghtech/tsdnsreflector:latest
    container_name: tsdnsreflector
    ports:
      - "53:53/udp"
      - "8080:8080"
      - "9090:9090"
    environment:
      - TS_AUTHKEY=tskey-auth-your-key
    volumes:
      - ./config.hujson:/config.hujson
      - tsdns-state:/var/lib/tailscale
    restart: unless-stopped

volumes:
  tsdns-state:
```

## Systemd Service

Create `/etc/systemd/system/tsdnsreflector.service`:

```ini
[Unit]
Description=tsdnsreflector DNS proxy
After=network.target

[Service]
Type=simple
User=tsdnsreflector
Group=tsdnsreflector
ExecStart=/usr/local/bin/tsdnsreflector -config /etc/tsdnsreflector/config.hujson
Environment=TS_AUTHKEY=tskey-auth-your-key
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl enable tsdnsreflector
sudo systemctl start tsdnsreflector
```

## Building from Source

### Development Setup
```bash
git clone https://github.com/rajsinghtech/tsdnsreflector.git
cd tsdnsreflector

# Install dependencies
go mod download

# Run tests
make test

# Build binary
make build
```

### Development Workflow
```bash
make dev    # clean, lint, test, build
make docker # build Docker image
```

### Multi-architecture Build
```bash
make docker-multiarch TAG=v1.1.0
```

## Security Configuration

### Container Security
The included manifests use security best practices:
- Non-root user (UID 65534)
- Read-only filesystem
- Dropped capabilities
- Security context constraints

### RBAC Permissions
Minimal Kubernetes permissions:
```yaml
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "update", "patch", "create"]
```

### Network Security
- Bind to specific interfaces when possible
- Use firewall rules to restrict access
- Monitor external client metrics
- Implement rate limiting at network level

## Monitoring and Health Checks

### Health Endpoint
```bash
curl http://tsdnsreflector:8080/health
```

### Prometheus Metrics
```bash
curl http://tsdnsreflector:9090/metrics
```

### Kubernetes Probes
Health checks are automatically configured in the StatefulSet:
- Liveness probe: `/health`
- Readiness probe: `/health`

## Troubleshooting

### Common Issues

1. **DNS queries timing out**
   - Check Tailscale connectivity
   - Verify backend DNS servers are reachable
   - Check firewall rules

2. **OAuth authentication failing**
   - Verify client credentials are valid
   - Check secret is properly mounted
   - Review OAuth configuration

3. **4via6 translation not working**
   - Confirm subnet router advertises 4via6 prefix
   - Verify site ID matches configuration
   - Check split-DNS routing

### Debug Commands
```bash
# Check Tailscale status
tailscale status

# Test DNS resolution
nslookup -type=AAAA test.domain.local <tsdnsreflector-ip>

# View detailed logs
kubectl logs -f tsdnsreflector-0 -n tsdnsreflector
```