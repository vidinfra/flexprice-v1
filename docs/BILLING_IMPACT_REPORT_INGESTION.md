# Billing Event Ingestion — Impact Report

**Date:** 2026-03-08

---

## tenbyte-cdn-api

**Branch:** `feat/billing-requests-and-vidinfra-split`

### Database Changes

None.

### Code Changes

| File | Change |
|------|--------|
| `internal/jobs/billing_metric_push_job.go` | Push `requests` metric alongside `traffic`; look up distribution type from DB; semaphore context cancellation fix; warn log for missing distributions |
| `internal/pkg/billing/providers/flexprice/provider.go` | Dynamic event names by distribution type (`cdn.*` vs `vidinfra.*`); skip billing for library+requests; traffic sent as GB, requests sent as raw count |
| `internal/pkg/billing/billing.go` | Added `ShouldSkipBilling()` to interface |

### Server Impact

- No new services or infra changes.
- Existing billing worker pushes 3 event types instead of 1.
- Event name `traffic.usage` renamed to `cdn.traffic.usage` — FlexPrice meter must match.

---

## vidinfra-api

**Branch:** `feat/billing-storage-push`

### Database Changes

**2 new tables** (migration: `20260304000001_create_billing_tables.sql`):

**`billing_snapshots`** — Tracks each storage usage event sent to FlexPrice.

| Column | Type |
|--------|------|
| `id` | `BIGSERIAL PK` |
| `organization_id` | `UUID NOT NULL` |
| `metric_type` | `TEXT` |
| `value` | `DOUBLE PRECISION` |
| `unit` | `TEXT` |
| `snapshot_hour` | `BIGINT` |
| `billing_status` | `TEXT` (pending/sent/failed/confirmed) |
| `billing_provider` | `TEXT` |
| `external_event_id` | `TEXT` |
| `idempotency_key` | `TEXT UNIQUE` |
| `failure_count` | `INTEGER` |
| `last_error` | `TEXT` |
| `processed_at` | `TIMESTAMPTZ` |
| `created_at` / `updated_at` | `TIMESTAMPTZ` |

Indexes: `organization_id`, `snapshot_hour`, `billing_status`, unique on `idempotency_key`.

**`billing_job_logs`** — Tracks each job execution with success/failure counts and FlexPrice health status.

No foreign keys to existing tables. No changes to existing tables.

### Code Changes

| File | Change |
|------|--------|
| `config/config.go` | Added billing config structs |
| `config/config.yaml` | Added `billing:` section |
| `internal/pkg/billing/billing.go` | Billing interface |
| `internal/pkg/billing/module.go` | Fx DI module (FlexPrice or no-op fallback) |
| `internal/pkg/billing/providers/flexprice/provider.go` | FlexPrice SDK wrapper |
| `internal/models/billing_snapshot.go` | BillingSnapshot model |
| `internal/models/billing_job_log.go` | BillingJobLog model |
| `internal/jobs/billing_storage_push_job.go` | Hourly storage push job |
| `internal/jobs/module.go` | Registered new job |
| `internal/bootstrap/bootstrap.go` | Added `billing.Module` |
| `go.mod` / `go.sum` | Added `flexprice/go-sdk@v1.0.17` |

### Server Impact

- Migration must run before deploy.
- New hourly cron job (`:05` every hour) pushes `storage.usage` events to FlexPrice.
- Reads `videos` table (read-only). No impact on existing API or worker flows.
- If FlexPrice is unreachable, snapshots are marked `failed` and retried next hour.
