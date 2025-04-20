# tsdnsreflector

A DNS server that reflects IPv4 addresses to Tailscale 4via6 IPv6 addresses.

## Overview

tsdnsreflector serves as a DNS server that converts IPv4 addresses to Tailscale's 4via6 IPv6 format. When a client requests a hostname in the configured IPv6 domain, the server:

1. Converts the domain name to the corresponding IPv4 domain
2. Looks up the A record for that domain
3. Converts the IPv4 address to an IPv6 address using the Tailscale 4via6 format with the given SITE_ID
4. Returns the IPv6 address to the client

This is particularly useful for networks with overlapping IPv4 subnets when using Tailscale's 4via6 subnet router feature.

### DNS Query Behavior

When a client queries a hostname in the IPv6 domain:

- **For AAAA queries**: Returns the IPv6 address in Tailscale 4via6 format.
- **For A queries**: Also returns the IPv6 address as an AAAA record, forcing clients to use IPv6.

This ensures that all traffic to the IPv6 domain goes through the Tailscale 4via6 routes, even when applications request A records.

## Configuration

The application requires the following environment variables:

- `SITE_ID`: The site ID used in the Tailscale 4via6 address format (must be between 0 and 65535)
- `IPV6_DOMAIN`: The domain suffix for which IPv6 addresses will be returned
- `IPV4_DOMAIN`: The domain suffix used for A record lookups

The following environment variables are optional:

- `DNS_RESOLVER`: Custom DNS resolver to use for lookups in the format "host:port" (e.g., "8.8.8.8:53"). If not specified, the system resolver will be used.
- `PORT`: Custom port to run the DNS server on (e.g., "5353"). If not specified, the standard DNS port 53 will be used.

All required environment variables must be provided or the application will fail to start.

## Example

Given the following configuration:
```
SITE_ID = 7
IPV6_DOMAIN = cluster1.local
IPV4_DOMAIN = cluster.local
DNS_RESOLVER = 8.8.8.8:53  # Optional, uses Google DNS
PORT = 5353               # Optional, uses port 5353 instead of 53
```

When a client requests `test.default.svc.cluster1.local`, the server will:
1. Convert to `test.default.svc.cluster.local`
2. Look up the A record, which resolves to `10.1.1.0` (using the specified DNS resolver if provided)
3. Convert `10.1.1.0` to `fd7a:115c:a1e0:b1a:0:7:a01:100` using SITE_ID=7
4. Return the AAAA record with the IPv6 address

## Docker Usage

```bash
docker build -t yourusername/tsdnsreflector:latest .

docker run -p 5353:5353/udp -p 5353:5353/tcp \
  -e SITE_ID=7 \
  -e IPV6_DOMAIN=cluster1.local \
  -e IPV4_DOMAIN=cluster.local \
  -e DNS_RESOLVER=8.8.8.8:53 \  # Optional
  -e PORT=5353 \                # Optional
  yourusername/tsdnsreflector:latest
```

Note: If you specify a custom PORT, make sure to also update the port mapping in your docker run command.

All required environment variables must be provided or the container will fail to start.

## Kubernetes Deployment

1. Apply the configuration:

```bash
kubectl apply -f kubernetes/deployment.yaml
kubectl apply -f kubernetes/service.yaml
```

2. Verify the deployment:

```bash
kubectl get pods -l app=tsdnsreflector
kubectl get svc tsdnsreflector
```

Note: If you run the server on a non-standard port in Kubernetes, you'll need to update both the deployment.yaml (container ports) and service.yaml (targetPort) to match.

## References

- [Tailscale 4via6 subnet routers](https://tailscale.com/kb/1201/4via6-subnets) 