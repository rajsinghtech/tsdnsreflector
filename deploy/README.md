# Kubernetes Deployment

Quick reference for deploying tsdnsreflector to Kubernetes. For detailed deployment instructions, see the [Deployment Guide](../docs/DEPLOYMENT.md).

## Quick Deploy

### Standard Deployment
```bash
# Apply all manifests
kubectl apply -k deploy/k8s/base/

# Create auth secret
kubectl create secret generic tailscale-auth \
  --from-literal=authkey=tskey-auth-your-key \
  -n tsdnsreflector
```

### OAuth Deployment (Production)
```bash
# Create OAuth secret
kubectl create secret generic tailscale-oauth \
  --from-literal=client_id=tskey-client-your-id \
  --from-literal=client_secret=your-secret \
  -n tsdnsreflector

# Deploy
kubectl apply -k deploy/k8s/base/
```

## Makefile Commands

```bash
make k8s-deploy AUTHKEY=tskey-auth-xxxxx  # Deploy with auth key
make k8s-status                           # Check deployment status  
make k8s-logs                             # View logs
make k8s-undeploy                         # Clean up
```

## Configuration

Edit `configmap.yaml` to customize DNS zones and backend servers.

## Security Features

- Non-root user (UID 65534)
- Read-only filesystem 
- Minimal RBAC permissions
- Security context constraints

For complete deployment instructions, troubleshooting, and other deployment methods, see the [Deployment Guide](../docs/DEPLOYMENT.md).