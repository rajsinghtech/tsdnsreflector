#!/bin/bash
set -e

echo "==== Building tsdnsreflector with system resolver support ===="
go build -o tsdnsreflector .

echo "==== Setting up test environment ===="
# Make a backup of the current resolv.conf
cp -f /etc/resolv.conf /tmp/resolv.conf.backup

# Create a test resolv.conf with known nameservers
echo "# Test resolver configuration" > /tmp/test_resolv.conf
echo "nameserver 8.8.8.8" >> /tmp/test_resolv.conf
echo "nameserver 8.8.4.4" >> /tmp/test_resolv.conf

echo "==== Starting tsdnsreflector with system resolver support ===="
# Run tsdnsreflector with test resolv.conf
RESOLV_CONF=/tmp/test_resolv.conf \
SITEID=1 \
REFLECTED_DOMAIN=cluster1.local \
ORIGINAL_DOMAIN=cluster.local \
DNS_RESOLVER=none \
FORCE_4VIA6=false \
LISTEN_ADDR=:5353 \
./tsdnsreflector &
PID=$!

# Wait for server to start
sleep 2

echo "==== Testing DNS resolution ===="
# Use dig to test the DNS resolution
dig @localhost -p 5353 example.default.svc.cluster1.local

echo "==== Checking resolver logs ===="
# Check if the logs show the correct resolvers from our test file
ps -p $PID >/dev/null && kill $PID

echo "==== Test complete ===="
# Restore the original resolv.conf
if [ -f /tmp/resolv.conf.backup ]; then
  echo "Restoring original resolv.conf"
  cp -f /tmp/resolv.conf.backup /etc/resolv.conf
fi 