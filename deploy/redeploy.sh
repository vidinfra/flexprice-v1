#!/bin/bash
set -e

# FlexPrice Redeploy Script
# Usage: ./redeploy.sh [api|consumer|worker|all]

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SERVICE=$1

if [ -z "$SERVICE" ]; then
    echo "Usage: ./redeploy.sh [api|consumer|worker|all]"
    echo ""
    echo "Examples:"
    echo "  ./redeploy.sh api       # Redeploy only API"
    echo "  ./redeploy.sh consumer  # Redeploy only Consumer"
    echo "  ./redeploy.sh worker    # Redeploy only Worker"
    echo "  ./redeploy.sh all       # Redeploy all services"
    exit 1
fi

echo -e "${YELLOW}Pulling latest code...${NC}"
cd /opt/flexprice-v1
git pull

echo -e "${YELLOW}Building binary...${NC}"
go build -ldflags="-w -s" -o /opt/flexprice/bin/flexprice-app cmd/server/main.go

echo -e "${GREEN}Build complete${NC}"

restart_service() {
    local svc=$1
    echo -e "${YELLOW}Restarting flexprice-${svc}...${NC}"
    sudo systemctl restart flexprice-${svc}
    sleep 2
    if systemctl is-active --quiet flexprice-${svc}; then
        echo -e "${GREEN}flexprice-${svc} is running${NC}"
    else
        echo -e "${RED}flexprice-${svc} failed to start${NC}"
        journalctl -u flexprice-${svc} --no-pager -n 20
        exit 1
    fi
}

case $SERVICE in
    api)
        restart_service "api"
        ;;
    consumer)
        restart_service "consumer"
        ;;
    worker)
        restart_service "worker"
        ;;
    all)
        restart_service "api"
        restart_service "consumer"
        restart_service "worker"
        ;;
    *)
        echo -e "${RED}Unknown service: $SERVICE${NC}"
        echo "Valid options: api, consumer, worker, all"
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}Redeploy complete!${NC}"
echo ""
echo "Check logs:"
echo "  journalctl -u flexprice-${SERVICE} -f"
