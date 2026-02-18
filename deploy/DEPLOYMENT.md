# FlexPrice Production Deployment Summary

**Date:** February 18, 2026
**Server:** 163.61.156.37 (FlexPrice)
**Deployed By:** Saad Rupai

---

## Overview

Successfully migrated FlexPrice billing system from Docker-based deployment to bare metal production deployment. This eliminates Docker overhead, reduces memory usage, and provides better control over individual services.

---

## Architecture Changes

### Before (Docker-based)
- All services running in Docker containers
- Kafka for message queue
- High memory usage (~2.4GB for Docker alone)
- Single docker-compose managing everything

### After (Bare Metal)
- Native systemd services
- Redpanda replaces Kafka (lightweight, no JVM)
- Direct process management via systemd
- Separate configs per service for isolation

---

## Services Deployed

### Infrastructure Services

| Service | Port | Status | Notes |
|---------|------|--------|-------|
| PostgreSQL 17 | 5432 | Running | Main database |
| ClickHouse | 9000, 8123 | Running | Analytics/events database |
| Redpanda | 9092 | Running | Kafka-compatible message broker (replaces Kafka) |
| Temporal | 7233 | Running | Workflow orchestration |

### Application Services

| Service | Port | Config Location | Status |
|---------|------|-----------------|--------|
| flexprice-api | 8080 | /opt/flexprice-api/config/config.yaml | Running |
| flexprice-consumer | 8081 | /opt/flexprice-consumer/config/config.yaml | Running |
| flexprice-worker | 8082 | /opt/flexprice-worker/config/config.yaml | Running |

### Web Services

| Domain | Service | SSL |
|--------|---------|-----|
| api-billing.tenbyte.io | FlexPrice API | Let's Encrypt |
| billing.tenbyte.io | FlexPrice Dashboard | Let's Encrypt |
| monitoring.tenbyte.io | SigNoz (pending) | Let's Encrypt |

---

## Key Configuration Files

### Systemd Services
```
/etc/systemd/system/flexprice-api.service
/etc/systemd/system/flexprice-consumer.service
/etc/systemd/system/flexprice-worker.service
/etc/systemd/system/temporal.service
/etc/systemd/system/redpanda.service
```

### Application Configs
```
/opt/flexprice-api/config/config.yaml      # API server config
/opt/flexprice-consumer/config/config.yaml # Consumer config
/opt/flexprice-worker/config/config.yaml   # Temporal worker config
```

### Nginx Configs
```
/etc/nginx/sites-available/api-billing      # API reverse proxy
/etc/nginx/sites-available/billing-dashboard # Frontend static files
```

### Frontend
```
/opt/flex-front/          # Frontend source
/opt/flex-front/dist/     # Built static files
/opt/flex-front/.env      # Frontend environment config
```

---

## Deployment Steps Performed

### 1. Infrastructure Setup
- Installed PostgreSQL 17 from official repository
- Installed ClickHouse from official repository
- Installed Redpanda (Kafka replacement) - no JVM required
- Installed Temporal server with PostgreSQL backend
- Installed Node.js 20 for frontend build

### 2. Database Migration
- Backed up Docker PostgreSQL data
- Restored to bare metal PostgreSQL
- Migrated ClickHouse events data (161 events, 87 events_processed)
- Created auth records for existing users

### 3. Application Deployment
- Built FlexPrice Go binary: `/opt/flexprice/bin/flexprice-app`
- Created separate working directories for each service mode
- Configured deployment modes: `api`, `consumer`, `temporal_worker`
- Set up systemd services with proper resource limits

### 4. Kafka Topics Created (via Redpanda)
```bash
rpk topic create events --partitions 3
rpk topic create events_lazy --partitions 3
rpk topic create events_post_processing --partitions 3
rpk topic create system_events --partitions 3
rpk topic create wallet_alert --partitions 1
```

### 5. Frontend Deployment
- Updated `.env` with new API endpoint: `https://api-billing.tenbyte.io/v1`
- Rebuilt frontend with Vite
- Configured Nginx to serve static files from `/opt/flex-front/dist/`

### 6. SSL Certificates
- Configured Let's Encrypt certificates via Certbot
- Auto-renewal enabled for all domains

### 7. CORS Configuration
- Added allowed origins:
  - https://billing.tenbyte.io
  - https://api-billing.tenbyte.io
  - https://api-staging.tenbyte.io
  - http://localhost:5173

---

## Issues Resolved

### 1. Consumer Service Panic (Divide by Zero)
**Problem:** Consumer service crashed with `integer divide by zero` in throttle middleware
**Cause:** Missing `feature_usage_tracking_lazy` and other config sections
**Solution:** Added all required config sections with proper `rate_limit` values

### 2. User Authentication Failure
**Problem:** Existing users couldn't log in
**Cause:** `auths` table was empty after migration
**Solution:** Created auth records with bcrypt password hashes

### 3. Nginx Routing Issues
**Problem:** Domains pointing to default static server
**Cause:** SSL certificates created but proxy rules missing
**Solution:** Created proper nginx configs for API and dashboard

### 4. Frontend Build Failure
**Problem:** npm dependencies built for macOS, not Linux
**Solution:** Removed node_modules, reinstalled on Linux server

---

## Verification Commands

```bash
# Check all services
sudo systemctl status flexprice-api flexprice-consumer flexprice-worker temporal redpanda clickhouse-server postgresql

# Check API health
curl https://api-billing.tenbyte.io/health

# Check logs
journalctl -u flexprice-api -f
journalctl -u flexprice-consumer -f
journalctl -u flexprice-worker -f

# Check Redpanda topics
rpk topic list

# Check database
sudo -u postgres psql -d flexprice -c "SELECT COUNT(*) FROM users;"
```

---

## Memory Usage Comparison

| Component | Docker | Bare Metal |
|-----------|--------|------------|
| Docker daemon | ~2.4GB | 0 |
| Kafka (JVM) | ~1GB | 0 |
| Redpanda | N/A | ~200MB |
| FlexPrice services | ~500MB | ~300MB |
| **Total** | **~4GB** | **~1.5GB** |

---

## Pending Items

1. **SigNoz Monitoring** - Docker compose available at `/opt/flexprice-v1/deploy/signoz/docker-compose.yml` (requires Docker if needed)
2. **Automated Backups** - Scripts available at `/opt/flexprice-v1/deploy/backup/`
3. **Password Reset** - Current test password is `password123`, should be changed

---

## Quick Reference

### Start/Stop Services
```bash
sudo systemctl start flexprice-api flexprice-consumer flexprice-worker
sudo systemctl stop flexprice-api flexprice-consumer flexprice-worker
sudo systemctl restart flexprice-api
```

### View Logs
```bash
journalctl -u flexprice-api -f --no-pager
journalctl -u flexprice-consumer -f --no-pager
```

### Rebuild Frontend
```bash
cd /opt/flex-front
npm run build
sudo systemctl reload nginx
```

### Redeploy Backend
```bash
cd /opt/flexprice-v1
git pull
go build -ldflags="-w -s" -o /opt/flexprice/bin/flexprice-app cmd/server/main.go
sudo systemctl restart flexprice-api flexprice-consumer flexprice-worker
```

---

## Repository Files Added

New deployment scripts committed to `feat/usage-metering` branch:
- `deploy/install.sh` - Full installation script
- `deploy/redeploy.sh` - Quick redeploy script
- `deploy/status.sh` - Service status checker
- `deploy/backup/backup.sh` - Database backup script
- `deploy/backup/restore.sh` - Database restore script
- `deploy/systemd/*.service` - All systemd service files
- `deploy/nginx/*.conf` - Nginx configuration templates
- `deploy/signoz/docker-compose.yml` - SigNoz monitoring stack

---

**Deployment completed successfully. All services operational.**
