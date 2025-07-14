# Documentation

This directory contains detailed guides for configuring and deploying tsdnsreflector.

## Guides

### 📋 [Configuration Guide](CONFIGURATION.md)
Complete reference for all configuration options, environment variables, and hot reload functionality.

### 🚀 [Deployment Guide](DEPLOYMENT.md)  
Step-by-step instructions for deploying with Docker, Kubernetes, systemd, and more.


### 🔗 [Tailscale Integration](TAILSCALE.md)
Authentication, TSNet features, MagicDNS proxy, and troubleshooting.

## Quick Links

### Common Tasks
- [Basic setup](CONFIGURATION.md#basic-configuration)
- [Kubernetes deployment](DEPLOYMENT.md#kubernetes-deployment)
- [Split-DNS configuration](TAILSCALE.md#split-dns-configuration)
- [OAuth setup](TAILSCALE.md#oauth-client-credentials-recommended)

### Troubleshooting
- [Tailscale connectivity issues](TAILSCALE.md#troubleshooting)
- [Kubernetes deployment problems](DEPLOYMENT.md#troubleshooting)
- [Configuration hot reload](CONFIGURATION.md#hot-reload)

### Advanced Topics
- [Hot reload](CONFIGURATION.md#hot-reload)
- [Security considerations](CONFIGURATION.md#security-considerations)
- [Subnet routing](TAILSCALE.md#subnet-routing)
- [Client detection](TAILSCALE.md#client-detection)

## Examples

### Minimal Config
```json
{
  "zones": {
    "cluster1": {
      "domains": ["*.cluster1.local"],
      "backend": {"dnsServers": ["10.0.0.10:53"]},
      "4via6": {"reflectedDomain": "cluster.local", "translateid": 1}
    }
  }
}
```

### Docker Command
```bash
docker run -d --name tsdnsreflector \
  -p 53:53/udp \
  -e TS_AUTHKEY=tskey-auth-your-key \
  ghcr.io/rajsinghtech/tsdnsreflector:latest
```

### Testing
```bash
# Test 4via6 translation
nslookup -type=AAAA kubernetes.default.svc.cluster1.local <tsdnsreflector-ip>

# Test MagicDNS proxy  
nslookup server.keiretsu.ts.net <tsdnsreflector-ip>
```