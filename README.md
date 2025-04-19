# tsdnsreflector

A DNS server that reflects IPv4 addresses to Tailscale 4via6 IPv6 addresses.

## Overview

tsdnsreflector serves as a DNS server on port 53 that converts IPv4 addresses to Tailscale's 4via6 IPv6 format. When a client requests a hostname in the configured IPv6 domain, the server:

1. Converts the domain name to the corresponding IPv4 domain
2. Looks up the A record for that domain
3. Converts the IPv4 address to an IPv6 address using the Tailscale 4via6 format with the given SITE_ID
4. Returns the IPv6 address to the client

This is particularly useful for networks with overlapping IPv4 subnets when using Tailscale's 4via6 subnet router feature.

## Configuration

The application requires the following environment variables:

- `SITE_ID`: The site ID used in the Tailscale 4via6 address format (must be between 0 and 65535)
- `IPV6_DOMAIN`: The domain suffix for which IPv6 addresses will be returned
- `IPV4_DOMAIN`: The domain suffix used for A record lookups

All environment variables are required. The application will fail to start if any of them are not provided.

## Example

Given the following configuration:
```
SITE_ID = 7
IPV6_DOMAIN = cluster1.local
IPV4_DOMAIN = cluster.local
```

When a client requests `test.default.svc.cluster1.local`, the server will:
1. Convert to `test.default.svc.cluster.local`
2. Look up the A record, which resolves to `10.1.1.0`
3. Convert `10.1.1.0` to `fd7a:115c:a1e0:b1a:0:7:a01:100` using SITE_ID=7
4. Return the AAAA record with the IPv6 address

## Docker Usage

```bash
docker build -t yourusername/tsdnsreflector:latest .

docker run -p 53:53/udp -p 53:53/tcp \
  -e SITE_ID=7 \
  -e IPV6_DOMAIN=cluster1.local \
  -e IPV4_DOMAIN=cluster.local \
  yourusername/tsdnsreflector:latest
```

All environment variables are required. The container will fail to start if any of them are not provided.

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

## References

- [Tailscale 4via6 subnet routers](https://tailscale.com/kb/1201/4via6-subnets) 