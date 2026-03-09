# Tenbyte Billing Deployment Checklist

> Updated: March 2026. Bare metal deployment (systemd, not Docker).

## Prerequisites
- [ ] Server with Go 1.23+ installed
- [ ] PostgreSQL 17, ClickHouse 26.x, Redpanda, Temporal running (see `deploy/DEPLOYMENT.md`)
- [ ] Domain/SSL configured (Certbot + Nginx)
- [ ] SSH access: `ssh FlexPrice`

## FlexPrice Setup

### 1. Deploy FlexPrice
```bash
ssh FlexPrice
cd /opt/flexprice-v1
git pull origin feat/usage-metering

# Build binary
go build -ldflags="-w -s" -o /opt/flexprice/bin/flexprice-app cmd/server/main.go

# Restart all services
sudo systemctl restart flexprice-api flexprice-consumer flexprice-worker

# Verify
curl https://api-billing.tenbyte.io/health
```

### 2. Run Migrations
```bash
cd /opt/flexprice-api
/opt/flexprice/bin/flexprice-app migrate
```

### 3. Create Tenbyte Tenant & API Key
```bash
cd /opt/flexprice-v1
go run scripts/main.go onboard-tenant
```
This creates:
- Tenbyte tenant + environments (production + sandbox)
- API key for authentication

### 4. Configure Service Configs
Each service has its own config. See `deploy/config/` for examples:
```bash
sudo nano /opt/flexprice-api/config/config.yaml       # mode: api, port: 8000
sudo nano /opt/flexprice-consumer/config/config.yaml   # mode: consumer
sudo nano /opt/flexprice-worker/config/config.yaml     # mode: temporal_worker
```

Key config items:
- Database credentials (postgres, clickhouse)
- Kafka brokers (Redpanda on 127.0.0.1:9092)
- Temporal address
- Auth secret + encryption key
- Payment gateway credentials (Stripe/SSLCommerz)
- Webhook tenant config (per-environment routing)

### 5. Configure Webhook Per-Environment Routing
In `/opt/flexprice-api/config/config.yaml`:
```yaml
webhook:
  enabled: true
  topic: "system_events"
  pubsub: "kafka"
  consumer_group: "webhook-consumer"
  tenants:
    # Use composite key: tenant_id/env_id (lowercased)
    "tenant_id/env_id_production":
      enabled: true
      endpoint: "https://api.tenbyte.io/v1/webhooks/flexprice/alert"
      headers:
        "X-Api-Key": "<tenbyte-api-key>"
    "tenant_id/env_id_staging":
      enabled: true
      endpoint: "https://api-staging.tenbyte.io/v1/webhooks/flexprice/alert"
      headers:
        "X-Api-Key": "<tenbyte-api-key>"
```

### 6. Create FlexPrice Customers for Existing Organizations
```bash
# From api.tenbyte.com repo
go run cmd/main.go org:migrate-to-flexprice --dry-run
go run cmd/main.go org:migrate-to-flexprice
```

### 7. Create Plans in FlexPrice
Create plans with features, entitlements, and tiered pricing via API or dashboard.

### 8. Create Meters for Usage Tracking
Required meters for billing event ingestion:

| Meter | Event Name | Aggregation | Unit |
|-------|-----------|-------------|------|
| CDN Traffic | `cdn.traffic.usage` | SUM | GB |
| CDN Requests | `cdn.requests.usage` | SUM | count |
| VidInfra Traffic | `vidinfra.traffic.usage` | SUM | GB |
| Storage | `storage.usage` | SUM | GB |

## api.tenbyte.com Setup

### 1. Deploy api.tenbyte.com
```bash
ssh FlexPrice
cd /opt/api.tenbyte.com
git pull origin feat/plan-lookup-prefix-filter
go build -o /opt/api-tenbyte/bin/api-tenbyte cmd/main.go
sudo systemctl restart api-tenbyte
```

### 2. Configure Environment
- FlexPrice API credentials in config
- Database connection
- Payment gateway webhooks (Stripe, SSLCommerz)

## Billing Event Ingestion Setup

### tenbyte-cdn-api
```bash
# Deploy with billing changes
cd /path/to/tenbyte-cdn-api
git checkout feat/billing-requests-and-vidinfra-split
# Configure billing section in config.yaml with FlexPrice API key + host
# Deploy worker — billing push job runs hourly at :05
```

### vidinfra-api
```bash
# Deploy with storage billing
cd /path/to/vidinfra-api
git checkout feat/billing-storage-push
# Run migration for billing tables
go run cmd/main.go migrate
# Configure billing section in config.yaml with FlexPrice API key + host
# Deploy worker — storage push job runs hourly at :05
```

## Stripe Setup

### 1. Configure Stripe Keys
Add to FlexPrice API config under the appropriate environment's secrets.

### 2. Configure Stripe Webhook Endpoint
```
URL: https://api-billing.tenbyte.io/v1/webhooks/stripe/{tenant_id}/{env_id}
Events: checkout.session.completed, payment_intent.succeeded, customer.created,
        setup_intent.succeeded, customer.subscription.created/updated/deleted
```

### 3. If Switching Stripe Keys
Run the reset script to clear stale customer mappings:
```bash
./scripts/reset_stripe_customers.sh \
  --api-url https://api-billing.tenbyte.io \
  --api-key <api-key> \
  --env-id <env_id> \
  --dry-run
```

## Verification Steps

### 1. Test Subscription Flow
- [ ] User can view plans
- [ ] User can subscribe to a plan
- [ ] Wallet is created/topped up
- [ ] Subscription invoice is generated
- [ ] Invoice is auto-paid from wallet

### 2. Test Usage Metering
- [ ] CDN traffic events are ingested (`cdn.traffic.usage`)
- [ ] CDN request events are ingested (`cdn.requests.usage`)
- [ ] VidInfra traffic events are ingested (`vidinfra.traffic.usage`)
- [ ] Storage events are ingested (`storage.usage`)
- [ ] Usage meters update correctly
- [ ] Overage invoices are generated when limits exceeded

### 3. Test Webhook Delivery
- [ ] `customer.updated` webhook delivered to Tenbyte on payment
- [ ] `wallet.balance.low` webhook triggers auto top-up
- [ ] Per-environment routing: staging events go to staging endpoint

### 4. Test Plan Upgrade
- [ ] Upgrade cost calculated correctly
- [ ] Old subscription cancelled
- [ ] New subscription created
- [ ] Old invoice voided (if applicable)

### 5. Test Billing Overview
- [ ] Wallet balance shows correctly
- [ ] Spent this cycle excludes top-ups and voided invoices
- [ ] Payment history displays correctly
- [ ] Invoice PDF download works (not for DRAFT invoices)

## Key Fixes Applied
1. **Multi-subscription entitlement tracking** - SubscriptionID set on each entitlement
2. **Spent calculation** - Excludes top-ups and voided invoices
3. **VoidInvoice** - Allows voiding SUCCEEDED payment status invoices
4. **Cycle start buffer** - 10-minute buffer to include first payment
5. **Stripe customer duplicate fix** - Entity integration mapping check prevents race condition
6. **Per-environment webhook routing** - Composite `tenant_id/env_id` keys in config
7. **CDN/VidInfra billing split** - Distribution type determines event name prefix
