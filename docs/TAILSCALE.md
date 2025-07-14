# Tailscale Integration

This guide covers Tailscale-specific configuration and integration patterns.

## Authentication Methods

### Traditional Auth Keys
Simple setup for development and testing:

```bash
export TS_AUTHKEY=tskey-auth-your-key-here
./tsdnsreflector -config config.hujson
```

### OAuth Client Credentials (Recommended)
More secure for production deployments:

```bash
export TS_API_CLIENT_ID=tskey-client-your-id
export TS_API_CLIENT_SECRET=your-secret
./tsdnsreflector -config config.hujson
```

### File-based OAuth (Kubernetes)
For containerized deployments:

```bash
export CLIENT_ID_FILE=/etc/tailscale/oauth/client_id
export CLIENT_SECRET_FILE=/etc/tailscale/oauth/client_secret
```

## TSNet Integration

tsdnsreflector uses TSNet for full Tailscale network participation:

### Key Features
- **MagicDNS Resolution**: Resolve `.ts.net` domains
- **Subnet Route Access**: Reach DNS servers on advertised subnets
- **Client Detection**: Identify Tailscale vs external clients
- **State Persistence**: Maintain device identity across restarts

### Configuration
```json
{
  "tailscale": {
    "hostname": "tsdnsreflector",
    "stateDir": "/var/lib/tailscale"
  }
}
```

### Environment Variables
```bash
TS_STATE=kube:tsdnsreflector-state  # Kubernetes state storage
TS_HOSTNAME=tsdnsreflector          # Device hostname
```

## MagicDNS Proxy

Allow external clients to resolve Tailscale domains:

### How It Works
1. External client queries `server.keiretsu.ts.net`
2. tsdnsreflector uses TSNet to resolve via MagicDNS
3. Returns the Tailscale IP to external client

### Configuration
No special configuration needed - MagicDNS proxy is automatic for `.ts.net` domains.

### Testing
```bash
# From external client
nslookup server.keiretsu.ts.net <tsdnsreflector-ip>
# Should return Tailscale IP (100.x.x.x)
```

## Subnet Routing

### TSNet Dial Support
tsdnsreflector automatically uses TSNet for DNS queries when available, enabling access to subnet-routed DNS servers:

```go
// DNS queries route through TSNet
conn, err := tsnetServer.Dial(ctx, "udp", "10.0.0.10:53")
```

## Split-DNS Configuration

### Tailscale Admin Console
Configure restricted nameservers (split DNS) at https://login.tailscale.com/admin/dns:

1. **Add Custom Nameserver**:
   - Select "Custom" from Add Nameserver dropdown
   - Enter tsdnsreflector's Tailscale IP (100.x.x.x)

2. **Configure Restricted Domain**:
   - Toggle "Restrict search domain" 
   - Enter domain: `cluster1.local`
   - Save changes

3. **Optional - Override DNS**:
   - Enable "Override DNS servers" to force all clients to use tailnet DNS
   - Only enable if all devices can reach your nameservers

### Multiple Domains
To route multiple domains to tsdnsreflector:
- Add separate nameserver entries for each domain
- Example: `cluster1.local`, `staging.local`, `dev.local`

### Client-Side Testing
```bash
# Verify split-DNS routing
tailscale status --peers

# Test domain resolution
nslookup kubernetes.default.svc.cluster1.local
```

## Client Detection

tsdnsreflector identifies client types for security and feature control:

### Detection Logic
```go
// Tailscale clients use 100.64.0.0/10 CGNAT range
isTailscaleClient := ip.As4()[0] == 100 && (ip.As4()[1]&0xC0) == 0x40
```

### Behavior by Client Type
- **Tailscale clients**: Full access to all zones and features
- **External clients**: Limited to MagicDNS and explicitly allowed zones

## State Management

### Kubernetes State Storage
```bash
TS_STATE=kube:$(POD_NAME)
```

This stores Tailscale state in a Kubernetes Secret, ensuring:
- Device identity persists across pod restarts
- No need for re-authentication
- Automatic cleanup when pod is deleted

### Required RBAC
```yaml
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "update", "patch", "create"]
```

## OAuth Configuration

### Kubernetes Secret
```bash
kubectl create secret generic tailscale-oauth \
  --from-literal=client_id=tskey-client-your-id \
  --from-literal=client_secret=your-secret \
  -n tsdnsreflector
```

### Advanced OAuth Options
```json
{
  "tailscale": {
    "oauth": {
      "tags": ["tag:dns"],
      "ephemeral": true,
      "preauthorized": true
    }
  }
}
```

## Network Configuration

### Dual Binding
tsdnsreflector binds to both Tailscale and external interfaces:

```bash
# Tailscale IP (for Tailscale clients)
100.x.x.x:53

# External IP (for container port forwarding)
0.0.0.0:53
```

### Port Configuration
```bash
TSDNS_DNS_PORT=53        # DNS server port
TSDNS_HTTP_PORT=8080     # Health/metrics HTTP port
TSDNS_BIND_ADDRESS=0.0.0.0  # Bind address
```

## Security Considerations

### Device Trust
- Use OAuth for production deployments
- Tag devices appropriately (`tag:dns`)
- Enable preauthorization for automated deployments
- Use ephemeral nodes for temporary deployments

### Network Security
- Tailscale traffic is encrypted by default
- No need for additional TLS for Tailscale clients
- External clients should use secure networks

### Access Control
- External clients have limited access by default
- Use zone-based `allowExternalClients` carefully
- Monitor external access via metrics

## Troubleshooting

### Common Issues

1. **Device not appearing in Tailscale**
   - Check auth key/OAuth credentials
   - Verify network connectivity
   - Review logs for authentication errors

2. **MagicDNS not resolving**
   - Confirm TSNet initialization
   - Check Tailscale connectivity status
   - Verify domain format (must end in `.ts.net`)

3. **Subnet routes not accessible**
   - Ensure subnet router advertises routes
   - Check route approval in admin console
   - Verify `--accept-routes` on clients

### Debug Commands
```bash
# Check Tailscale status
tailscale status

# Test direct TSNet connectivity
tailscale ping <target-ip>

# View device information
tailscale status --peers --active
```

## Best Practices

### Authentication
- Use OAuth for production
- Rotate credentials regularly
- Use device tags for organization
- Enable preauthorization for CI/CD

### Networking
- Configure split-DNS properly
- Test from multiple client types
- Monitor Tailscale connectivity
- Use health checks effectively

### Monitoring
- Track Tailscale status in health endpoint
- Monitor client type metrics
- Alert on authentication failures
- Log MagicDNS resolution patterns