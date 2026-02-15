# Tenbyte Billing Deployment Checklist

## Prerequisites
- [ ] Server with Docker installed
- [ ] PostgreSQL, ClickHouse, Kafka, Temporal running
- [ ] Domain/SSL configured for API endpoints

## FlexPrice Setup

### 1. Deploy FlexPrice
```bash
cd /opt/flexprice-v1
git pull origin feat/usage-metering
docker build --no-cache -f Dockerfile.local -t flexprice-app:local .
docker compose up -d flexprice-api flexprice-consumer flexprice-worker
```

### 2. Run Migrations
```bash
docker exec flexprice-v1-flexprice-api-1 ./app migrate
```

### 3. Create Tenbyte Tenant & API Key
```bash
# Run the tenant creation script
go run scripts/create_tenbyte_tenant.go
```
This creates:
- Tenbyte tenant
- API key for authentication

### 4. Configure Credentials
Update `.env` or config with:
- `FLEXPRICE_API_URL`
- `FLEXPRICE_API_KEY`
- Payment gateway credentials (Stripe/SSLCommerz)

### 5. Create FlexPrice Customers for Existing Organizations
```bash
# Use the migrate-to-flexprice command
go run cmd/organizations/main.go org:migrate-to-flexprice
# Or with dry-run first:
go run cmd/organizations/main.go org:migrate-to-flexprice --dry-run
```

### 6. Create Plans in FlexPrice
- Create Starter, Pro, CDN plans with:
  - Features (metered/boolean)
  - Entitlements
  - Pricing tiers

### 7. Create Meters for Usage Tracking
- `storage_usage` (SUM on storage_gb)
- `video_transcode` (SUM on minutes)
- `api_request` (COUNT)
- `bandwidth_usage` (SUM on bandwidth_gb)

## api.tenbyte.com Setup

### 1. Deploy api.tenbyte.com
```bash
cd /opt/api.tenbyte.com
git pull origin feat/plan-lookup-prefix-filter
docker build --no-cache -f Dockerfile.local -t api-tenbyte:local .
docker compose up -d
```

### 2. Configure Environment
- FlexPrice API credentials
- Database connection
- Payment gateway webhooks

## tenbyte-billing-dashboard Setup

### 1. Deploy Dashboard
```bash
cd /opt/tenbyte-billing-dashboard
npm install
npm run build
# Deploy static files or run with Docker
```

### 2. Configure Environment
```env
VITE_TENBYTE_API_URL=https://api.tenbyte.com
VITE_FLEXPRICE_API_URL=https://flexprice-api-url
VITE_FLEXPRICE_API_KEY=your-api-key
```

## Verification Steps

### 1. Test Subscription Flow
- [ ] User can view plans
- [ ] User can subscribe to a plan
- [ ] Wallet is created/topped up
- [ ] Subscription invoice is generated
- [ ] Invoice is auto-paid from wallet

### 2. Test Usage Metering
- [ ] Events are ingested correctly
- [ ] Usage meters update
- [ ] Overage invoices are generated when limits exceeded

### 3. Test Plan Upgrade
- [ ] Upgrade cost calculated correctly
- [ ] Old subscription cancelled
- [ ] New subscription created
- [ ] Old invoice voided (if applicable)

### 4. Test Billing Overview
- [ ] Wallet balance shows correctly
- [ ] Spent this cycle excludes top-ups
- [ ] Spent this cycle excludes voided invoices
- [ ] Payment history displays correctly

## Key Fixes Applied
1. **Multi-subscription entitlement tracking** - SubscriptionID set on each entitlement
2. **Spent calculation** - Excludes top-ups and voided invoices
3. **VoidInvoice** - Allows voiding SUCCEEDED payment status invoices
4. **Cycle start buffer** - 10-minute buffer to include first payment
