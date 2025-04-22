#!/bin/sh
set -e

# Wait for DNS server to start
echo "Waiting for DNS server to be ready..."
sleep 5

# Set the DNS server to use our reflector
echo "nameserver 172.28.1.2" > /etc/resolv.conf

# Run the test query
echo ""
echo "=== Testing DNS resolution ==="
echo "Query: tsdnsreflector.tailscale.svc.cluster2.local"
echo ""

# Use nslookup to test AAAA record resolution
RESULT=$(nslookup -type=AAAA tsdnsreflector.tailscale.svc.cluster2.local 172.28.1.2 | grep -A1 "tsdnsreflector.tailscale.svc.cluster2.local" | grep "address" | awk '{print $2}')

echo "Result: $RESULT"

# Check if the result matches the expected value
if [ "$RESULT" = "fdbb:cbf8:2702::9d30" ]; then
  echo "✅ TEST PASSED: Got expected result fdbb:cbf8:2702::9d30"
  exit 0
else
  echo "❌ TEST FAILED: Expected fdbb:cbf8:2702::9d30, got $RESULT"
  exit 1
fi 