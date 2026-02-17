#!/bin/bash

# FlexPrice Service Status

echo "========================================"
echo "FlexPrice Service Status"
echo "========================================"
echo ""

services=("postgresql" "clickhouse-server" "redpanda" "temporal" "flexprice-api" "flexprice-consumer" "flexprice-worker" "nginx")

for svc in "${services[@]}"; do
    if systemctl is-active --quiet $svc 2>/dev/null; then
        echo -e "✅ $svc: \033[0;32mrunning\033[0m"
    else
        echo -e "❌ $svc: \033[0;31mstopped\033[0m"
    fi
done

echo ""
echo "========================================"
echo "Resource Usage"
echo "========================================"
echo ""
echo "Memory:"
free -h | grep Mem
echo ""
echo "Disk:"
df -h / | tail -1
echo ""
echo "========================================"
echo "Quick Commands"
echo "========================================"
echo "Logs:    journalctl -u flexprice-api -f"
echo "Restart: sudo systemctl restart flexprice-api"
echo "Redeploy: ./redeploy.sh api"
