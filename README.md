<div align="center">
  <img src="assets/logo.svg" alt="tsdnsreflector" width="120" height="120">
  
  # tsdnsreflector

  **DNS proxy for Tailscale networks that just works**

  [![CI](https://github.com/rajsinghtech/tsdnsreflector/actions/workflows/ci.yml/badge.svg)](https://github.com/rajsinghtech/tsdnsreflector/actions/workflows/ci.yml)
  [![Docker](https://github.com/rajsinghtech/tsdnsreflector/actions/workflows/docker.yml/badge.svg)](https://github.com/rajsinghtech/tsdnsreflector/actions/workflows/docker.yml)
  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
</div>

tsdnsreflector is a DNS proxy that solves routing conflicts in Tailscale networks and enables external access to dns servers on the tailnet.

## Problems it solves

### Multiple Kubernetes Clusters with Overlapping IPs
When you have multiple Kubernetes clusters using the same subnet ranges (e.g., both using `10.0.0.0/16`), Tailscale clients cannot distinguish between clusters when resolving service DNS records.

**Example scenario**:
- Cluster A: `api.default.svc.cluster.local` → `10.0.0.1`
- Cluster B: `api.default.svc.cluster.local` → `10.0.0.1` (same IP!)

tsdnsreflector solves this by mapping cluster-specific domains to 4via6 IPv6 addresses:
- `api.default.svc.cluster1.local` → `fd7a:115c:a1e0:b1a:0:1:a00:1` (Cluster A)
- `api.default.svc.cluster2.local` → `fd7a:115c:a1e0:b1a:0:2:a00:1` (Cluster B)

### DNS Proxy for External Clients
External clients need access to DNS servers or MagicDNS within your Tailscale network but cannot reach them directly.

**Example scenario**:
- Internal DNS server at `100.64.1.10:53` (Tailscale IP or Subnet Router Advertised IP)
- External monitoring system needs to resolve internal domains
- Direct access blocked by network boundaries

tsdnsreflector bridges this gap by:
- Connecting to internal DNS servers via Tailscale
- Serving external clients from a public IP
- Enabling secure DNS resolution without VPN access

**Use cases**:
- CI/CD systems resolving internal service names
- External monitoring accessing private DNS records  
- Third-party integrations needing internal domain resolution

## Quick Start

### 1. Create Configuration
Create `config.hujson` with your DNS zones:

```json
{
  "zones": {
    "internal": {
      "domains": ["*.internal.local"],
      "backend": {"dnsServers": ["100.64.1.10:53"]}, // Tailscale exposed DNS server
      "allowExternalClients": true
    },
    "cluster": {
      "domains": ["*.cluster.local"],
      "backend": {"dnsServers": ["10.1.0.10:53"]}, // Subnet-routed DNS
      "4via6": {"reflectedDomain": "cluster.local", "translateid": 1}
    }
  }
}
```

### 2. Run with Docker
```bash
# Run with config file
docker run -d --name tsdnsreflector \
  -p 53:53/udp \
  -v ./config.hujson:/config.hujson \
  -e TS_AUTHKEY=tskey-auth-your-key \
  ghcr.io/rajsinghtech/tsdnsreflector:latest -config /config.hujson

# Test IPv6 translation  
nslookup -type=AAAA service.internal.local 100.x.x.x
```

## How it works

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│ Tailscale Client│    │  tsdnsreflector  │    │   Kubernetes    │
│                 │────┤ DNS Query        │────┤ CoreDNS         │
│ Query:          │ 1  │ *.cluster1.local │ 2  │ 10.0.0.10:53    │
│ api.default.svc │    │                  │    │                 │
│ .cluster1.local │    │ Translates:      │◄───┤ Returns:        │
│                 │    │ 10.0.0.1 →       │ 3  │ 10.0.0.1        │
│                 │◄───┤ fd7a:115c:a1e0:  │    │                 │
│ Gets: IPv6      │ 4  │ b1a:0:1:a00:1    │    │                 │
│ 4via6 address   │    │                  │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘

Flow:
1. Client queries api.default.svc.cluster1.local
2. tsdnsreflector forwards to CoreDNS as cluster.local  
3. CoreDNS returns IPv4 address (10.0.0.1)
4. tsdnsreflector converts to 4via6 IPv6 and returns to client
```

This allows multiple Kubernetes clusters with overlapping IPs to be uniquely addressable via different IPv6 addresses.

## Documentation

- **[Configuration Guide](docs/CONFIGURATION.md)** - All config options and examples
- **[Deployment Guide](docs/DEPLOYMENT.md)** - Docker, Kubernetes, systemd setups  
- **[Tailscale Integration](docs/TAILSCALE.md)** - Authentication, networking, troubleshooting

## Features

- **Multi-Cluster DNS** - Resolve Kubernetes services across clusters with overlapping IPs
- **DNS Proxy Bridge** - External clients access internal DNS servers via Tailscale
- **4via6 Translation** - Automatic IPv4→IPv6 conversion for unique addressing
- **TSNet Integration** - Connects to DNS servers on Tailscale IPs and subnet routes
- **MagicDNS Proxy** - External clients can resolve `.ts.net` domains  
- **Zone-Based Routing** - Map different domains to different DNS servers
- **Hot Reload** - Update config without restarting (SIGHUP)
- **Production Ready** - Health checks, metrics, security hardening
- **Kubernetes Native** - StatefulSet, RBAC, OAuth support

## Real-World Example

Here's how we use it for Kubernetes access:

### 1. Subnet Router Setup
```yaml
# Kubernetes Connector resource
apiVersion: tailscale.com/v1alpha1
kind: Connector
metadata:
  name: k8s-subnet-router
spec:
  hostname: k8s-subnet-router
  subnetRouter:
    advertiseRoutes:
    - fd7a:115c:a1e0:b1a:0:1::/96  # 4via6 prefix for site ID 1
  tags:
  - tag:k8s
```

### 2. tsdnsreflector Config
```json
{
  "zones": {
    "cluster1": {
      "domains": ["*.cluster1.local"], 
      "backend": {"dnsServers": ["10.1.0.10:53"]}, # CoreDNS IP
      "4via6": {"reflectedDomain": "cluster.local", "translateid": 1}
    },
    "cluster2": {
      "domains": ["*.cluster2.local"], 
      "backend": {"dnsServers": ["10.2.0.10:53"]}, # Same CoreDNS IP, different cluster
      "4via6": {"reflectedDomain": "cluster.local", "translateid": 2}
    }
  }
}
```

### 3. Split-DNS Setup
Configure in Tailscale admin console (https://login.tailscale.com/admin/dns):

1. **Add Nameserver**: Custom → Enter tsdnsreflector IP (100.x.x.x)
2. **Restrict Search Domain**: Enable and enter `cluster1.local`
3. **Save Changes**

This routes all `*.cluster1.local` queries to tsdnsreflector while other domains use default DNS.

### 4. Result
```bash
# Resolve Kubernetes services from Tailscale clients
curl api.default.svc.cluster1.local  # Cluster A 
curl api.default.svc.cluster2.local  # Cluster B

# Both work despite overlapping IPs via 4via6 translation
```

## License

MIT License - see [LICENSE](LICENSE) file.

---

Built by the Tailscale community for solving real networking problems.