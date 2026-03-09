# Tenbyte FlexPrice Fork - Developer Documentation

This document describes all modifications made to the FlexPrice billing system fork (`flexprice-v1`) for Tenbyte's requirements. This fork is based on FlexPrice v1.0.47.

---

## Table of Contents

1. [Overview](#overview)
2. [SSLCommerz Payment Gateway Integration](#1-sslcommerz-payment-gateway-integration)
3. [Real-Time Overage Billing](#2-real-time-overage-billing)
4. [Unified Upgrade Flow via Webhooks](#3-unified-upgrade-flow-via-webhooks)
5. [Schema & Database Changes](#4-schema--database-changes)
6. [Security Improvements](#5-security-improvements)
7. [Bug Fixes & Code Quality](#6-bug-fixes--code-quality)
8. [Configuration Changes](#7-configuration-changes)
9. [File Reference](#8-file-reference)

---

## Overview

### Purpose
This fork extends FlexPrice to support Tenbyte's billing requirements:
- **SSLCommerz** payment gateway for Bangladesh market
- **Real-time overage billing** with threshold-based invoicing
- **Negative wallet balance** support for post-paid usage
- **Webhook notifications** when wallet goes negative (for api.tenbyte to block users)

### Architecture Summary

```
┌─────────────────────────────────────────────────────────────────────┐
│                        api.tenbyte.com                              │
│  - Pushes hourly usage events to FlexPrice                         │
│  - Receives webhook: "wallet.balance.negative"                     │
│  - Decides whether to block user based on webhook                  │
└─────────────────────────────────────────────────────────────────────┘
                              ↕ (Events & Webhooks)
┌─────────────────────────────────────────────────────────────────────┐
│                        FlexPrice (this repo)                        │
│  - Event post-processing with overage detection                    │
│  - Threshold-based invoice creation ($5 buckets)                   │
│  - Wallet deduction (unlimited negative allowed)                   │
│  - Webhook when wallet crosses to negative                         │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 1. SSLCommerz Payment Gateway Integration

### What
Added SSLCommerz as a payment gateway option alongside Stripe, enabling payments via bKash, Nagad, cards, and bank transfers for the Bangladesh market.

### Why
Stripe has limited support in Bangladesh. SSLCommerz is the dominant payment aggregator supporting local payment methods.

### Key Files

| File | Purpose |
|------|---------|
| `internal/integration/sslcommerz/client.go` | HTTP client for SSLCommerz Session API and Validation API |
| `internal/integration/sslcommerz/payment.go` | Payment service - creates payment links, handles responses |
| `internal/integration/sslcommerz/dto.go` | Request/response DTOs for SSLCommerz APIs |
| `internal/integration/sslcommerz/webhook/handler.go` | IPN (Instant Payment Notification) webhook handler |
| `internal/integration/sslcommerz/webhook/dto.go` | Webhook payload DTOs |
| `internal/integration/factory.go` | Factory to instantiate SSLCommerz or Stripe based on connection |
| `internal/api/v1/webhook.go` | HTTP endpoint for SSLCommerz IPN callbacks |

### Flow

```
1. User initiates payment
   └── POST /payments with gateway_type: SSLCOMMERZ

2. FlexPrice creates SSLCommerz session
   └── SSLCommerz returns payment URL (GatewayPageURL)

3. User completes payment on SSLCommerz hosted page
   └── SSLCommerz redirects to success/fail/cancel URL

4. SSLCommerz sends IPN callback
   └── POST /v1/webhooks/sslcommerz/ipn
   └── Handler validates signature, updates payment status
   └── Triggers wallet credit if payment succeeded
```

### Configuration

```yaml
# config.yaml
sslcommerz:
  sandbox: true  # Use sandbox for testing
  ipn_url: "https://api.flexprice.tenbyte.io/v1/webhooks/sslcommerz/ipn"
  success_url: "https://dashboard.tenbyte.io/payment/success"
  fail_url: "https://dashboard.tenbyte.io/payment/failed"
  cancel_url: "https://dashboard.tenbyte.io/payment/cancelled"
```

Store credentials are stored via the Connection API (POST /connections), not in config.

### Security Notes
- IPN validation uses MD5 hash verification with store password
- Credentials stored securely in Connection metadata (encrypted)
- No credentials in config files or logs

---

## 2. Real-Time Overage Billing

### What
Detects usage overage during event processing, accumulates cost per subscription, and creates invoices when a threshold (default $5) is reached.

### Why
Tenbyte needs real-time billing to:
1. Bill customers immediately when they exceed their plan limits
2. Track negative wallet balances
3. Notify api.tenbyte when users should be blocked (via webhook)

### Key Files

| File | Purpose |
|------|---------|
| `internal/service/overage_billing.go` | Main overage billing service |
| `internal/service/overage_billing_test.go` | Comprehensive test suite (21 tests) |
| `internal/types/settings.go` | `OverageBillingConfig` type and setting key |
| `internal/types/webhook.go` | `WebhookEventWalletBalanceNegative` event type |
| `internal/webhook/dto/wallet.go` | `WalletNegativeBalancePayload` structure |
| `internal/webhook/payload/wallet.go` | Payload builder for negative balance webhook |

### Flow

```
Event Arrives (hourly usage from Tenbyte)
       ↓
Event Post-Processing (event_post_processing.go:740)
       ↓
Calculate Cost (existing FlexPrice logic)
       ↓
OverageBillingService.ProcessEventOverage()
       ↓
Query accumulated cost for subscription period (ClickHouse)
       ↓
If accumulated >= $5 threshold:
       │
       ├── 1. Create Invoice (fixed $5 amount per bucket)
       │      - Idempotency key: "overage_{sub_id}_{period_id}_{bucket}"
       │
       ├── 2. Deduct from Wallet
       │      - Allow negative balance (no limit)
       │
       ├── 3. Mark Invoice as PAID
       │
       └── 4. Check: Did wallet cross from positive to negative?
               │
               └── If YES → Send webhook: "wallet.balance.negative"
```

### Example Scenario

```
Subscription: sub_123
Entitlement: 1000 API calls/month FREE
Overage Price: $0.01 per call
Invoice Threshold: $5.00
Wallet Balance: $3.00

Timeline:
─────────────────────────────────────────────────────────────────────

Event 1001 → Overage! Cost: $0.01 → Accumulated: $0.01
Event 1002 → Overage! Cost: $0.01 → Accumulated: $0.02
...
Event 1500 → Overage! Cost: $0.01 → Accumulated: $5.00 ← THRESHOLD!
                                          ↓
                              Create Invoice: $5.00
                              Deduct Wallet: $3.00 → -$2.00
                              Invoice Status: PAID ✓
                              Wallet crossed to negative!
                                          ↓
                              Send Webhook: "wallet.balance.negative"
                                          ↓
Event 1501 → Accumulated: $0.01 (fresh start after invoice)
```

### Webhook Payload

```json
{
  "event_type": "wallet.balance.negative",
  "timestamp": "2026-01-15T10:00:00Z",
  "data": {
    "customer_id": "cust_123",
    "subscription_id": "sub_456",
    "wallet_id": "wal_789",
    "wallet_balance": "-2.00",
    "currency": "USD",
    "invoice_id": "inv_abc"
  }
}
```

### Configuration

Stored in settings table via API:

```json
{
  "key": "overage_billing_config",
  "value": {
    "enabled": true,
    "invoice_threshold": "5.00"
  }
}
```

---

## 3. Unified Upgrade Flow via Webhooks

### What
All plan upgrades (Stripe auto-charge, SSLCommerz, payment links) now follow the same flow: payment completes → webhook fires → upgrade executes.

### Why
Originally, Stripe auto-charge would execute the upgrade immediately, while other payment methods waited for webhooks. This caused inconsistent behavior and made it hard to debug. Now all methods use the same `wallet.transaction.created` webhook to trigger upgrades.

### Flow (All Payment Methods)

```
1. User initiates upgrade
   └── POST /subscriptions/{id}/upgrade

2. Calculate upgrade cost, create wallet top-up request
   └── If auto-charge (Stripe): Charge card, return "upgrade will be processed via webhook"
   └── If payment link: Return payment URL

3. Payment completes
   └── SSLCommerz IPN or Stripe webhook
   └── Creates wallet transaction with metadata: {pending_upgrade: true, ...}

4. wallet.transaction.created webhook fires
   └── api.tenbyte receives webhook
   └── ExecutePendingUpgradeFromMetadata():
       - Cancel old subscription
       - Create new subscription with upgraded plan
       - Debit wallet for upgrade cost
       - Void/cancel new subscription's auto-generated invoice
```

### Key Changes

```go
// internal/service/billing.go - Stripe auto-charge no longer executes upgrade
if hasDefaultPaymentMethod {
    _, err := s.CreateWalletTransaction(ctx, orgID, topUpReq)
    if err == nil {
        // Don't execute upgrade here - let webhook handle it
        return &UpgradeSubscriptionResponse{
            PaymentRequired: false,
            Message: "Payment successful. Upgrade will be processed shortly.",
        }, nil
    }
}
```

### Metadata on Wallet Transaction

```json
{
  "pending_upgrade": "true",
  "from_plan_id": "plan_basic",
  "to_plan_id": "plan_pro",
  "subscription_id": "sub_123",
  "billing_customer_id": "cust_456",
  "upgrade_cost": "70.00"
}
```

---

## 4. Schema & Database Changes

### Subscription Schema
Added `invoice_cadence` field for billing frequency control.

```go
// ent/schema/subscription.go
field.String("invoice_cadence").Optional().Nillable()
```

### Wallet Transaction Schema
Added balance tracking fields:

```go
// ent/schema/wallettransaction.go
field.Other("balance_before", decimal.Decimal{}).SchemaType(decimalSchemaType).Optional()
field.Other("balance_after", decimal.Decimal{}).SchemaType(decimalSchemaType).Optional()
```

### Migrations

| File | Purpose |
|------|---------|
| `migrations/postgres/V3__plan_defaults.sql` | Plan default values |
| `migrations/postgres/V4__prices_plan_id_nullable.sql` | Allow nullable plan_id on prices |
| `migrations/postgres/V5__wallet_transaction_balance_fields.up.sql` | Add balance_before/after columns |

---

## 5. Security Improvements

### CORS Origin Whitelisting
Replaced permissive CORS with explicit origin allowlist:

```yaml
# config.yaml
server:
  allowed_origins:
    - "http://localhost:3000"
    - "https://app.flexprice.io"
    - "https://dashboard.tenbyte.io"
```

### CORS Credentials Fix
Fixed bug where `Access-Control-Allow-Credentials: true` was set with wildcard origin (browsers reject this):

```go
// internal/rest/middleware/cors.go
if origin != "" && (len(allowedOrigins) == 0 || allowedSet[origin]) {
    c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
    allowCredentials = true  // Only with specific origin
} else if len(allowedOrigins) == 0 {
    c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
    // No credentials with wildcard
}
```

### Debug Log Removal
Removed debug logs that leaked sensitive information:
- Tenant webhook endpoints (config.go)
- Transaction metadata (wallet.go)

---

## 6. Bug Fixes & Code Quality

### Sentry Error Capture
Added Sentry integration for overage billing failures:

```go
// internal/service/event_post_processing.go
sentrySvc := sentry.NewSentryService(s.Config, s.Logger)  // Initialize once
// ...
if err := overageBillingService.ProcessEventOverage(...); err != nil {
    sentrySvc.CaptureException(err)  // Reuse instance
}
```

### MAX Aggregation Documentation
Added documentation for eventual consistency edge case:

```go
// NOTE on eventual consistency: Under heavy load, concurrent events may see
// stale currentMax values due to ClickHouse merge lag. Mitigations:
// 1. Query uses FINAL modifier for read consistency
// 2. ReplacingMergeTree deduplicates by event ID at merge time
// 3. Overage invoices use fixed threshold amounts, not summed event costs
// 4. Final billing recalculates from MAX(qty_total), not summed deltas
```

### Threshold Bucket Calculation
Fixed potential edge case with explicit Floor():

```go
// Before: targetBucket := totalPeriodCost.Div(config.InvoiceThreshold).IntPart()
// After:
targetBucket := totalPeriodCost.Div(config.InvoiceThreshold).Floor().IntPart()
```

### Real-Time Wallet Balance Overage Deduction
Fixed `GetWalletBalanceV2` to properly account for already-invoiced overage amounts when calculating pending charges.

**Problem**: When overage billing creates invoices in real-time (e.g., $5 buckets), the wallet balance calculation was still counting the full period usage as "pending charges". This caused:
- Real-time balance to show incorrectly low values even after auto top-up
- Wallet alert state never recovering from `in_alarm` to `ok`
- Auto top-up triggering once but never again

**Example Before Fix**:
```
Total period usage:    $108
Already invoiced:      $103 (21 overage invoices × $5)
Pending charges:       $108 (WRONG - counted full usage)
Wallet balance:        $100
Real-time balance:     $100 - $108 = -$8 (WRONG)
```

**Example After Fix**:
```
Total period usage:    $108
Already invoiced:      $103 (21 overage invoices × $5)
Pending charges:       $5   (CORRECT - only unbilled usage)
Wallet balance:        $100
Real-time balance:     $100 - $5 = $95 (CORRECT)
```

**Files Changed**:

| File | Change |
|------|--------|
| `internal/service/billing.go` | Made `GetOverageInvoicedAmount` public (was private `getOverageInvoicedAmount`) |
| `internal/service/wallet.go` | In `GetWalletBalanceV2`, subtract overage already invoiced before adding to pending charges |

**Code Change** (wallet.go lines 2226-2254):
```go
// Calculate usage charges
usageCharges, usageTotal, err := billingService.CalculateUsageCharges(ctx, sub, usage, periodStart, periodEnd)

// NEW: Deduct already-invoiced overage amounts to prevent double counting
overageInvoiced, err := billingService.GetOverageInvoicedAmount(ctx, sub.ID, periodStart, periodEnd)
if err != nil {
    s.Logger.Warnw("failed to get overage invoiced amount, proceeding without deduction", ...)
    overageInvoiced = decimal.Zero
}

// Adjust usage total by subtracting overage already invoiced
adjustedUsageTotal := usageTotal.Sub(overageInvoiced)
if adjustedUsageTotal.LessThan(decimal.Zero) {
    adjustedUsageTotal = decimal.Zero  // Protect against negative
}

totalPendingCharges = totalPendingCharges.Add(adjustedUsageTotal)  // Was: Add(usageTotal)
```

This fix ensures wallet balance alerts work correctly with overage billing:
1. Auto top-up triggers when balance drops below threshold
2. Balance recovers above threshold after top-up (alert state → `ok`)
3. Next usage drop can trigger another alert (state transition `ok` → `in_alarm`)

---

## 7. Configuration Changes

### New Config Sections

```yaml
# SSLCommerz configuration
sslcommerz:
  sandbox: true
  ipn_url: "https://api.flexprice.tenbyte.io/v1/webhooks/sslcommerz/ipn"
  success_url: "https://dashboard.tenbyte.io/payment/success"
  fail_url: "https://dashboard.tenbyte.io/payment/failed"
  cancel_url: "https://dashboard.tenbyte.io/payment/cancelled"

# CORS allowed origins
server:
  allowed_origins:
    - "http://localhost:3000"
    - "https://dashboard.tenbyte.io"
    - "https://account.tenbyte.io"

# Webhook tenant configuration
webhook:
  tenants:
    tenant_id_here:
      enabled: true
      endpoint: "https://api.tenbyte.com/webhooks/flexprice"
      headers:
        Authorization: "Bearer xxx"
```

---

## 8. File Reference

### New Files

| Path | Description |
|------|-------------|
| `internal/integration/sslcommerz/*` | SSLCommerz payment gateway integration |
| `internal/service/overage_billing.go` | Real-time overage billing service |
| `internal/service/overage_billing_test.go` | Test suite for overage billing |
| `internal/testutil/mock_processed_event_repo.go` | Mock repository for testing |
| `migrations/postgres/V3-V5*.sql` | Database migrations |
| `TENBYTE_CHANGES.md` | This documentation |

### Modified Files (Key Changes)

| Path | Changes |
|------|---------|
| `internal/service/event_post_processing.go` | Hook for overage billing after cost calculation |
| `internal/service/payment_processor.go` | SSLCommerz integration, negative balance support |
| `internal/service/wallet_payment.go` | Allow negative wallet balance |
| `internal/service/wallet.go` | Balance tracking on transactions; `GetWalletBalanceV2` overage deduction fix |
| `internal/service/billing.go` | Made `GetOverageInvoicedAmount` public for wallet balance calculation |
| `internal/rest/middleware/cors.go` | Origin whitelisting, credentials fix |
| `internal/config/config.go` | SSLCommerz, CORS config parsing |
| `internal/types/webhook.go` | New webhook event types |
| `internal/webhook/payload/factory.go` | Register new payload builders |

---

## Testing

### Overage Billing Tests
Run the overage billing test suite:

```bash
go test -v ./internal/service/... -run TestOverageBilling
```

### Manual Testing Flow

1. **Setup**: Create customer, subscription, wallet with $10 balance
2. **Ingest events**: Send usage events exceeding entitlement
3. **Verify**: Check that invoices are created at $5 thresholds
4. **Verify**: Check wallet goes negative after balance depleted
5. **Verify**: Check `wallet.balance.negative` webhook is sent

---

## Contact

For questions about these changes, contact the Tenbyte engineering team.

- **Original FlexPrice**: https://github.com/flexprice/flexprice
- **This Fork**: flexprice-v1 (internal Tenbyte repo)
