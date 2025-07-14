# Configuration Guide

This guide covers all configuration options for tsdnsreflector.

## Basic Configuration

Create a `config.hujson` file to map Kubernetes cluster DNS records for Tailscale clients:

```json
{
  "zones": {
    "production": {
      "domains": ["*.prod.local"],
      "backend": {"dnsServers": ["10.0.0.10:53"]},  // CoreDNS IP
      "4via6": {"reflectedDomain": "cluster.local", "translateid": 1}
    },
    "staging": {
      "domains": ["*.staging.local"],
      "backend": {"dnsServers": ["10.0.0.10:53"]},  // Same CoreDNS IP, different cluster
      "4via6": {"reflectedDomain": "cluster.local", "translateid": 2}
    }
  }
}
```

This enables Tailscale clients to resolve `api.default.svc.prod.local` and `api.default.svc.staging.local` to different IPv6 addresses, even when both clusters use overlapping IP ranges.

## Configuration Structure

tsdnsreflector uses a dual configuration system:
- **Runtime settings**: Environment variables and command-line flags
- **Business logic**: Zone configuration in `config.hujson`

### Zone Configuration (config.hujson)

```json
{
  "global": {
    "backend": {
      "dnsServers": ["8.8.8.8:53", "1.1.1.1:53"],
      "timeout": "5s",
      "retries": 3
    },
    "cache": {
      "maxSize": 10000,
      "ttl": "300s"
    }
  },
  "zones": {
    "zone-name": {
      "domains": ["*.example.local"],
      "backend": {
        "dnsServers": ["10.1.0.53:53"],
        "timeout": "3s"
      },
      "reflectedDomain": "example.local",
      "translateid": 1,
      "prefixSubnet": "fd7a:115c:a1e0:b1a:0:1::/96",
      "allowExternalClients": false,
      "cache": {
        "maxSize": 5000,
        "ttl": "600s"
      }
    }
  }
}
```

### Zone Fields

- **domains**: List of domain patterns this zone handles (supports wildcards)
- **backend**: DNS servers and connection settings for this zone
- **reflectedDomain**: Domain suffix to replace when forwarding queries (for 4via6)
- **translateid**: Site ID for 4via6 translation (enables IPv4→IPv6 conversion)
- **prefixSubnet**: IPv6 prefix for 4via6 translation (optional, auto-generated if not specified)
- **allowExternalClients**: Allow non-Tailscale clients to query this zone
- **cache**: Zone-specific cache configuration (overrides global)

## Environment Variables

Configure runtime settings via environment variables:

### Server Settings
```bash
TSDNS_HOSTNAME=tsdnsreflector        # Hostname for the service
TSDNS_DNS_PORT=53                    # DNS server port
TSDNS_HTTP_PORT=8080                 # HTTP server port (metrics/health)
TSDNS_BIND_ADDRESS=0.0.0.0           # Bind address for all services
TSDNS_DEFAULT_TTL=300                # Default DNS TTL in seconds
TSDNS_HEALTH_ENABLED=true            # Enable health endpoint
TSDNS_HEALTH_PATH=/health            # Health check path
TSDNS_METRICS_ENABLED=true           # Enable Prometheus metrics
TSDNS_METRICS_PATH=/metrics          # Metrics endpoint path
```

### Tailscale Settings
```bash
# Basic configuration
TS_AUTHKEY=tskey-auth-xxx            # Traditional auth key
TS_STATE=kube:$(POD_NAME)            # State storage (Kubernetes)
TSDNS_TS_HOSTNAME=                   # Override TSNet hostname (defaults to TSDNS_HOSTNAME)
TSDNS_TS_STATE_DIR=/tmp/tailscale    # State directory
TSDNS_TS_EXIT_NODE=false             # Act as exit node
TSDNS_TS_AUTO_SPLIT_DNS=false        # Auto-configure split DNS

# OAuth authentication (preferred)
CLIENT_ID_FILE=/etc/tailscale/oauth/client_id       # OAuth client ID file
CLIENT_SECRET_FILE=/etc/tailscale/oauth/client_secret # OAuth secret file
TS_API_CLIENT_ID=tskey-client-xxx    # OAuth client ID (fallback)
TS_API_CLIENT_SECRET=xxx             # OAuth secret (fallback)

# OAuth advanced settings
TSDNS_TS_OAUTH_URL=https://login.tailscale.com  # OAuth endpoint
TSDNS_TS_OAUTH_TAGS=tag:dns                     # Device tags
TSDNS_TS_OAUTH_EPHEMERAL=true                   # Ephemeral device
TSDNS_TS_OAUTH_PREAUTHORIZED=true               # Pre-authorized device
```

### Logging
```bash
TSDNS_LOG_LEVEL=info          # debug, info, warn, error
TSDNS_LOG_FORMAT=json         # json, text
TSDNS_LOG_QUERIES=false       # Enable DNS query logging
TSDNS_LOG_FILE=               # Log file path (empty = stdout)
```

## Hot Reload

Update configuration without restarting:

```bash
# Send SIGHUP signal
kill -HUP $(pidof tsdnsreflector)

# With Docker
docker kill -s HUP container_name

# With Kubernetes
kubectl exec -n tsdnsreflector tsdnsreflector-0 -- kill -HUP 1
```

### Reloadable Settings
- Zone definitions and routing rules
- Backend DNS servers and timeouts
- Logging configuration
- Cache settings

### Non-Reloadable Settings
- Network ports and bind addresses
- Tailscale authentication settings

## Security Considerations

### External Client Access

By default, only Tailscale clients (100.64.0.0/10) can query tsdnsreflector. To allow external access:

```json
{
  "zones": {
    "public": {
      "domains": ["*.api.example.com"],
      "allowExternalClients": true  // ⚠️ Use carefully
    }
  }
}
```

**Important**: 4via6 zones cannot allow external clients (enforced by validation).

### Split-DNS Setup (Tailscale)

After deploying tsdnsreflector, configure Tailscale to route specific domains:

1. **Access Admin Console**: Go to https://login.tailscale.com/admin/dns
2. **Add Custom Nameserver**:
   - Select "Custom" from Add Nameserver dropdown
   - Enter tsdnsreflector's Tailscale IP (e.g., 100.78.103.47)
3. **Configure Domain Restriction**:
   - Toggle "Restrict search domain"
   - Enter your zone domain (e.g., `cluster1.local`)
   - Save changes

This routes all queries for `*.cluster1.local` to tsdnsreflector while other domains use default DNS.

### Best Practices
- Use separate zones for public vs private DNS
- Monitor external access via Prometheus metrics
- Enable query logging for external-facing zones
- Implement rate limiting at the network level
- Use OAuth authentication instead of auth keys for production
- Configure zone-specific cache settings for frequently accessed domains
- Set `allowExternalClients: false` for sensitive internal zones