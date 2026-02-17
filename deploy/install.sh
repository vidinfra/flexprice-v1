#!/bin/bash
set -e

# FlexPrice Production Install Script
# Run as root: sudo bash install.sh

echo "=========================================="
echo "FlexPrice Production Installation"
echo "=========================================="

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Please run as root (sudo bash install.sh)${NC}"
    exit 1
fi

# Detect OS
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
    VERSION=$VERSION_ID
else
    echo -e "${RED}Cannot detect OS${NC}"
    exit 1
fi

echo -e "${GREEN}Detected OS: $OS $VERSION${NC}"

# ==========================================
# 1. Create users and directories
# ==========================================
echo -e "${YELLOW}Creating users and directories...${NC}"

# Create flexprice user
if ! id "flexprice" &>/dev/null; then
    useradd -r -s /bin/false -d /opt/flexprice flexprice
fi

# Create temporal user
if ! id "temporal" &>/dev/null; then
    useradd -r -s /bin/false -d /opt/temporal temporal
fi

# Create directories
mkdir -p /opt/flexprice/{bin,config,logs,assets}
mkdir -p /opt/temporal/{bin,config}
mkdir -p /opt/flex-front

# ==========================================
# 2. Install PostgreSQL 17
# ==========================================
echo -e "${YELLOW}Installing PostgreSQL 17...${NC}"

if [ "$OS" = "ubuntu" ] || [ "$OS" = "debian" ]; then
    # Add PostgreSQL APT repository
    sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
    wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add -
    apt-get update
    apt-get install -y postgresql-17 postgresql-client-17
elif [ "$OS" = "centos" ] || [ "$OS" = "rhel" ] || [ "$OS" = "rocky" ]; then
    dnf install -y https://download.postgresql.org/pub/repos/yum/reporpms/EL-9-x86_64/pgdg-redhat-repo-latest.noarch.rpm
    dnf -qy module disable postgresql
    dnf install -y postgresql17-server postgresql17
    /usr/pgsql-17/bin/postgresql-17-setup initdb
fi

systemctl enable postgresql
systemctl start postgresql

# Create FlexPrice database and user
sudo -u postgres psql <<EOF
CREATE USER flexprice WITH PASSWORD 'flexprice123';
CREATE DATABASE flexprice OWNER flexprice;
GRANT ALL PRIVILEGES ON DATABASE flexprice TO flexprice;

CREATE USER temporal WITH PASSWORD 'temporal123';
CREATE DATABASE temporal OWNER temporal;
CREATE DATABASE temporal_visibility OWNER temporal;
GRANT ALL PRIVILEGES ON DATABASE temporal TO temporal;
GRANT ALL PRIVILEGES ON DATABASE temporal_visibility TO temporal;
EOF

echo -e "${GREEN}PostgreSQL installed and configured${NC}"

# ==========================================
# 3. Install ClickHouse
# ==========================================
echo -e "${YELLOW}Installing ClickHouse...${NC}"

if [ "$OS" = "ubuntu" ] || [ "$OS" = "debian" ]; then
    apt-get install -y apt-transport-https ca-certificates dirmngr
    apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv 8919F6BD2B48D754
    echo "deb https://packages.clickhouse.com/deb stable main" | tee /etc/apt/sources.list.d/clickhouse.list
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y clickhouse-server clickhouse-client
elif [ "$OS" = "centos" ] || [ "$OS" = "rhel" ] || [ "$OS" = "rocky" ]; then
    yum install -y yum-utils
    yum-config-manager --add-repo https://packages.clickhouse.com/rpm/clickhouse.repo
    yum install -y clickhouse-server clickhouse-client
fi

# Configure ClickHouse
cat > /etc/clickhouse-server/users.d/flexprice.xml <<EOF
<?xml version="1.0"?>
<clickhouse>
    <users>
        <flexprice>
            <password>flexprice123</password>
            <networks>
                <ip>::/0</ip>
            </networks>
            <profile>default</profile>
            <quota>default</quota>
            <access_management>1</access_management>
        </flexprice>
    </users>
</clickhouse>
EOF

systemctl enable clickhouse-server
systemctl start clickhouse-server

# Create FlexPrice database
clickhouse-client --query "CREATE DATABASE IF NOT EXISTS flexprice"

echo -e "${GREEN}ClickHouse installed and configured${NC}"

# ==========================================
# 4. Install Redpanda
# ==========================================
echo -e "${YELLOW}Installing Redpanda...${NC}"

if [ "$OS" = "ubuntu" ] || [ "$OS" = "debian" ]; then
    curl -1sLf 'https://dl.redpanda.com/nzc4ZYQK3WRGd9sy/redpanda/cfg/setup/bash.deb.sh' | bash
    apt-get install -y redpanda
elif [ "$OS" = "centos" ] || [ "$OS" = "rhel" ] || [ "$OS" = "rocky" ]; then
    curl -1sLf 'https://dl.redpanda.com/nzc4ZYQK3WRGd9sy/redpanda/cfg/setup/bash.rpm.sh' | bash
    yum install -y redpanda
fi

# Configure Redpanda
rpk redpanda config set redpanda.developer_mode true
rpk redpanda config set pandaproxy_client.brokers '["127.0.0.1:9092"]'

# Create topics
systemctl enable redpanda
systemctl start redpanda

sleep 5  # Wait for Redpanda to start

rpk topic create events --partitions 3
rpk topic create events_lazy --partitions 3
rpk topic create events_post_processing --partitions 3
rpk topic create system_events --partitions 3
rpk topic create wallet_alert --partitions 1

echo -e "${GREEN}Redpanda installed and configured${NC}"

# ==========================================
# 5. Install Temporal
# ==========================================
echo -e "${YELLOW}Installing Temporal...${NC}"

TEMPORAL_VERSION="1.24.2"
cd /tmp
curl -LO "https://github.com/temporalio/temporal/releases/download/v${TEMPORAL_VERSION}/temporal_${TEMPORAL_VERSION}_linux_amd64.tar.gz"
tar -xzf "temporal_${TEMPORAL_VERSION}_linux_amd64.tar.gz" -C /opt/temporal/bin/
rm "temporal_${TEMPORAL_VERSION}_linux_amd64.tar.gz"

# Create Temporal config
cat > /opt/temporal/config/temporal.yaml <<EOF
log:
  level: info

persistence:
  defaultStore: default
  visibilityStore: visibility
  numHistoryShards: 4
  datastores:
    default:
      sql:
        pluginName: postgres
        databaseName: temporal
        connectAddr: 127.0.0.1:5432
        connectProtocol: tcp
        user: temporal
        password: temporal123
        maxConns: 20
        maxIdleConns: 20
    visibility:
      sql:
        pluginName: postgres
        databaseName: temporal_visibility
        connectAddr: 127.0.0.1:5432
        connectProtocol: tcp
        user: temporal
        password: temporal123
        maxConns: 20
        maxIdleConns: 20

global:
  membership:
    maxJoinDuration: 30s
  pprof:
    port: 7936

services:
  frontend:
    rpc:
      grpcPort: 7233
      membershipPort: 6933
      bindOnLocalHost: true
  history:
    rpc:
      grpcPort: 7234
      membershipPort: 6934
      bindOnLocalHost: true
  matching:
    rpc:
      grpcPort: 7235
      membershipPort: 6935
      bindOnLocalHost: true
  worker:
    rpc:
      grpcPort: 7239
      membershipPort: 6939
      bindOnLocalHost: true

clusterMetadata:
  enableGlobalNamespace: false
  failoverVersionIncrement: 10
  masterClusterName: active
  currentClusterName: active
  clusterInformation:
    active:
      enabled: true
      initialFailoverVersion: 1
      rpcName: frontend
      rpcAddress: 127.0.0.1:7233

dcRedirectionPolicy:
  policy: noop

EOF

chown -R temporal:temporal /opt/temporal

# Initialize Temporal schema
cd /opt/temporal/bin
./temporal-sql-tool --plugin postgres --ep 127.0.0.1 -p 5432 -u temporal -pw temporal123 --db temporal setup-schema -v 0.0
./temporal-sql-tool --plugin postgres --ep 127.0.0.1 -p 5432 -u temporal -pw temporal123 --db temporal update-schema -d /opt/temporal/schema/postgresql/v12/temporal/versioned
./temporal-sql-tool --plugin postgres --ep 127.0.0.1 -p 5432 -u temporal -pw temporal123 --db temporal_visibility setup-schema -v 0.0
./temporal-sql-tool --plugin postgres --ep 127.0.0.1 -p 5432 -u temporal -pw temporal123 --db temporal_visibility update-schema -d /opt/temporal/schema/postgresql/v12/visibility/versioned

echo -e "${GREEN}Temporal installed and configured${NC}"

# ==========================================
# 6. Install Node.js (for frontend)
# ==========================================
echo -e "${YELLOW}Installing Node.js 20...${NC}"

if [ "$OS" = "ubuntu" ] || [ "$OS" = "debian" ]; then
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
    apt-get install -y nodejs
elif [ "$OS" = "centos" ] || [ "$OS" = "rhel" ] || [ "$OS" = "rocky" ]; then
    curl -fsSL https://rpm.nodesource.com/setup_20.x | bash -
    yum install -y nodejs
fi

npm install -g pm2

echo -e "${GREEN}Node.js installed${NC}"

# ==========================================
# 7. Install Nginx
# ==========================================
echo -e "${YELLOW}Installing Nginx...${NC}"

if [ "$OS" = "ubuntu" ] || [ "$OS" = "debian" ]; then
    apt-get install -y nginx certbot python3-certbot-nginx
elif [ "$OS" = "centos" ] || [ "$OS" = "rhel" ] || [ "$OS" = "rocky" ]; then
    yum install -y nginx certbot python3-certbot-nginx
fi

# Copy nginx configs
cp ./nginx/flexprice-api.conf /etc/nginx/sites-available/flexprice-api
cp ./nginx/flexprice-dashboard.conf /etc/nginx/sites-available/flexprice-dashboard
cp ./nginx/signoz.conf /etc/nginx/sites-available/signoz

# Enable sites
ln -sf /etc/nginx/sites-available/flexprice-api /etc/nginx/sites-enabled/
ln -sf /etc/nginx/sites-available/flexprice-dashboard /etc/nginx/sites-enabled/
ln -sf /etc/nginx/sites-available/signoz /etc/nginx/sites-enabled/

# Test and reload nginx
nginx -t && systemctl reload nginx

systemctl enable nginx
systemctl start nginx

echo -e "${GREEN}Nginx installed${NC}"
echo -e "${YELLOW}NOTE: Run 'certbot --nginx -d your-domain.com' to enable SSL${NC}"

# ==========================================
# 8. Install systemd service files
# ==========================================
echo -e "${YELLOW}Installing systemd service files...${NC}"

cp ./systemd/*.service /etc/systemd/system/
systemctl daemon-reload

echo -e "${GREEN}Systemd services installed${NC}"

# ==========================================
# 9. Create FlexPrice config
# ==========================================
echo -e "${YELLOW}Creating FlexPrice config...${NC}"

cat > /opt/flexprice/config/config.yaml <<EOF
deployment:
  mode: "production"

server:
  address: ":8080"
  allowed_origins:
    - "https://billing.tenbyte.io"
    - "https://api-billing.tenbyte.io"
    - "http://localhost:5173"

auth:
  provider: "flexprice"
  secret: "$(openssl rand -hex 64)"
  api_key:
    header: "x-api-key"

kafka:
  brokers: "127.0.0.1:9092"
  consumer_group: "flexprice-consumer"
  topic: "events"
  topic_lazy: "events_lazy"
  tls: false
  use_sasl: false

clickhouse:
  address: 127.0.0.1:9000
  tls: false
  username: flexprice
  password: flexprice123
  database: flexprice

postgres:
  host: 127.0.0.1
  port: 5432
  user: flexprice
  password: flexprice123
  dbname: flexprice
  sslmode: disable
  max_open_conns: 25
  max_idle_conns: 10
  conn_max_lifetime_minutes: 60
  auto_migrate: true

temporal:
  address: "127.0.0.1:7233"
  tls: false
  namespace: "default"
  task_queue: "billing-task-queue"

event:
  publish_destination: "kafka"

logging:
  level: "info"

webhook:
  enabled: true
  topic: "system_events"
  pubsub: "kafka"

secrets:
  encryption_key: "$(openssl rand -hex 64)"

tenbyte:
  signoz:
    enabled: true
    endpoint: "127.0.0.1:4317"
    service_name: "tenbyte-billing"
    environment: "production"
    sample_rate: 1.0
    insecure: true
EOF

chown -R flexprice:flexprice /opt/flexprice

echo -e "${GREEN}FlexPrice config created${NC}"

# ==========================================
# 10. Build FlexPrice
# ==========================================
echo -e "${YELLOW}Building FlexPrice...${NC}"

# Install Go if not present
if ! command -v go &> /dev/null; then
    GO_VERSION="1.23.3"
    curl -LO "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
    rm -rf /usr/local/go && tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
    export PATH=$PATH:/usr/local/go/bin
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    rm "go${GO_VERSION}.linux-amd64.tar.gz"
fi

# Clone and build
cd /tmp
if [ -d "flexprice-v1" ]; then
    cd flexprice-v1
    git pull
else
    git clone https://github.com/vidinfra/flexprice-v1.git
    cd flexprice-v1
fi

go build -ldflags="-w -s" -o /opt/flexprice/bin/flexprice-app cmd/server/main.go
cp -r assets/* /opt/flexprice/assets/

chown -R flexprice:flexprice /opt/flexprice

echo -e "${GREEN}FlexPrice built${NC}"

# ==========================================
# 11. Summary
# ==========================================
echo ""
echo -e "${GREEN}=========================================="
echo "Installation Complete!"
echo "==========================================${NC}"
echo ""
echo "Services installed:"
echo "  - PostgreSQL 17 (port 5432)"
echo "  - ClickHouse (port 9000, 8123)"
echo "  - Redpanda (port 9092)"
echo "  - Temporal (port 7233)"
echo "  - Nginx (port 80, 443)"
echo ""
echo "Start services:"
echo "  sudo systemctl start temporal"
echo "  sudo systemctl start flexprice-api"
echo "  sudo systemctl start flexprice-consumer"
echo "  sudo systemctl start flexprice-worker"
echo ""
echo "Enable SSL:"
echo "  sudo certbot --nginx -d api-billing.tenbyte.io"
echo "  sudo certbot --nginx -d billing.tenbyte.io"
echo "  sudo certbot --nginx -d monitoring.tenbyte.io"
echo ""
echo "Check logs:"
echo "  journalctl -u flexprice-api -f"
echo "  journalctl -u flexprice-consumer -f"
echo "  journalctl -u flexprice-worker -f"
echo ""
echo "Start SigNoz (monitoring):"
echo "  cd /opt/flexprice-v1/deploy/signoz && docker compose up -d"
echo ""
echo "Next steps:"
echo "  1. Update /opt/flexprice/config/config.yaml with your settings"
echo "  2. Build and deploy flex-front to /opt/flex-front"
echo "  3. Start SigNoz: cd deploy/signoz && docker compose up -d"
echo "  4. Start all services"
echo ""
