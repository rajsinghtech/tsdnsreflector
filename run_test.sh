#!/bin/bash
set -e

echo "==== Building and starting test environment ===="
docker-compose down -v || true
docker-compose build
docker-compose up -d

echo ""
echo "==== Waiting for services to start (15 seconds) ===="
sleep 15

echo ""
echo "==== Checking tsdnsreflector logs ===="
docker logs tsdnsreflector

echo ""
echo "==== Checking mock_resolver logs ===="
docker logs mock_resolver

echo ""
echo "==== Running DNS query test ===="
docker exec dns_tester nslookup -type=AAAA tsdnsreflector.tailscale.svc.cluster2.local 172.28.1.2

echo ""
echo "==== Test complete ===="
echo "The test is successful if you see fdbb:cbf8:2702::9d30 in the AAAA record response"
echo ""
echo "==== You can access the test environment with: ===="
echo "docker exec -it dns_tester sh"
echo ""
echo "To tear down the test environment, run:"
echo "docker-compose down -v" 