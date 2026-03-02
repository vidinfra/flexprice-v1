# FlexPrice - Usage-Based Billing & Metering Platform

## What Is FlexPrice?

FlexPrice is an open-source (AGPLv3), self-hostable usage-based metering and billing platform. It handles real-time event ingestion, tiered pricing calculations, subscription management, invoicing, wallet/credit systems, and integrates with payment providers (Stripe, Razorpay, etc.). It powers **Tenbyte's** billing infrastructure — Tenbyte's account service (`api.tenbyte.com`) calls FlexPrice APIs for all billing operations.

## Tech Stack

| Component        | Technology                              |
| ---------------- | --------------------------------------- |
| Language         | Go 1.23.0                              |
| Web Framework    | Gin                                    |
| ORM              | Ent (code-generated from schemas)       |
| OLTP Database    | PostgreSQL 17                           |
| OLAP Database    | ClickHouse 26.x                         |
| Message Broker   | Redpanda (Kafka-compatible)             |
| Pub/Sub Router   | Watermill                               |
| Workflow Engine  | Temporal.io                             |
| DI Container     | Uber Fx                                 |
| Logging          | Uber Zap                                |
| Tracing          | OpenTelemetry + SigNoz                  |
| Error Tracking   | Sentry                                  |
| Profiling        | Pyroscope                               |
| Webhooks         | Svix                                    |
| Payments         | Stripe, Razorpay, SSLCommerz            |
| Email            | Resend                                  |
| Object Storage   | AWS S3 (invoice PDFs)                   |

## Project Structure

```
flexprice-v1/
├── cmd/
│   ├── server/main.go              # Main entry point (Fx DI wiring, ~600 lines)
│   └── migrate/main.go             # Database migration runner
│
├── internal/
│   ├── api/
│   │   ├── v1/                     # 39 REST handlers (events, customers, subscriptions, invoices, etc.)
│   │   ├── cron/                   # Scheduled job handlers
│   │   ├── dto/                    # Request/response DTOs
│   │   └── router.go              # Gin route definitions + middleware stack
│   │
│   ├── service/                    # Business logic (core of the app)
│   │   ├── billing.go             # Billing calculations, invoice generation
│   │   ├── subscription.go        # Subscription lifecycle management
│   │   ├── event.go               # Event ingestion
│   │   ├── event_consumption.go   # Raw event processing from Kafka
│   │   ├── event_post_processing.go # Cost calculation per event
│   │   ├── invoice.go             # Invoice CRUD and finalization
│   │   ├── overage_billing.go     # Real-time overage charging
│   │   ├── wallet.go              # Wallet/credit management
│   │   ├── customer.go            # Customer management
│   │   ├── plan.go                # Plan management
│   │   ├── price.go               # Pricing logic (tiered, flat, slab)
│   │   ├── entitlement.go         # Feature entitlements
│   │   └── ...50+ service files
│   │
│   ├── domain/                     # Pure domain models & repository interfaces
│   │   ├── subscription/           # Subscription domain
│   │   ├── events/                 # Event domain
│   │   ├── invoice/                # Invoice domain
│   │   ├── wallet/                 # Wallet domain
│   │   ├── meter/                  # Meter/metric domain
│   │   ├── price/                  # Pricing domain (FIXED, USAGE, TIERED, SLAB)
│   │   ├── customer/               # Customer domain
│   │   ├── plan/                   # Plan domain
│   │   └── ...30+ domain packages
│   │
│   ├── repository/
│   │   ├── ent/                    # PostgreSQL repositories (Ent ORM)
│   │   └── clickhouse/            # ClickHouse repositories (events, processed_events, feature_usage)
│   │
│   ├── temporal/                   # Temporal workflows & workers
│   │   ├── workflows/             # Onboarding, price sync, HubSpot, tasks
│   │   ├── worker/                # Worker management
│   │   └── client/                # Temporal client setup
│   │
│   ├── integration/                # External provider integrations
│   │   ├── stripe/                # Stripe payment processing
│   │   ├── chargebee/             # Chargebee sync
│   │   ├── hubspot/               # HubSpot CRM
│   │   ├── quickbooks/            # QuickBooks accounting
│   │   ├── razorpay/              # Razorpay payments
│   │   ├── s3/                    # AWS S3 for invoice storage
│   │   └── factory.go             # Provider factory pattern
│   │
│   ├── pubsub/                     # Kafka/Watermill message routing
│   │   ├── kafka/                 # Kafka pub/sub implementation
│   │   ├── router/                # Watermill message router
│   │   └── memory/                # In-memory pub/sub for tests
│   │
│   ├── config/                     # Configuration (YAML + env vars)
│   │   ├── config.go              # Main config struct
│   │   └── config.yaml            # Default YAML config
│   │
│   ├── rest/middleware/            # Gin middleware (auth, CORS, request ID, Sentry)
│   ├── auth/                       # Authentication service
│   ├── rbac/                       # Role-based access control
│   ├── cache/                      # In-memory caching
│   ├── tenbyte/signoz/            # SigNoz tracing integration
│   └── ...other infra packages
│
├── ent/schema/                     # 50+ Ent ORM schema definitions
├── migrations/
│   ├── postgres/                   # PostgreSQL migration scripts
│   └── clickhouse/                # ClickHouse DDL scripts
│
├── api/                            # Auto-generated SDKs (Go, Python, JS/TS)
├── deploy/                         # Deployment configs (systemd, nginx, signoz, install.sh)
├── docker-compose.yml              # Local dev environment
├── Makefile                        # Build commands
└── ee/                             # Commercial features (1%, Open Core model)
```

## Architecture

### Layered Architecture (Clean Architecture)

1. **API Layer** (`internal/api/v1/`) — HTTP handlers with Swagger docs
2. **Service Layer** (`internal/service/`) — Business logic orchestration
3. **Domain Layer** (`internal/domain/`) — Pure domain models, business rules, repository interfaces
4. **Repository Layer** (`internal/repository/`) — Data access (Ent for Postgres, native for ClickHouse)
5. **Infrastructure** — External services (Kafka, Temporal, Stripe, etc.)

### Deployment Modes

The single binary (`flexprice-app`) runs in different modes via config:

| Mode              | Config `deployment.mode` | Purpose                           |
| ----------------- | ------------------------ | --------------------------------- |
| Local             | `local`                  | All-in-one (API + Consumer + Worker) |
| API Server        | `api`                    | REST API only (port 8080)         |
| Event Consumer    | `consumer`               | Kafka message consumer only       |
| Temporal Worker   | `temporal_worker`        | Temporal workflow worker only     |

### Event Processing Pipeline

```
POST /v1/events → Kafka "events" topic → Event Consumption (dedup, insert to ClickHouse)
                                        → Event Post-Processing (cost calculation → events_processed)
                                        → Feature Usage Tracking
                                        → Wallet Balance Alerts
```

### Dual-Database Strategy

- **PostgreSQL**: OLTP — customers, subscriptions, invoices, wallets, plans, prices
- **ClickHouse**: OLAP — raw events, processed events with costs, feature usage, aggregations

### Two Cost Calculation Methods

1. **Usage API** (`/v1/events/usage`): Recalculates from raw ClickHouse events with tiered pricing. Customer-facing display.
2. **events_processed table**: Incremental per-event cost. Used for real-time overage billing and invoicing.

### Invoice Cadence

- **ADVANCE**: Charge upfront at period start (traditional subscription)
- **ARREAR**: Charge at period end after usage is known (usage-based)

## Key Domain Concepts

- **Tenant/Environment**: Multi-tenancy with isolated data per tenant, multiple environments per tenant
- **Customer**: Has external_id for lookup, internal cust_* ID
- **Plan**: Contains base pricing and features, versioned
- **Price**: Billing types — FIXED, USAGE, TIERED, SLAB. Invoice cadence — ADVANCE or ARREAR
- **Subscription**: Binds customer to plan. States: Active, Draft, Paused, Cancelled. Billing cycles: Anniversary or Calendar
- **Subscription Line Item**: One per price on the plan. Has its own invoice cadence
- **Events**: Raw usage data points (meter name, quantity, timestamp, customer, properties)
- **Meter**: Named usage metric (e.g., bandwidth_gb, api_calls)
- **Invoice**: Generated per subscription period. Types: PERIODIC, CREDIT_NOTE, CUSTOM
- **Wallet/Credit**: Prepaid credit balance with grants, expiration, auto-deduction
- **Entitlement**: Feature access grants per subscription (on/off, metered, config values)

## Kafka Topics

| Topic                              | Purpose                         |
| ---------------------------------- | ------------------------------- |
| `events`                           | Main event stream               |
| `events_lazy`                      | Low-priority events             |
| `events_post_processing`           | Secondary processing            |
| `events_post_processing_backfill`  | Backfill processing             |
| `system_events`                    | System-level events             |
| `wallet_alert`                     | Wallet balance alerts           |

## REST API (v1)

**Auth**: `x-api-key` header or Bearer token. Per-environment API keys.

Key endpoints:
- `POST /v1/events` — Ingest usage events (returns 202)
- `GET /v1/events/usage` — Calculate usage with tiered pricing
- `POST /v1/customers` — CRUD customers
- `POST /v1/subscriptions` — CRUD subscriptions
- `GET /v1/invoices` — Invoice management
- `POST /v1/wallets` — Wallet operations (credit, debit, top-up)
- `POST /v1/plans` — Plan management
- `POST /v1/prices` — Pricing management

## Configuration

YAML config at `config/config.yaml`, overridable with `FLEXPRICE_*` env vars.

Key sections: `deployment.mode`, `server.address`, `auth.provider`, `kafka`, `postgres`, `clickhouse`, `temporal`, `s3`, `event_processing`, `webhook`, `tenbyte.signoz`

## Development

```bash
make dev-setup        # Full local dev environment (Docker Compose)
make build-image      # Docker image build
make run-server       # Local Go run
make swagger          # Generate OpenAPI docs
make generate-sdk     # Generate SDKs (Go, Python, JS)
make test             # Run tests
```

---

# Tenbyte Integration (api.tenbyte.com)

## Overview

`api.tenbyte.com` is Tenbyte's account/billing backend (Go + Gin + GORM). It manages organizations, users, auth, and subscriptions. **All billing operations proxy through FlexPrice APIs.** The FlexPrice Go SDK (v1.0.17) and a custom HTTP client are used.

**Repo location**: `/Users/saadrupai/Documents/projects/api.tenbyte.com`

## How Tenbyte Connects to FlexPrice

```
Tenbyte Dashboard (frontend)
    → api.tenbyte.com (account service)
        → FlexPrice API (billing engine)
            → ClickHouse (usage data)
            → PostgreSQL (billing data)
            → Stripe (payments)
```

### Customer Flow
1. User creates org in Tenbyte → async job queued
2. Job calls `POST /customers` on FlexPrice with org UUID as `external_id`
3. FlexPrice returns `cust_*` ID → stored in `organizations.billing_customer_id`
4. Job creates wallet: `POST /wallets` with `customer_id`
5. Customer ready for subscriptions

### Subscription Flow
1. User selects plan → Tenbyte calls `POST /subscriptions` on FlexPrice
2. FlexPrice returns `subs_*` ID → stored in `subscriptions.provider_subscription_id`
3. External systems (CDN, storage) send events to FlexPrice `POST /events`
4. Tenbyte queries usage via FlexPrice `POST /subscriptions/usage`

### Webhook Flow (FlexPrice → Tenbyte)
- `POST /v1/webhooks/flexprice/alert` — wallet balance alerts, auto top-up triggers
- `POST /v1/webhooks/flexprice/generic` — customer.updated, wallet.transaction.created

## Key Tenbyte Files

| File | Purpose |
| ---- | ------- |
| `internal/service/billing_service.go` | Core billing logic (~2,461 lines). GetOverview, UsageSummary, CreateSubscription, etc. |
| `internal/pkg/billing/providers/flexprice/api.go` | All FlexPrice API methods (~1,289 lines) |
| `internal/pkg/billing/providers/flexprice/client.go` | HTTP client with retry logic |
| `internal/pkg/billing/providers/flexprice/provider.go` | Implements Billing interface |
| `internal/pkg/billing/billing.go` | Billing interface definition |
| `internal/api/http/controller/billing/` | HTTP endpoints for billing |
| `internal/api/http/controller/webhook/flexprice_webhook_controller.go` | Webhook handler |
| `internal/jobs/billing_create_customer_job.go` | Async customer creation in FlexPrice |
| `internal/models/organization.go` | Has `billing_customer_id` field |

## Tenbyte API Endpoints (Billing)

Base: `/v1/organization/billing`

- `GET /overview/:orgID` — Wallet + usage summary
- `GET /usage/:orgID` — Detailed usage breakdown
- `GET/POST /subscriptions` — CRUD subscriptions
- `POST /subscriptions/upgrade` — Plan upgrade
- `GET /wallet` — Wallet balance
- `POST /wallet/transactions` — Top-up/refund
- `GET /invoices` — Invoice list
- `GET /plans` — Available plans

## Tenbyte Config (FlexPrice section)

```yaml
billing:
  provider: "flexprice"
  flexprice:
    flexprice_api_key: "sk_xxxxx"
    flexprice_api_url: "https://api.flexprice.io"
    flexprice_environment: "env_xxxxx"
```

---

# Production Server Setup

**Host**: `163.61.156.37` (SSH alias: `FlexPrice`)
**OS**: Ubuntu (Linux), 4 CPU cores, 8GB RAM, 96GB disk
**SSH**: `ssh FlexPrice` (key: `~/.ssh/id_flexprice`, user: root)

## Directory Layout (`/opt/`)

```
/opt/
├── flexprice/                      # Shared FlexPrice resources
│   ├── bin/
│   │   └── flexprice-app           # Single Go binary (~93MB), used by all 3 services
│   ├── config/                     # Shared config directory
│   ├── assets/                     # Email templates, fonts, images, typst templates
│   ├── scripts/
│   │   ├── backup.sh              # DB backup script (postgres + clickhouse)
│   │   └── restore.sh             # DB restore script
│   ├── backups/                    # Nightly backup storage
│   │   ├── clickhouse/
│   │   └── postgres/
│   └── logs/
│
├── flexprice-api/                  # API server working directory
│   └── config/
│       ├── config.yaml            # deployment.mode: "api" (port 8080)
│       └── rbac/
│
├── flexprice-consumer/             # Event consumer working directory
│   └── config/
│       ├── config.yaml            # deployment.mode: "consumer"
│       └── rbac/
│
├── flexprice-worker/               # Temporal worker working directory
│   └── config/
│       ├── config.yaml            # deployment.mode: "temporal_worker"
│       └── rbac/
│
├── flexprice-v1/                   # Git repo clone (source code)
│
├── flex-front/                     # FlexPrice dashboard frontend
│   ├── dist/                      # Built static files (served by Nginx)
│   ├── .env                       # Frontend env config
│   └── server.js                  # Node.js server (port 5173, currently inactive)
│
├── flexprice-backup/               # Pre-migration backup snapshot
│   ├── clickhouse/
│   ├── postgres/
│   └── config/
│
├── redpanda/                       # Redpanda (Kafka-compatible broker)
│   ├── bin/redpanda
│   └── lib/, libexec/
│
├── temporal/                       # Temporal server
│   ├── bin/temporal-server
│   ├── config/development.yaml    # Uses PostgreSQL backend
│   └── schema/                    # Temporal DB schemas
│
└── signoz/                         # SigNoz monitoring stack
    ├── bin/
    │   ├── signoz-otel-collector
    │   └── signoz-query-service
    ├── config/
    │   ├── otel-collector-config.yaml
    │   ├── signoz-config.yaml
    │   └── prometheus.yml
    ├── web/                        # SigNoz web UI
    └── templates/
```

## Systemd Services (`/etc/systemd/system/`)

### FlexPrice Application Services

All three use the **same binary** (`/opt/flexprice/bin/flexprice-app`) with different working directories (and thus different `config.yaml` files that set different `deployment.mode`).

| Service | Unit File | WorkingDirectory | Port | Description |
| ------- | --------- | ---------------- | ---- | ----------- |
| `flexprice-api` | `flexprice-api.service` | `/opt/flexprice-api` | 8080 | REST API server |
| `flexprice-consumer` | `flexprice-consumer.service` | `/opt/flexprice-consumer` | — | Kafka event consumer |
| `flexprice-worker` | `flexprice-worker.service` | `/opt/flexprice-worker` | — | Temporal workflow worker |

Common settings: `User=flexprice`, `Restart=always`, `RestartSec=5`, `MemoryMax=512M`, `LimitNOFILE=65535`, `NoNewPrivileges=true`

Dependencies: `After=postgresql.service clickhouse-server.service redpanda.service temporal.service`

### Infrastructure Services

| Service | Unit File | User | Port(s) | MemoryMax | Notes |
| ------- | --------- | ---- | ------- | --------- | ----- |
| `redpanda` | `redpanda.service` | `redpanda` | 9092 (Kafka), 9644 (admin), 33145 (RPC) | 3G | Config: `/etc/redpanda/redpanda.yaml`. 3G memory, 2 SMP cores, developer_mode. **MemoryMax must match redpanda config `memory` setting or OOM crash loop occurs.** |
| `clickhouse-server` | (system-managed) | — | 9000, 8123 | — | ClickHouse 26.1.3, system package |
| `postgresql` | `postgresql@17-main` | — | 5432 | — | PostgreSQL 17.8, system package |
| `temporal` | `temporal.service` | `temporal` | 7233 (frontend), 7234 (history), 7235 (matching), 7239 (worker) | 1G | Config: `/opt/temporal/config/development.yaml`, uses PostgreSQL backend |

### Monitoring Services

| Service | Unit File | Port | Notes |
| ------- | --------- | ---- | ----- |
| `signoz-otel-collector` | `signoz-otel-collector.service` | — | Config: `/opt/signoz/config/otel-collector-config.yaml` |
| `signoz-query-service` | `signoz-query-service.service` | 8080 (internal) | Web UI at `/opt/signoz/web`, SQLite metadata at `/var/lib/signoz/signoz.db` |

### Frontend Service

| Service | Unit File | Port | Notes |
| ------- | --------- | ---- | ----- |
| `flex-front` | `flex-front.service` | 5173 | Currently **inactive**. Dashboard served as static files via Nginx instead |

## Nginx Reverse Proxy (`/etc/nginx/sites-enabled/`)

All domains use HTTPS via Let's Encrypt (Certbot managed).

| Domain | Nginx Config | Backend | Purpose |
| ------ | ------------ | ------- | ------- |
| `api-staging-flex.tenbyte.io` | `flexprice` | `127.0.0.1:8080` | FlexPrice REST API |
| `api-billing.tenbyte.io` | `api-billing` | `127.0.0.1:8000` | Tenbyte billing proxy |
| `billing.tenbyte.io` | `billing-dashboard` | Static files at `/opt/flex-front/dist` | FlexPrice dashboard (SPA) |
| `monitoring.tenbyte.io` | `monitoring` | `127.0.0.1:8080` | SigNoz monitoring UI |

## Cron Jobs

| Schedule | Command | Purpose |
| -------- | ------- | ------- |
| Daily 2 AM | `/opt/flexprice/scripts/backup.sh all` | Backup PostgreSQL + ClickHouse to `/opt/flexprice/backups/` |

## Service Status (as of last check)

| Service | Status |
| ------- | ------ |
| flexprice-api | **active** |
| flexprice-consumer | **active** |
| flexprice-worker | **active** |
| redpanda | **active** |
| clickhouse-server | **active** |
| postgresql | **active** |
| temporal | **active** |
| signoz-otel-collector | **active** |
| signoz-query-service | **active** |
| nginx | **active** |
| flex-front | **inactive** (dashboard served via Nginx static) |

## Common Operations

```bash
# SSH into server
ssh FlexPrice

# Restart a FlexPrice service
sudo systemctl restart flexprice-api
sudo systemctl restart flexprice-consumer
sudo systemctl restart flexprice-worker

# View logs
sudo journalctl -u flexprice-api -f
sudo journalctl -u flexprice-consumer -f
sudo journalctl -u flexprice-worker -f

# Deploy new binary
# 1. Build locally: GOOS=linux GOARCH=amd64 go build -o flexprice-app ./cmd/server
# 2. Copy to server: scp flexprice-app FlexPrice:/opt/flexprice/bin/
# 3. Restart services

# Edit API config
sudo nano /opt/flexprice-api/config/config.yaml
sudo systemctl restart flexprice-api

# Run backup manually
sudo /opt/flexprice/scripts/backup.sh all

# Check Redpanda topics
rpk topic list
rpk topic consume events --num 5

# Check Redpanda data size (retention set to 7 days / 1GB per topic)
du -sh /var/lib/redpanda/data/
```

## Known Issues & Fixes

### Redpanda OOM Crash Loop (Fixed 2026-03-02)
**Symptom**: Redpanda crash-looping (24,332 restarts), `connection refused` on port 9092, FlexPrice consumer/webhooks not working.
**Root cause**: Systemd `MemoryMax=2G` but redpanda config `memory: 3G`. Each shard only got 118MB (needs 1GB min). On startup, replaying 9.9GB of partition logs triggered OOM → SIGABRT → restart loop.
**Fix**: Changed `/etc/systemd/system/redpanda.service` `MemoryMax` from `2G` to `3G`. Set topic retention to 7 days + 1GB max per topic to prevent unbounded data growth.
**Prevention**: Always ensure systemd `MemoryMax` >= redpanda config `memory` value.

---

# Frontend Applications

## Tenbyte Console (Main Dashboard)

**Repo**: `/Users/saadrupai/Documents/projects/frontend`
**Framework**: Next.js 16 + React 19 + TypeScript
**Styling**: Tailwind CSS 4 + Shadcn/ui (Radix UI primitives)
**State**: Zustand (auth, UI) + React Query (server state) + nuqs (URL state)
**Forms**: React Hook Form + Zod validation
**HTTP**: Axios with JWT interceptors and auto token refresh

### Key Pages

| Route | Purpose |
| ----- | ------- |
| `/sign-in`, `/sign-up` | Authentication |
| `/organization/billing/overview` | Billing dashboard — wallet balance, cycle spend |
| `/organization/billing/plans` | Plan catalog and subscription flow |
| `/organization/billing/plans/checkout/[planId]` | Plan checkout with payment |
| `/organization/billing/plans/upgrade` | Plan upgrade |
| `/organization/billing/usages` | Usage analytics breakdown |
| `/organization/billing/topup` | Wallet top-up |
| `/organization/billing/auto-topup` | Auto top-up settings |
| `/organization/billing/settings` | Billing settings |
| `/vidinfra` | Video libraries management |
| `/vidinfra/[libraryId]/videos` | Videos in library |
| `/cdn/(general)/distributions` | CDN distributions |
| `/cdn/(manage)/distributions/[id]/*` | CDN management (cache, access, analytics) |

### API Connection

- Connects to `api.tenbyte.com` via `NEXT_PUBLIC_API_BASE_URL`
- Auth: JWT Bearer token, stored in cookies, auto-refreshed on 401
- Billing calls go through Tenbyte API (which proxies to FlexPrice)

### Key Directories

```
src/
├── app/                          # Next.js App Router (routes)
│   └── (dashboard)/(organization)/billing/  # Billing pages
├── features/
│   ├── billing/                  # Plans, subscriptions, invoices, wallets
│   ├── streams/                  # Video streaming
│   ├── cdn/                      # CDN management
│   └── ...
├── lib/auth/                     # Auth store (Zustand), JWT handling
├── lib/axios-config.ts           # Axios with interceptors
├── schemas/                      # Zod validation schemas
└── components/ui/                # Shadcn/ui components
```

---

## Tenbyte Billing Dashboard (Standalone)

**Repo**: `/Users/saadrupai/Documents/projects/tenbyte-billing-dashboard`
**Framework**: React 19 + TypeScript + Vite 7
**Styling**: Tailwind CSS 4 (dark theme)
**State**: React Query (30s stale time, 30s polling for usage)
**HTTP**: Axios with JWT interceptors

### Dual-API Architecture

This dashboard connects to **both** APIs:

1. **Tenbyte API** (`VITE_TENBYTE_API_URL`) — Auth, subscriptions, plans, billing, invoices
   - Auth: JWT Bearer token from `/v1/auth/login`
   - All billing under `/v1/organization/billing/`
2. **FlexPrice API** (`VITE_FLEXPRICE_API_URL`) — Event ingestion, raw usage queries, wallets
   - Auth: `X-API-Key` header
   - Direct calls to `/events`, `/events/usage`, `/wallets/`

### Key Views

| View | Component | Features |
| ---- | --------- | -------- |
| Billing Overview | `BillingOverview.tsx` | Wallet balance, negative detection, quick top-up ($25/$50/$100), transactions |
| My Subscriptions | `CurrentSubscription.tsx` | Active subs by category (vidinfra/cdn), usage tracking, cancellation |
| Plans | `PlansList.tsx` | Browse plans, subscribe, upgrade detection |
| Usage | `PlansUsage.tsx` | Per-subscription usage, metered/boolean features, 30s auto-refresh |
| Invoices | `Invoices.tsx` | Invoice list, status, PDF download, summary cards |
| Event Generator | `MockEventGenerator.tsx` | Test event generation for FlexPrice |

### Payment Gateways

- **Default** — Wallet / saved payment method
- **Stripe** — International payments
- **SSLCommerz** — Bangladesh payments

### Environment Variables

```env
VITE_FLEXPRICE_API_URL=http://localhost:8080
VITE_FLEXPRICE_API_KEY=sk_xxxxx
VITE_TENBYTE_API_URL=http://localhost:8090
```

### Key Files

```
src/
├── api/
│   ├── flexprice.ts              # FlexPrice API client (events, usage, wallet)
│   └── tenbyte.ts                # Tenbyte API client (auth, billing, plans, subs)
├── components/
│   ├── BillingOverview.tsx        # Wallet + spending dashboard
│   ├── CurrentSubscription.tsx    # Active subscriptions + usage
│   ├── PlansList.tsx              # Plan catalog
│   ├── PlansUsage.tsx             # Usage breakdown
│   ├── Invoices.tsx               # Invoice management
│   ├── Login.tsx                  # Auth (login/register)
│   ├── PaymentMethodModal.tsx     # Payment gateway selection
│   └── MockEventGenerator.tsx     # Test event generator
├── context/AuthContext.tsx         # JWT + org ID management
└── types/index.ts                 # TypeScript interfaces
```

---

# Development Conventions

- **Architecture**: Clean/layered — handler → service → domain → repository
- **Error handling**: Wrapped with CockroachDB errors for full stack traces, custom error types in `internal/errors`
- **Multi-tenancy**: Every query scoped by tenant_id and environment_id via context
- **Soft deletes**: Entities archived, not hard-deleted
- **Idempotency**: API operations use idempotency keys
- **Testing**: Table-driven tests, mock repositories, in-memory pub/sub for testing
- **Config**: YAML primary, `FLEXPRICE_*` env vars for overrides
- **Ent ORM**: Schemas in `ent/schema/`, auto-generated code — run `go generate ./ent` after schema changes

---

# Billing UI Conventions (Tenbyte Frontend)

## Cancel Subscription
- **Do NOT call the cancel subscription API.** The flow is: select reason → confirm → show "Contact Support" message with `mailto:support@tenbyte.io`.
- The `CancelSubscriptionModal` receives `planName` prop from `PlansOverview` to display which plan is being cancelled.

## Billing Address
- Country field is **locked after first save** — uses `selectProps={{ disabled: !!initialValues?.country }}` on the `SelectField` component.
- The warning "Billing country cannot be changed once set" is shown alongside the disabled field.

## Bill As
- Removed. All billing defaults to organization. No individual/company toggle.

## Plans Grid
- Plans are displayed per category (Vidinfra, CDN) with a responsive grid:
  - 1 plan → single column, centered (`max-w-md mx-auto`)
  - 2 plans → 2-column grid (`md:grid-cols-2`)
  - 3+ plans → 3-column grid (`md:grid-cols-3`)
- Outer container: `max-w-5xl mx-auto`

## Payment History Table (Overview)
- Columns are sortable client-side (click header to toggle asc/desc).
- Backend also supports `sort_field` and `sort_order` query params on `GET /overview/:orgID`.
- Invoice PDF download tracks per-invoice loading state (`downloadingId`).
- If `invoice_download_url` is empty, download button is disabled with tooltip "PDF not available yet".
- Backend rejects PDF download for DRAFT invoices with HTTP 400.

## Invoice Payment
- Unpaid invoices show "Pay Now" → navigates to `/organization/billing/plans/invoice/{invoiceId}/pay`
- Supports Stripe and SSLCommerz payment gateways.
- Saved card (default payment method) vs new card choice modal.
