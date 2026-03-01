# Tenbyte Billing System - Technical Documentation

> Integration of FlexPrice billing engine with api.tenbyte.com

## Architecture Overview

```
┌─────────────────────┐     ┌─────────────────────┐     ┌─────────────────────┐
│   Tenbyte Dashboard │     │    api.tenbyte.com  │     │      FlexPrice      │
│   (billing.tenbyte) │────▶│   (Billing Proxy)   │────▶│  (Billing Engine)   │
└─────────────────────┘     └─────────────────────┘     └─────────────────────┘
                                      │                           │
                                      │                           │
                            ┌─────────▼─────────┐       ┌─────────▼─────────┐
                            │   Tenbyte Users   │       │  Kafka (Redpanda) │
                            │   Organizations   │       │    ClickHouse     │
                            └───────────────────┘       │    PostgreSQL     │
                                                        └───────────────────┘
```

### Components

| Component | URL | Purpose |
|-----------|-----|---------|
| FlexPrice API | `api-billing.tenbyte.io` | Core billing engine |
| Tenbyte API | `api.tenbyte.io` | User/org management, billing proxy |
| Dashboard | `billing.tenbyte.io` | Customer-facing billing UI |

### Environment IDs

| Environment | ID |
|-------------|-----|
| Sandbox | `env_01KGHBCS9S28GC7WQAS2T4HD04` |
| Production | `env_01KGHBCS9S28GC7WQAS2T4HD05` |

---

## 1. Features (Meters)

Features define what usage is tracked and how it's aggregated.

### Create Feature

```bash
curl -X POST https://api-billing.tenbyte.io/v1/features \
  -H "x-api-key: sk_01KGHBDWHMA896YDVMKAEN2VV6" \
  -H "X-Environment-ID: env_01KGHBCS9S28GC7WQAS2T4HD05" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Bandwidth",
    "lookup_key": "bandwidth",
    "type": "metered",
    "meter_type": "counter",
    "aggregation_type": "sum",
    "unit_singular": "MB",
    "unit_plural": "MB"
  }'
```

### Feature Types

| Type | Use Case |
|------|----------|
| `metered` | Usage-based (bandwidth, storage, API calls) |
| `binary` | On/off features (premium support) |

### Aggregation Types

| Type | Description |
|------|-------------|
| `sum` | Sum all values in period |
| `count` | Count number of events |
| `max` | Maximum value in period |
| `unique_count` | Count unique values |

---

## 2. Plans & Pricing

### Plan Structure

```
Plan
├── Base Price (fixed monthly/yearly)
├── Entitlements (included usage)
└── Prices (usage-based charges)
    └── Price Tiers (tiered pricing)
```

### Create Plan

```bash
curl -X POST https://api-billing.tenbyte.io/v1/plans \
  -H "x-api-key: sk_01KGHBDWHMA896YDVMKAEN2VV6" \
  -H "X-Environment-ID: env_01KGHBCS9S28GC7WQAS2T4HD05" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Standard Plan",
    "lookup_key": "standard",
    "description": "Standard plan with included bandwidth",
    "invoice_cadence": "ARREAR"
  }'
```

### Create Price with Tiered Pricing

```bash
curl -X POST https://api-billing.tenbyte.io/v1/prices \
  -H "x-api-key: sk_01KGHBDWHMA896YDVMKAEN2VV6" \
  -H "X-Environment-ID: env_01KGHBCS9S28GC7WQAS2T4HD05" \
  -H "Content-Type: application/json" \
  -d '{
    "plan_id": "plan_01KJ...",
    "feature_id": "feat_01KJ...",
    "type": "USAGE",
    "billing_model": "TIERED",
    "billing_cadence": "MONTHLY",
    "tier_mode": "SLAB",
    "currency": "USD",
    "tiers": [
      {"up_to": 1048576, "unit_amount": 0, "flat_amount": 0},
      {"up_to": 2097152, "unit_amount": 0.0008879, "flat_amount": 0},
      {"up_to": 4194304, "unit_amount": 0.0006684, "flat_amount": 0},
      {"up_to": null, "unit_amount": 0.0004488, "flat_amount": 0}
    ]
  }'
```

### Tier Modes

| Mode | Description |
|------|-------------|
| `SLAB` | Each tier applies only to usage within that range |
| `VOLUME` | Entire usage charged at the tier rate reached |

### Entitlements (Soft Limits)

Entitlements define included usage before overage charges apply.

```bash
curl -X POST https://api-billing.tenbyte.io/v1/entitlements \
  -H "x-api-key: sk_01KGHBDWHMA896YDVMKAEN2VV6" \
  -H "X-Environment-ID: env_01KGHBCS9S28GC7WQAS2T4HD05" \
  -H "Content-Type: application/json" \
  -d '{
    "plan_id": "plan_01KJ...",
    "feature_id": "feat_01KJ...",
    "feature_type": "metered",
    "is_soft_limit": true,
    "usage_limit": 1048576,
    "usage_reset_period": "MONTHLY"
  }'
```

---

## 3. Customer Creation Flow

When a user creates an organization in Tenbyte, a corresponding customer is created in FlexPrice.

### Migration Command

```bash
# Migrate single organization
go run cmd/main.go org:migrate-to-flexprice --org-id <uuid>

# Migrate all organizations
go run cmd/main.go org:migrate-to-flexprice

# Dry run
go run cmd/main.go org:migrate-to-flexprice --dry-run
```

### Flow Diagram

```
User Creates Org (api.tenbyte.com)
         │
         ▼
┌────────────────────────┐
│ org:migrate-to-flexprice│
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Create FlexPrice      │
│  Customer              │
│  - Name: org.Name      │
│  - Email: owner.Email  │
│  - External ID: org.ID │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Create Wallet (USD)   │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Update Organization   │
│  - billing_customer_id │
│  - billing_provider    │
└────────────────────────┘
```

### API Response

```json
{
  "id": "cust_01KJ...",
  "name": "Acme Corp",
  "email": "admin@acme.com",
  "external_id": "org-uuid-here",
  "metadata": {
    "organization_id": "org-uuid-here",
    "migration_date": "2026-02-23T..."
  }
}
```

---

## 4. Subscription Management

### Create Subscription

```bash
# Via api.tenbyte.com
curl -X POST https://api.tenbyte.io/v1/organization/billing/subscriptions \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "plan_id": "plan_01KJ...",
    "billing_cadence": "MONTHLY",
    "billing_period": "month",
    "billing_period_count": 1,
    "currency": "USD"
  }'
```

### Subscription States

```
PENDING → ACTIVE → CANCELLED
    │         │
    │         └──→ PAUSED → ACTIVE
    │
    └──→ CANCELLED
```

### Pause Subscription

```bash
curl -X POST https://api.tenbyte.io/v1/organization/billing/subscriptions/{id}/pause \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "pause_mode": "IMMEDIATE",
    "reason": "Customer requested pause"
  }'
```

### Resume Subscription

```bash
curl -X POST https://api.tenbyte.io/v1/organization/billing/subscriptions/{id}/resume \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "resume_mode": "IMMEDIATE"
  }'
```

### Cancel Subscription

```bash
curl -X POST https://api.tenbyte.io/v1/organization/billing/subscriptions/{id}/cancel \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "cancellation_reason_id": 1,
    "cancellation_reason": "Customer requested"
  }'
```

---

## 5. Usage Metering

### Ingest Events

```bash
curl -X POST https://api-billing.tenbyte.io/v1/events \
  -H "x-api-key: sk_01KGHBDWHMA896YDVMKAEN2VV6" \
  -H "X-Environment-ID: env_01KGHBCS9S28GC7WQAS2T4HD05" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "event_name": "bandwidth",
        "external_customer_id": "org-uuid-here",
        "timestamp": "2026-02-23T10:00:00Z",
        "properties": {
          "value": 150000
        }
      }
    ]
  }'
```

### Event Processing Flow

```
Event Ingested
     │
     ▼
┌─────────────┐
│   Kafka     │ (events topic)
└─────────────┘
     │
     ▼
┌─────────────────────────┐
│  Event Consumer         │
│  - Validate event       │
│  - Store in ClickHouse  │
└─────────────────────────┘
     │
     ▼
┌─────────────────────────┐
│  Post-Processing        │
│  - Calculate cost       │
│  - Check entitlements   │
│  - Trigger overage      │
└─────────────────────────┘
```

### Get Usage Summary

```bash
curl -X GET "https://api-billing.tenbyte.io/v1/subscriptions/{sub_id}/usage" \
  -H "x-api-key: sk_01KGHBDWHMA896YDVMKAEN2VV6" \
  -H "X-Environment-ID: env_01KGHBCS9S28GC7WQAS2T4HD05"
```

---

## 6. Overage Billing (Soft Limit)

When usage exceeds the entitlement soft limit, overage charges are calculated and debited from the wallet.

### Overage Flow

```
Usage Exceeds Soft Limit
         │
         ▼
┌─────────────────────────┐
│  Calculate Overage      │
│  - Apply tiered pricing │
│  - Determine cost       │
└─────────────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Create Invoice         │
│  - Status: PAID         │
│  - Type: overage        │
└─────────────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Debit Wallet           │
│  - Reduce balance       │
│  - Create transaction   │
└─────────────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Send Webhook           │
│  - wallet.balance.low   │
│  - invoice.paid         │
└─────────────────────────┘
```

### Tiered Pricing Example

For bandwidth with tiers:
- 0 - 1,048,576 MB: Free
- 1,048,577 - 2,097,152 MB: $0.0008879/MB
- 2,097,153 - 4,194,304 MB: $0.0006684/MB
- 4,194,305+ MB: $0.0004488/MB

**Example Calculation:**
- Usage: 3,000,000 MB
- Free tier: 1,048,576 MB @ $0 = $0
- Tier 2: 1,048,576 MB @ $0.0008879 = $931.04
- Tier 3: 902,848 MB @ $0.0006684 = $603.42
- **Total: $1,534.46**

---

## 7. Wallet System

### Wallet Structure

```json
{
  "id": "wallet_01KJ...",
  "customer_id": "cust_01KJ...",
  "currency": "USD",
  "balance": 500.00,
  "auto_topup_trigger": "balance_threshold",
  "auto_topup_min_balance": 100.00,
  "auto_topup_amount": 500.00
}
```

### Get Wallet Balance

```bash
curl -X GET https://api.tenbyte.io/v1/organization/billing/wallets \
  -H "Authorization: Bearer <token>"
```

### Manual Top-Up

```bash
curl -X POST https://api.tenbyte.io/v1/organization/billing/wallets/{id}/topup \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "amount": 100.00,
    "currency": "USD",
    "payment_gateway": "stripe"
  }'
```

### Auto Top-Up Configuration

```bash
curl -X PUT https://api.tenbyte.io/v1/organization/billing/wallets/{id} \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "auto_topup_trigger": "enable",
    "auto_topup_min_balance": 50.00,
    "auto_topup_amount": 200.00
  }'
```

### Auto Top-Up Flow

```
Wallet Balance Drops Below Threshold
              │
              ▼
┌───────────────────────────┐
│  Check auto_topup_trigger │
│  = "balance_threshold"    │
└───────────────────────────┘
              │
              ▼
┌───────────────────────────┐
│  Charge Payment Method    │
│  (Stripe/SSLCommerz)      │
└───────────────────────────┘
              │
              ▼
┌───────────────────────────┐
│  Credit Wallet            │
│  + auto_topup_amount      │
└───────────────────────────┘
              │
              ▼
┌───────────────────────────┐
│  Send Webhook             │
│  wallet.topped_up         │
└───────────────────────────┘
```

---

## 8. Payment Integrations

### Supported Gateways

| Gateway | Regions | Features |
|---------|---------|----------|
| Stripe | Global | Cards, ACH, SEPA |
| SSLCommerz | Bangladesh | Cards, Mobile Banking, Net Banking |

### Stripe Payment Flow

```
Customer Initiates Payment
         │
         ▼
┌────────────────────────┐
│  Create Payment Intent │
│  (Stripe)              │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Customer Completes    │
│  Payment (Stripe UI)   │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Stripe Webhook        │
│  payment_intent.       │
│  succeeded             │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Update Payment Status │
│  Credit Wallet         │
└────────────────────────┘
```

### Stripe Webhook URL

```
POST https://api-billing.tenbyte.io/v1/webhooks/stripe/{tenant_id}/{environment_id}
```

### SSLCommerz Payment Flow

```
Customer Initiates Payment
         │
         ▼
┌────────────────────────┐
│  Create Session        │
│  (SSLCommerz)          │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Redirect to           │
│  SSLCommerz Gateway    │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Customer Completes    │
│  Payment               │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  IPN Callback          │
│  (Server-to-Server)    │
└────────────────────────┘
         │
         ▼
┌────────────────────────┐
│  Validate & Process    │
│  Credit Wallet         │
└────────────────────────┘
```

### SSLCommerz Webhook URL

```
POST https://api-billing.tenbyte.io/v1/webhooks/sslcommerz/{tenant_id}/{environment_id}
```

### SSLCommerz Configuration

```yaml
sslcommerz:
  base_url: "https://securepay.sslcommerz.com"
  session_api: "https://securepay.sslcommerz.com/gwprocess/v4/api.php"
  validation_api: "https://securepay.sslcommerz.com/validator/api/validationserverAPI.php"
  ipn_url: "https://api-billing.tenbyte.io/v1/webhooks/sslcommerz/{tenant_id}/{env_id}"
  success_url: "https://billing.tenbyte.io/billing/payment/success"
  fail_url: "https://billing.tenbyte.io/billing/payment/fail"
  cancel_url: "https://billing.tenbyte.io/billing/payment/cancel"
```

---

## 9. Webhooks

### Outgoing Webhooks (FlexPrice → Tenbyte)

Configure in FlexPrice config:

```yaml
webhook:
  enabled: true
  topic: "system_events"
  tenants:
    "tenant_01KGHBCRK45K5C1KJATZFQ332Y":
      enabled: true
      endpoint: "https://api.tenbyte.io/v1/webhooks/flexprice/alert"
      headers:
        "X-Api-Key": "<api-key>"
```

### Webhook Events

| Event | Description |
|-------|-------------|
| `invoice.created` | New invoice generated |
| `invoice.paid` | Invoice payment completed |
| `wallet.balance.low` | Wallet balance below threshold |
| `wallet.topped_up` | Wallet successfully topped up |
| `subscription.created` | New subscription started |
| `subscription.cancelled` | Subscription cancelled |
| `payment.succeeded` | Payment processed successfully |
| `payment.failed` | Payment processing failed |

### Incoming Webhooks (Payment Providers → FlexPrice)

| Provider | Endpoint |
|----------|----------|
| Stripe | `/v1/webhooks/stripe/{tenant_id}/{env_id}` |
| SSLCommerz | `/v1/webhooks/sslcommerz/{tenant_id}/{env_id}` |

---

## 10. Invoice Management

### Invoice Types

| Type | Description |
|------|-------------|
| `subscription` | Regular billing cycle invoice |
| `overage` | Usage overage charges |
| `one_time` | One-time charges |

### Invoice States

```
DRAFT → PENDING → PAID
           │
           └──→ VOID
```

### Get Invoice

```bash
curl -X GET https://api.tenbyte.io/v1/organization/billing/invoices/{id} \
  -H "Authorization: Bearer <token>"
```

### Download Invoice PDF

```bash
curl -X GET https://api.tenbyte.io/v1/organization/billing/invoices/{id}/pdf \
  -H "Authorization: Bearer <token>" \
  -o invoice.pdf
```

### List Invoices

```bash
curl -X GET https://api.tenbyte.io/v1/organization/billing/invoices \
  -H "Authorization: Bearer <token>"
```

---

## 11. API Reference

### Authentication

**FlexPrice API (api-billing.tenbyte.io):**
```
x-api-key: sk_01KGHBDWHMA896YDVMKAEN2VV6
X-Environment-ID: env_01KGHBCS9S28GC7WQAS2T4HD05
```

**Tenbyte API (api.tenbyte.io):**
```
Authorization: Bearer <jwt_token>
```

### Common Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/organization/billing/overview` | Billing summary |
| GET | `/v1/organization/billing/wallets` | List wallets |
| POST | `/v1/organization/billing/wallets/{id}/topup` | Top up wallet |
| GET | `/v1/organization/billing/subscriptions` | List subscriptions |
| POST | `/v1/organization/billing/subscriptions` | Create subscription |
| GET | `/v1/organization/billing/invoices` | List invoices |
| GET | `/v1/organization/billing/invoices/{id}/pdf` | Download PDF |
| GET | `/v1/organization/billing/usage` | Usage summary |

---

## 12. Deployment

### Services

| Service | Port | Config |
|---------|------|--------|
| flexprice-api | 8000 | `/opt/flexprice-api/config/config.yaml` |
| flexprice-consumer | 8001 | `/opt/flexprice-consumer/config/config.yaml` |
| flexprice-worker | 8002 | `/opt/flexprice-worker/config/config.yaml` |

### Systemd Commands

```bash
# Check status
sudo systemctl status flexprice-api flexprice-consumer flexprice-worker

# Restart services
sudo systemctl restart flexprice-api flexprice-consumer flexprice-worker

# View logs
journalctl -u flexprice-api -f
```

### Redeploy Script

```bash
cd /opt/flexprice-v1/deploy
./redeploy.sh all   # Redeploy all services
./redeploy.sh api   # Redeploy only API
```

---

## 13. Monitoring

### SigNoz Integration

Traces and metrics are sent to SigNoz for observability.

```yaml
tenbyte:
  signoz:
    enabled: true
    endpoint: "127.0.0.1:4317"
    service_name: "tenbyte-billing"
    environment: "production"
```

### Key Metrics

| Metric | Description |
|--------|-------------|
| Event ingestion rate | Events processed per second |
| Payment success rate | Successful payments / total |
| Wallet balance alerts | Low balance notifications |
| Invoice generation time | Time to generate invoices |

### Alerts

- Event ingestion failures
- Kafka consumer errors
- Payment webhook failures
- Low wallet balance

---

## 14. Troubleshooting

### Common Issues

**Events not processing:**
1. Check Redpanda is running: `systemctl status redpanda`
2. Check consumer logs: `journalctl -u flexprice-consumer -f`
3. Verify Kafka topics exist

**Webhooks not triggering:**
1. Verify webhook URL has correct environment ID
2. Check nginx access logs for incoming requests
3. Verify payment provider webhook configuration

**Invoice not generated:**
1. Check subscription status is ACTIVE
2. Verify billing period has ended
3. Check worker logs for errors

### Health Checks

```bash
# FlexPrice API health
curl https://api-billing.tenbyte.io/health

# Check all services
curl -s https://api-billing.tenbyte.io/v1/health | jq
```

---

## Appendix: Test Data

### Test Customer
- External ID: `e0c197b8-bc09-407e-af83-4f1063d960cc`
- Customer ID: `cust_01KHFTH8QM0XJWMYFVKGCB4407`
- Wallet ID: `wallet_01KHFTH8R4V10EHM9VKAN0Q95G`

### Test Events

```bash
# Ingest test bandwidth event
curl -X POST https://api-billing.tenbyte.io/v1/events \
  -H "x-api-key: sk_01KGHBDWHMA896YDVMKAEN2VV6" \
  -H "X-Environment-ID: env_01KGHBCS9S28GC7WQAS2T4HD05" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [{
      "event_name": "bandwidth",
      "external_customer_id": "e0c197b8-bc09-407e-af83-4f1063d960cc",
      "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
      "properties": {"value": 100000}
    }]
  }'
```

---

*Document Version: 1.0*
*Last Updated: February 2026*
