#!/bin/bash
# Run ChartMogul UUID Migration

set -e  # Exit on error

echo "================================================"
echo "ChartMogul UUID Migration - Execution"
echo "================================================"
echo ""

cd /var/www/html/flexprice

# Ensure postgres is running
echo "📦 Starting postgres..."
docker compose up -d postgres
sleep 5

# Create backup
echo ""
echo "💾 Creating backup..."
BACKUP_FILE="backup_$(date +%Y%m%d_%H%M%S).dump"
docker compose exec -T postgres pg_dump -U flexprice -d flexprice -F c > "$BACKUP_FILE"

if [ -f "$BACKUP_FILE" ]; then
    BACKUP_SIZE=$(ls -lh "$BACKUP_FILE" | awk '{print $5}')
    echo "  ✓ Backup created: $BACKUP_FILE ($BACKUP_SIZE)"
else
    echo "  ✗ Backup failed!"
    exit 1
fi

# Run migration
echo ""
echo "🚀 Running migration..."
echo "  File: migrations/postgres/V3__add_chartmogul_uuid_columns.up.sql"
echo ""

if docker compose exec -T postgres psql -U flexprice -d flexprice < migrations/postgres/V3__add_chartmogul_uuid_columns.up.sql; then
    echo ""
    echo "  ✅ Migration executed successfully!"
else
    echo ""
    echo "  ❌ Migration failed!"
    echo ""
    echo "To rollback, run:"
    echo "  docker compose exec -T postgres psql -U flexprice -d flexprice < migrations/postgres/V3__add_chartmogul_uuid_columns.down.sql"
    echo ""
    echo "Or restore from backup:"
    echo "  docker compose exec -T postgres pg_restore -U flexprice -d flexprice < $BACKUP_FILE"
    exit 1
fi

# Verify migration
echo ""
echo "🔍 Verifying migration..."
echo ""

echo "Checking columns created:"
docker compose exec -T postgres psql -U flexprice -d flexprice -c "
SELECT table_name, column_name, data_type 
FROM information_schema.columns 
WHERE table_schema = 'public' 
  AND column_name LIKE '%chartmogul%'
ORDER BY table_name, column_name;
"

echo ""
echo "Checking indexes created:"
docker compose exec -T postgres psql -U flexprice -d flexprice -c "
SELECT indexname, tablename 
FROM pg_indexes 
WHERE indexname LIKE '%chartmogul%';
"

echo ""
echo "Checking data migration:"
docker compose exec -T postgres psql -U flexprice -d flexprice -c "
SELECT 
  'customers' as table_name,
  COUNT(*) as total_rows,
  COUNT(chartmogul_uuid) as rows_with_uuid,
  COUNT(*) FILTER (WHERE metadata->>'chartmogul_customer_uuid' IS NOT NULL) as metadata_had_uuid
FROM customers
UNION ALL
SELECT 
  'plans',
  COUNT(*),
  COUNT(chartmogul_uuid),
  COUNT(*) FILTER (WHERE metadata->>'chartmogul_plan_uuid' IS NOT NULL)
FROM plans
UNION ALL
SELECT 
  'subscriptions',
  COUNT(*),
  COUNT(chartmogul_invoice_uuid),
  COUNT(*) FILTER (WHERE metadata->>'chartmogul_subscription_invoice_uuid' IS NOT NULL)
FROM subscriptions
UNION ALL
SELECT 
  'invoices',
  COUNT(*),
  COUNT(chartmogul_uuid),
  COUNT(*) FILTER (WHERE metadata->>'chartmogul_invoice_uuid' IS NOT NULL)
FROM invoices;
"

echo ""
echo "================================================"
echo "✅ Migration completed successfully!"
echo "================================================"
echo ""
echo "Backup saved to: $BACKUP_FILE"
echo ""
echo "Next steps:"
echo "  1. Restart your application:"
echo "     docker compose restart"
echo ""
echo "  2. Monitor logs for any issues:"
echo "     docker compose logs -f --tail=100"
echo ""
echo "  3. Test ChartMogul sync operations"
echo ""
echo "If you need to rollback:"
echo "  docker compose exec -T postgres psql -U flexprice -d flexprice < migrations/postgres/V3__add_chartmogul_uuid_columns.down.sql"
echo ""
