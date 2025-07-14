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

### Global Settings
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
  }
}
```

### Zone Configuration
```json
{
  "zones": {
    "zone-name": {
      "domains": ["*.example.local"],
      "backend": {
        "dnsServers": ["10.1.0.53:53"],
        "timeout": "3s"
      },
      "4via6": {
        "reflectedDomain": "example.local",
        "translateid": 1
      },
      "allowExternalClients": false
    }
  }
}
```

### 4via6 Translation

Configure IPv4-to-IPv6 translation for subnet routing:

```json
{
  "4via6": {
    "reflectedDomain": "cluster.local",  // Actual domain to resolve
    "translateid": 1                     // Site ID (matches subnet router)
  }
}
```

## Environment Variables

Configure runtime settings via environment variables:

### Server Settings
```bash
TSDNS_HOSTNAME=tsdnsreflector
TSDNS_DNS_PORT=53
TSDNS_HTTP_PORT=8080
TSDNS_BIND_ADDRESS=0.0.0.0
TSDNS_DEFAULT_TTL=300
```

### Tailscale Authentication
```bash
# Traditional auth key
TS_AUTHKEY=tskey-auth-your-key-here

# OAuth client credentials
TS_API_CLIENT_ID=tskey-client-your-id
TS_API_CLIENT_SECRET=your-secret

# OAuth with files (Kubernetes)
CLIENT_ID_FILE=/etc/tailscale/oauth/client_id
CLIENT_SECRET_FILE=/etc/tailscale/oauth/client_secret
```

### Logging
```bash
TSDNS_LOG_LEVEL=info          # debug, info, warn, error
TSDNS_LOG_FORMAT=json         # json, text
TSDNS_LOG_QUERIES=false       # Enable query logging
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