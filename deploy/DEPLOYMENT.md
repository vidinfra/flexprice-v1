# FlexPrice Production Deployment & Handover Guide

**Last Updated:** February 19, 2026
**Server:** 163.61.156.37
**SSH Alias:** `ssh FlexPrice`

---

## Quick Access (Copy-Paste Ready)

```bash
# SSH to server
ssh FlexPrice

# Check ALL services status (one command)
sudo systemctl status flexprice-api flexprice-consumer flexprice-worker \
     signoz-otel-collector signoz-query-service \
     temporal redpanda clickhouse-server postgresql zookeeper nginx

# View logs (real-time)
journalctl -u flexprice-api -f          # API logs
journalctl -u flexprice-consumer -f     # Consumer logs
journalctl -u flexprice-worker -f       # Worker logs
journalctl -u signoz-otel-collector -f  # Monitoring logs

# Restart all FlexPrice services
sudo systemctl restart flexprice-api flexprice-consumer flexprice-worker

# Check health
curl https://api-billing.tenbyte.io/health   # API
curl https://monitoring.tenbyte.io/          # SigNoz UI
```

---

## Credentials & Access

### Server Access
| Item | Value |
|------|-------|
| IP Address | `163.61.156.37` |
| SSH Alias | `FlexPrice` (configured in ~/.ssh/config) |
| SSH User | Check your SSH config |

### Database Credentials
| Database | Host | Port | User | Password | Database |
|----------|------|------|------|----------|----------|
| PostgreSQL | 127.0.0.1 | 5432 | flexprice | flexprice123 | flexprice |
| ClickHouse | 127.0.0.1 | 9000 | flexprice | flexprice123 | flexprice |
| Temporal DB | 127.0.0.1 | 5432 | temporal | temporal123 | temporal |

### API Keys & Secrets
- **Auth Secret:** Located in `/opt/flexprice-api/config/config.yaml` under `auth.secret`
- **Encryption Key:** Located in `/opt/flexprice-api/config/config.yaml` under `secrets.encryption_key`
- **Tenbyte Tenant ID:** `tenant_01KGHBCRK45K5C1KJATZFQ332Y`

### URLs
| Service | URL |
|---------|-----|
| API | https://api-billing.tenbyte.io |
| Dashboard | https://billing.tenbyte.io |
| API Health | https://api-billing.tenbyte.io/health |
| Monitoring | https://monitoring.tenbyte.io (SigNoz) |

### Test Login
- **Email:** mantis@yopmail.com
- **Password:** password123 (CHANGE THIS!)

---

## Architecture Overview

```
                              ┌─────────────────────────────────────────────────┐
                              │              NGINX (SSL Termination)            │
                              │   billing.tenbyte.io     → /opt/flex-front      │
                              │   api-billing.tenbyte.io → :8000 (FlexPrice)    │
                              │   monitoring.tenbyte.io  → :8080 (SigNoz)       │
                              └──────────────────────┬──────────────────────────┘
                                                     │
          ┌──────────────────────────────────────────┼──────────────────────────────────┐
          │                                          │                                  │
          ▼                                          ▼                                  ▼
┌───────────────────┐                    ┌───────────────────┐            ┌───────────────────┐
│  flexprice-api    │                    │ flexprice-consumer│            │ flexprice-worker  │
│    (port 8000)    │──events──┐         │    (port 8081)    │            │    (port 8082)    │
│   mode: "api"     │          │    ┌────│  mode: "consumer" │            │mode:"temporal_worker"│
└────────┬──────────┘          │    │    └─────────┬─────────┘            └─────────┬─────────┘
         │                     ▼    ▼              │                                │
         │              ┌─────────────────┐        │                                │
         │              │    Redpanda     │────────┘                                │
         │              │     (9092)      │                                         │
         │              │  Event Broker   │                                         │
         │              └─────────────────┘                                         │
         │                                                                          │
         │              ┌───────────────────────────────────────────────────────────┤
         │              │                                                           │
         ▼              ▼                                                           ▼
┌─────────────┐    ┌─────────────┐                                         ┌───────────────┐
│ PostgreSQL  │    │ ClickHouse  │◄──────────────────────────┐             │   Temporal    │
│   (5432)    │    │   (9000)    │                           │             │    (7233)     │
│  Main Data  │    │  Analytics  │                           │             │   Workflows   │
└─────────────┘    └──────┬──────┘                           │             └───────────────┘
                          │                                  │
                          │    ┌─────────────────────────────┴─────────────────────────────┐
                          │    │              SigNoz Monitoring Stack                      │
                          │    │  ┌─────────────────┐              ┌──────────────────┐   │
                          └────┼─▶│  OTel Collector │───traces────▶│  Query Service   │   │
                               │  │   (4317/4318)   │              │   (port 8080)    │   │
                               │  └─────────────────┘              └──────────────────┘   │
                               │         ▲                                                │
                               └─────────┼────────────────────────────────────────────────┘
                                         │
                        FlexPrice services send traces via OTLP
```

---

## Directory Structure (/opt/)

```
/opt/
├── flexprice/                      # Shared FlexPrice resources
│   ├── bin/
│   │   └── flexprice-app           # Single Go binary (used by all 3 services)
│   ├── scripts/
│   │   ├── backup.sh               # Database backup script
│   │   └── restore.sh              # Database restore script
│   └── backups/                    # Database backups (7-day retention)
│       ├── postgres/               # PostgreSQL backups (.sql.gz)
│       └── clickhouse/             # ClickHouse backups (.tar.gz)
│
├── flexprice-api/                  # API server working directory
│   └── config/
│       ├── config.yaml             # mode: api, port: 8000
│       └── rbac/roles.json
│
├── flexprice-consumer/             # Kafka consumer working directory
│   └── config/
│       ├── config.yaml             # mode: consumer, port: 8081
│       └── rbac/roles.json
│
├── flexprice-worker/               # Temporal worker working directory
│   └── config/
│       ├── config.yaml             # mode: temporal_worker, port: 8082
│       └── rbac/roles.json
│
├── flexprice-v1/                   # Git repository (source code)
│   ├── cmd/server/main.go          # Application entry point
│   ├── internal/                   # Business logic
│   ├── deploy/                     # This documentation & scripts
│   └── ...
│
├── flex-front/                     # Frontend (React/Vite)
│   ├── dist/                       # Built files (nginx serves this)
│   ├── .env                        # VITE_API_URL=https://api-billing.tenbyte.io/v1
│   └── ...
│
├── signoz/                         # SigNoz Monitoring (deployed 2026-02-18)
│   ├── bin/
│   │   ├── signoz-otel-collector   # OpenTelemetry Collector
│   │   ├── signoz-query-service    # Query Service & UI
│   │   └── signoz-schema-migrator  # DB schema tool
│   ├── config/
│   │   └── otel-collector-config.yaml
│   ├── web/                        # SigNoz frontend assets
│   └── data/                       # Local data
│
├── zookeeper/                      # ZooKeeper (required by ClickHouse cluster)
│   └── data/
│
└── temporal/                       # Temporal server
    ├── bin/                        # Temporal binaries
    └── config/                     # Temporal config
```

**Important:** Each service reads `config/config.yaml` from its working directory. The systemd service files set `WorkingDirectory` appropriately.

---

## Services & Ports

| Service | Port | Systemd Unit | Config Path |
|---------|------|--------------|-------------|
| flexprice-api | 8000 | flexprice-api.service | /opt/flexprice-api/config/config.yaml |
| flexprice-consumer | 8081 | flexprice-consumer.service | /opt/flexprice-consumer/config/config.yaml |
| flexprice-worker | 8082 | flexprice-worker.service | /opt/flexprice-worker/config/config.yaml |
| signoz-otel-collector | 4317, 4318 | signoz-otel-collector.service | /opt/signoz/config/otel-collector-config.yaml |
| signoz-query-service | 8080 | signoz-query-service.service | (env vars in systemd unit) |
| PostgreSQL | 5432 | postgresql.service | /etc/postgresql/17/main/ |
| ClickHouse | 9000, 8123 | clickhouse-server.service | /etc/clickhouse-server/ |
| Redpanda | 9092 | redpanda.service | /etc/redpanda/ |
| Temporal | 7233 | temporal.service | /opt/temporal/config/ |
| ZooKeeper | 2181 | zookeeper.service | /opt/zookeeper/conf/ |
| Nginx | 80, 443 | nginx.service | /etc/nginx/ |

**Note:** FlexPrice API runs on port 8000 internally. SigNoz Query Service uses port 8080. Both are proxied through Nginx.

---

## Deployment Scripts

All scripts are located in `/opt/flexprice-v1/deploy/`

### status.sh - Check All Services
```bash
# Shows status of all services + resource usage
sudo /opt/flexprice-v1/deploy/status.sh
```
**Output:**
- Service status (running/stopped) for all 8 services
- Memory usage
- Disk usage
- Quick command hints

### redeploy.sh - Deploy Code Changes
```bash
# Redeploy specific service
sudo /opt/flexprice-v1/deploy/redeploy.sh api
sudo /opt/flexprice-v1/deploy/redeploy.sh consumer
sudo /opt/flexprice-v1/deploy/redeploy.sh worker

# Redeploy all services
sudo /opt/flexprice-v1/deploy/redeploy.sh all
```
**What it does:**
1. `git pull` latest code
2. `go build` new binary
3. Restart specified service(s)
4. Verify service is running
5. Show logs if failed

### backup.sh - Database Backup
```bash
# Backup everything
sudo /opt/flexprice-v1/deploy/backup/backup.sh all

# Backup specific database
sudo /opt/flexprice-v1/deploy/backup/backup.sh postgres
sudo /opt/flexprice-v1/deploy/backup/backup.sh clickhouse
```
**Output:** Compressed backups in `/opt/flexprice/backups/`

### restore.sh - Database Restore
```bash
# Restore PostgreSQL
sudo /opt/flexprice-v1/deploy/backup/restore.sh postgres /opt/flexprice/backups/postgres/flexprice_YYYYMMDD.sql.gz

# Restore ClickHouse
sudo /opt/flexprice-v1/deploy/backup/restore.sh clickhouse /opt/flexprice/backups/clickhouse/flexprice_YYYYMMDD.tar.gz
```

### install.sh - Full Installation (First Time Only)
```bash
# WARNING: Only run on fresh server
sudo /opt/flexprice-v1/deploy/install.sh
```
**What it installs:**
- PostgreSQL 17
- ClickHouse
- Redpanda
- Temporal
- Node.js 20
- Nginx
- All systemd services
- FlexPrice binary

---

## Common Operations

### Deploy New Code (Manual Method)
```bash
# SSH to server
ssh FlexPrice

# Pull latest code
cd /opt/flexprice-v1
git pull

# Rebuild binary (takes ~2-3 minutes)
go build -ldflags="-w -s" -o /opt/flexprice/bin/flexprice-app cmd/server/main.go

# Restart services
sudo systemctl restart flexprice-api flexprice-consumer flexprice-worker

# Verify
curl https://api-billing.tenbyte.io/health
```

### Deploy Frontend Changes
```bash
ssh FlexPrice
cd /opt/flex-front
git pull
npm install  # Only if package.json changed
npm run build
# No restart needed - nginx serves static files
```

### Rollback (if something breaks)
```bash
# Check recent commits
cd /opt/flexprice-v1
git log --oneline -10

# Rollback to previous commit
git checkout <commit-hash>

# Rebuild and restart
go build -ldflags="-w -s" -o /opt/flexprice/bin/flexprice-app cmd/server/main.go
sudo systemctl restart flexprice-api flexprice-consumer flexprice-worker
```

### Update Config
```bash
# Edit the config (use correct path for the service)
sudo nano /opt/flexprice-api/config/config.yaml

# Restart the service
sudo systemctl restart flexprice-api
```

### Add New User
```bash
# Connect to PostgreSQL
sudo -u postgres psql -d flexprice

# Check existing users
SELECT id, email, status FROM users;

# If user exists but can't login, check auths table
SELECT * FROM auths WHERE user_id = 'user_xxx';

# Create auth record with password (use Python to generate bcrypt hash)
python3 -c "import bcrypt; print(bcrypt.hashpw(b'newpassword', bcrypt.gensalt()).decode())"

# Insert auth record
INSERT INTO auths (user_id, provider, token, status, created_at, updated_at)
VALUES ('user_xxx', 'flexprice', '<bcrypt_hash>', 'published', NOW(), NOW());
```

---

## Automated Backups

### Cron Job (runs daily at 2 AM)
```
0 2 * * * /opt/flexprice/scripts/backup.sh all >> /var/log/flexprice-backup.log 2>&1
```

### Backup Locations
```
/opt/flexprice/backups/
├── postgres/flexprice_YYYYMMDD_HHMMSS.sql.gz
└── clickhouse/flexprice_YYYYMMDD_HHMMSS.tar.gz
```

### Manual Backup
```bash
# Full backup
sudo /opt/flexprice/scripts/backup.sh all

# PostgreSQL only
sudo /opt/flexprice/scripts/backup.sh postgres

# ClickHouse only
sudo /opt/flexprice/scripts/backup.sh clickhouse
```

### Restore from Backup
```bash
# Restore latest PostgreSQL backup
sudo /opt/flexprice/scripts/restore.sh postgres

# Restore specific backup
sudo /opt/flexprice/scripts/restore.sh postgres flexprice_20260218_020000.sql.gz

# Restore ClickHouse
sudo /opt/flexprice/scripts/restore.sh clickhouse
```

### Check Backup Status
```bash
# View backup log
tail -50 /var/log/flexprice-backup.log

# List backups
ls -lah /opt/flexprice/backups/postgres/
ls -lah /opt/flexprice/backups/clickhouse/
```

---

## Troubleshooting

### Service Won't Start
```bash
# Check logs for errors
journalctl -u flexprice-api -n 100 --no-pager

# Common issues:
# 1. Config syntax error - validate YAML
# 2. Port already in use - check with: ss -tlnp | grep 8080
# 3. Database connection failed - check PostgreSQL is running
```

### Consumer Panic (Divide by Zero)
**Cause:** Missing `rate_limit` in config sections
**Fix:** Ensure ALL these sections exist in consumer config with `rate_limit > 0`:
- event_processing
- event_processing_lazy
- event_post_processing
- feature_usage_tracking
- feature_usage_tracking_lazy  ← This one was missing!
- costsheet_usage_tracking
- costsheet_usage_tracking_lazy
- wallet_balance_alert

### Can't Login to Dashboard
```bash
# Check if user exists
sudo -u postgres psql -d flexprice -c "SELECT id, email FROM users WHERE email = 'user@example.com';"

# Check if auth record exists
sudo -u postgres psql -d flexprice -c "SELECT * FROM auths WHERE user_id = 'user_xxx';"

# If auth missing, create one (see "Add New User" section above)
```

### API Returns 502 Bad Gateway
```bash
# Check if API is running
sudo systemctl status flexprice-api

# Check nginx config
sudo nginx -t

# Check nginx error log
tail -50 /var/log/nginx/error.log
```

### ClickHouse Connection Issues
```bash
# Check ClickHouse is running
sudo systemctl status clickhouse-server

# Test connection
clickhouse-client -u flexprice --password flexprice123 -q "SELECT 1"
```

### Redpanda/Kafka Issues
```bash
# Check Redpanda status
sudo systemctl status redpanda

# List topics
rpk topic list

# Check consumer groups
rpk group list
```

### Redpanda OOM Crash Loop
**Symptom:** Redpanda service in `activating` state, rapid restart count (`systemctl show redpanda -p NRestarts`), `connection refused` on port 9092, FlexPrice consumer stops processing events, webhooks not firing.

**Diagnosis:**
```bash
# Check restart count
systemctl show redpanda -p NRestarts

# Check for OOM in logs
journalctl -u redpanda -n 200 | grep -iE 'oom|memory|abort|SIGABRT'

# Check memory config mismatch
grep memory /etc/redpanda/redpanda.yaml     # redpanda config
grep Memory /etc/systemd/system/redpanda.service  # systemd limit
```

**Root Cause:** Systemd `MemoryMax` set lower than redpanda config `memory`. Each shard gets insufficient memory → OOM on partition log replay → crash loop.

**Fix:**
```bash
# 1. Set MemoryMax >= redpanda memory config
sudo sed -i 's/MemoryMax=2G/MemoryMax=3G/' /etc/systemd/system/redpanda.service
sudo systemctl daemon-reload
sudo systemctl restart redpanda

# 2. Wait 15s, verify stable
sleep 15 && systemctl status redpanda

# 3. Set topic retention to prevent unbounded growth
for topic in events events_lazy events_post_processing system_events wallet_alert onboarding_events; do
  rpk topic alter-config $topic --set retention.ms=604800000 --set retention.bytes=1073741824
done

# 4. Restart FlexPrice services to reconnect
sudo systemctl restart flexprice-consumer flexprice-api flexprice-worker
```

**Prevention:** Always ensure systemd `MemoryMax` >= redpanda config `memory` value. Monitor `/var/lib/redpanda/data/` size.

---

## Kafka Topics (Redpanda)

| Topic | Partitions | Retention | Purpose |
|-------|------------|-----------|---------|
| events | 3 | 7 days / 1GB | Main event ingestion |
| events_lazy | 3 | 7 days / 1GB | Lazy processing events |
| events_post_processing | 3 | 7 days / 1GB | Post-processing queue |
| system_events | 3 | 7 days / 1GB | Webhooks & system events |
| wallet_alert | 1 | 7 days / 1GB | Wallet balance alerts |
| onboarding_events | 1 | 7 days / 1GB | Customer onboarding events |

```bash
# Create topic (if missing)
rpk topic create <topic_name> --partitions 3

# Delete topic (careful!)
rpk topic delete <topic_name>

# View topic info
rpk topic describe events
```

---

## SSL Certificates

Managed by Let's Encrypt via Certbot. Auto-renewal is enabled.

```bash
# Check certificate expiry
sudo certbot certificates

# Force renewal (if needed)
sudo certbot renew --force-renewal

# Test auto-renewal
sudo certbot renew --dry-run
```

---

## Monitoring (SigNoz - DEPLOYED)

SigNoz is deployed on bare metal using the **existing FlexPrice ClickHouse** instance.

### Quick Commands
```bash
# Check SigNoz status
sudo systemctl status signoz-otel-collector signoz-query-service

# View OTel Collector logs (traces coming in)
journalctl -u signoz-otel-collector -f

# View Query Service logs
journalctl -u signoz-query-service -f

# Restart SigNoz
sudo systemctl restart signoz-otel-collector signoz-query-service
```

### Access
| Item | Value |
|------|-------|
| URL | https://monitoring.tenbyte.io |
| Internal Port | 8080 (proxied via nginx) |
| First Login | Create account on first visit |

### Architecture
```
FlexPrice Services ──(OTLP gRPC:4317)──► SigNoz OTel Collector ──► ClickHouse (shared)
                                                                         │
                                              SigNoz Query Service ◄─────┘
                                                       │
                                              Nginx (monitoring.tenbyte.io)
```

### Components (All Running)
| Component | Port | Status | Purpose |
|-----------|------|--------|---------|
| signoz-otel-collector | 4317 (gRPC), 4318 (HTTP) | ✅ Running | Receives traces from FlexPrice |
| signoz-query-service | 8080 | ✅ Running | Query API & Web UI |
| zookeeper | 2181 | ✅ Running | Required by ClickHouse cluster |

### ClickHouse Databases (SigNoz)
```bash
# View SigNoz databases
clickhouse-client -q "SHOW DATABASES" | grep signoz

# Databases created:
# - signoz_traces    (trace data)
# - signoz_metrics   (metrics data)
# - signoz_logs      (log data)
# - signoz_metadata  (internal metadata)
# - signoz_analytics (analytics data)
```

### FlexPrice Config (Sending Traces)
```yaml
# In /opt/flexprice-*/config/config.yaml
tenbyte:
  signoz:
    enabled: true
    endpoint: "127.0.0.1:4317"  # OTel Collector gRPC
    service_name: "tenbyte-billing"
    environment: "production"
    sample_rate: 1.0
    insecure: true
```

### Nginx Config
```
/etc/nginx/sites-available/monitoring → proxy to localhost:8080
```

### Systemd Services
```bash
# OTel Collector
/etc/systemd/system/signoz-otel-collector.service

# Query Service (configured via environment variables)
/etc/systemd/system/signoz-query-service.service
# Key env vars:
#   SIGNOZ_WEB_DIRECTORY=/opt/signoz/web
#   SIGNOZ_SQLSTORE_SQLITE_PATH=/var/lib/signoz/signoz.db
#   SIGNOZ_TELEMETRYSTORE_CLICKHOUSE_DSN=tcp://127.0.0.1:9000
#   SIGNOZ_TELEMETRYSTORE_CLICKHOUSE_CLUSTER=cluster
#   SIGNOZ_TOKENIZER_JWT_SECRET=<secret>
```

### Troubleshooting SigNoz
```bash
# No traces showing up?
# 1. Check OTel Collector is receiving data
journalctl -u signoz-otel-collector -n 50 | grep -i "trace"

# 2. Check FlexPrice is sending traces
journalctl -u flexprice-api -n 50 | grep -i "signoz\|otel\|trace"

# 3. Check ClickHouse has data
clickhouse-client -q "SELECT count() FROM signoz_traces.signoz_index_v2"

# Query Service won't start?
# Check JWT secret is set
cat /etc/systemd/system/signoz-query-service.service | grep JWT

# Check ClickHouse cluster config exists
cat /etc/clickhouse-server/config.d/cluster.xml
```

---

## Emergency Contacts

| Role | Contact |
|------|---------|
| DevOps | [Add contact] |
| Backend Lead | [Add contact] |
| CTO | [Add contact] |

---

## Useful Commands Cheatsheet

```bash
# === Services ===
sudo systemctl status flexprice-api        # Check status
sudo systemctl restart flexprice-api       # Restart
sudo systemctl stop flexprice-api          # Stop
sudo systemctl start flexprice-api         # Start
journalctl -u flexprice-api -f             # Live logs

# === Database ===
sudo -u postgres psql -d flexprice         # PostgreSQL shell
clickhouse-client -u flexprice --password flexprice123  # ClickHouse shell

# === Redpanda ===
rpk topic list                             # List topics
rpk group list                             # List consumer groups
rpk topic consume events -n 5              # Consume 5 messages

# === Nginx ===
sudo nginx -t                              # Test config
sudo systemctl reload nginx                # Reload config

# === Disk Space ===
df -h                                      # Check disk usage
du -sh /opt/flexprice/backups/*            # Backup sizes

# === Memory ===
free -h                                    # Memory usage
htop                                       # Process monitor
```

---

## Change Log

| Date | Change | By |
|------|--------|-----|
| 2026-02-18 | Initial bare metal deployment | Saad Rupai |
| 2026-02-18 | Migrated from Docker to systemd | Saad Rupai |
| 2026-02-18 | Replaced Kafka with Redpanda | Saad Rupai |
| 2026-02-18 | Set up automated backups (cron) | Saad Rupai |
| 2026-02-18 | Fixed consumer panic issue | Saad Rupai |
| 2026-02-18 | Created user auth records | Saad Rupai |
| 2026-02-18 | Deployed SigNoz monitoring (bare metal) | Saad Rupai |
| 2026-02-18 | Changed FlexPrice API port 8080→8000 (SigNoz uses 8080) | Saad Rupai |
| 2026-02-18 | Added ZooKeeper for ClickHouse cluster | Saad Rupai |
| 2026-03-02 | Fixed Redpanda OOM crash loop: MemoryMax 2G→3G to match redpanda config | Claude |
| 2026-03-02 | Set topic retention policies: 7 days + 1GB max per topic (was unbounded, 9.9GB data) | Claude |

---

**This document should be updated whenever infrastructure changes are made.**
