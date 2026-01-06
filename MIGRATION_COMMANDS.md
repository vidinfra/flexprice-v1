# ChartMogul UUID Migration - Execution Commands

## Prerequisites

Before running the migration, ensure you have:
- Database credentials for local and staging environments
- Backup of the database
- Application stopped or in maintenance mode (recommended)

## Local Environment (Docker Compose)

### Option 1: Using docker compose exec (Recommended)

```bash
# Navigate to project root
cd /Users/tenbyte/Documents/TenByte/Services/flexprice

# Ensure Docker containers are running
docker compose up -d postgres

# Wait for postgres to be ready
sleep 5

# Create a backup first (from inside the container)
docker compose exec -T postgres pg_dump -U flexprice -d flexprice -F c > backup_before_migration_$(date +%Y%m%d_%H%M%S).dump

# Run the migration
docker compose exec -T postgres psql -U flexprice -d flexprice < migrations/postgres/V3__add_chartmogul_uuid_columns.up.sql

# Verify the migration
docker compose exec -T postgres psql -U flexprice -d flexprice -c "
SELECT 
  schemaname, 
  tablename, 
  indexname 
FROM pg_indexes 
WHERE indexname IN (
  'idx_customers_chartmogul_uuid',
  'idx_plans_chartmogul_uuid', 
  'idx_subscriptions_chartmogul_invoice_uuid',
  'idx_invoices_chartmogul_uuid'
);
"
```

### Option 2: Using psql from host to Docker container

```bash
# Navigate to project root
cd /Users/tenbyte/Documents/TenByte/Services/flexprice

# Ensure Docker containers are running
docker compose up -d postgres

# Database credentials from docker-compose.yml:
# Host: localhost
# Port: 5432
# User: flexprice
# Password: flexprice123
# Database: flexprice

# Set environment variable for password
export PGPASSWORD=flexprice123

# Create a backup first
pg_dump -h localhost -p 5432 -U flexprice -d flexprice -F c -f backup_before_migration_$(date +%Y%m%d_%H%M%S).dump

# Run the migration
psql -h localhost -p 5432 -U flexprice -d flexprice -f migrations/postgres/V3__add_chartmogul_uuid_columns.up.sql

# Verify the migration
psql -h localhost -p 5432 -U flexprice -d flexprice -c "
SELECT table_name, column_name, data_type 
FROM information_schema.columns 
WHERE table_schema = 'public' 
  AND column_name LIKE '%chartmogul%'
ORDER BY table_name, column_name;
"

# Unset password
unset PGPASSWORD
```

### Option 3: Copy file into container and execute

```bash
# Navigate to project root
cd /Users/tenbyte/Documents/TenByte/Services/flexprice

# Ensure Docker containers are running
docker compose up -d postgres

# The migration file is already accessible in the container via volume mount
# Run the migration directly
docker compose exec postgres psql -U flexprice -d flexprice -f /docker-entrypoint-initdb.d/migration/postgres/V3__add_chartmogul_uuid_columns.up.sql

# If the above doesn't work, copy the file explicitly
docker compose cp migrations/postgres/V3__add_chartmogul_uuid_columns.up.sql postgres:/tmp/migration.sql
docker compose exec postgres psql -U flexprice -d flexprice -f /tmp/migration.sql
```

### Option 4: Using Database URL with Docker

```bash
# Navigate to project root
cd /Users/tenbyte/Documents/TenByte/Services/flexprice

# Set database URL based on docker-compose.yml
export DATABASE_URL="postgresql://flexprice:flexprice123@localhost:5432/flexprice"

# Create a backup first
pg_dump $DATABASE_URL -F c -f backup_before_migration_$(date +%Y%m%d_%H%M%S).dump

# Run the migration
psql $DATABASE_URL -f migrations/postgres/V3__add_chartmogul_uuid_columns.up.sql

# Verify the migration
psql $DATABASE_URL -c "
SELECT table_name, column_name, data_type 
FROM information_schema.columns 
WHERE table_schema = 'public' 
  AND column_name LIKE '%chartmogul%'
ORDER BY table_name, column_name;
"
```

---

## Staging Environment

### Using psql with SSH Tunnel (if database is not directly accessible)

```bash
# Navigate to migrations directory
cd /Users/tenbyte/Documents/TenByte/Services/flexprice/migrations/postgres

# Step 1: Create SSH tunnel to staging database
ssh -N -L 5433:staging-db-host:5432 staging-bastion-host &
SSH_PID=$!

# Step 2: Set database connection (through tunnel)
export DB_HOST="localhost"
export DB_PORT="5433"
export DB_NAME="flexprice_staging"
export DB_USER="flexprice_user"
export DB_PASSWORD="staging_password"

# Step 3: Create backup
pg_dump -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -F c \
  -f backup_staging_before_migration_$(date +%Y%m%d_%H%M%S).dump

# Step 4: Verify backup
ls -lh backup_staging_before_migration_*.dump

# Step 5: Run the migration
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -f V3__add_chartmogul_uuid_columns.up.sql

# Step 6: Verify the migration
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME <<EOF
-- Check columns were created
SELECT table_name, column_name, data_type 
FROM information_schema.columns 
WHERE table_schema = 'public' 
  AND column_name LIKE '%chartmogul%'
ORDER BY table_name, column_name;

-- Check indexes were created
SELECT indexname, tablename 
FROM pg_indexes 
WHERE indexname LIKE '%chartmogul%';

-- Check data migration (sample)
SELECT COUNT(*) as total, 
       COUNT(chartmogul_uuid) as with_uuid
FROM customers;

SELECT COUNT(*) as total, 
       COUNT(chartmogul_uuid) as with_uuid
FROM plans;

SELECT COUNT(*) as total, 
       COUNT(chartmogul_invoice_uuid) as with_uuid
FROM subscriptions;

SELECT COUNT(*) as total, 
       COUNT(chartmogul_uuid) as with_uuid
FROM invoices;
EOF

# Step 7: Close SSH tunnel
kill $SSH_PID
```

### Using Direct Database Connection (if allowed)

```bash
# Navigate to migrations directory
cd /Users/tenbyte/Documents/TenByte/Services/flexprice/migrations/postgres

# Set staging database connection
export STAGING_DB_URL="postgresql://user:password@staging-db-host:5432/flexprice_staging?sslmode=require"

# Create backup
pg_dump $STAGING_DB_URL -F c -f backup_staging_$(date +%Y%m%d_%H%M%S).dump

# Run the migration
psql $STAGING_DB_URL -f V3__add_chartmogul_uuid_columns.up.sql

# Verify the migration
psql $STAGING_DB_URL -f - <<EOF
-- Verification queries
SELECT 'Columns created:' as check;
SELECT table_name, column_name 
FROM information_schema.columns 
WHERE column_name LIKE '%chartmogul%';

SELECT 'Indexes created:' as check;
SELECT indexname FROM pg_indexes WHERE indexname LIKE '%chartmogul%';

SELECT 'Data migrated:' as check;
SELECT 'customers' as table, COUNT(*) FILTER (WHERE chartmogul_uuid IS NOT NULL) as migrated_count FROM customers
UNION ALL
SELECT 'plans', COUNT(*) FILTER (WHERE chartmogul_uuid IS NOT NULL) FROM plans
UNION ALL
SELECT 'subscriptions', COUNT(*) FILTER (WHERE chartmogul_invoice_uuid IS NOT NULL) FROM subscriptions
UNION ALL
SELECT 'invoices', COUNT(*) FILTER (WHERE chartmogul_uuid IS NOT NULL) FROM invoices;
EOF
```

### Using AWS RDS (if on AWS)

```bash
# Navigate to migrations directory
cd /Users/tenbyte/Documents/TenByte/Services/flexprice/migrations/postgres

# Get database endpoint from AWS
export DB_ENDPOINT=$(aws rds describe-db-instances \
  --db-instance-identifier flexprice-staging \
  --query 'DBInstances[0].Endpoint.Address' \
  --output text)

# Set connection details
export DB_HOST=$DB_ENDPOINT
export DB_PORT="5432"
export DB_NAME="flexprice"
export DB_USER="flexprice_admin"
# Get password from AWS Secrets Manager or parameter store
export PGPASSWORD=$(aws secretsmanager get-secret-value \
  --secret-id staging/flexprice/db-password \
  --query SecretString \
  --output text)

# Create backup
pg_dump -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -F c \
  -f backup_staging_aws_$(date +%Y%m%d_%H%M%S).dump

# Run migration
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME \
  -f V3__add_chartmogul_uuid_columns.up.sql

# Verify migration
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME \
  -c "SELECT COUNT(*) FROM customers WHERE chartmogul_uuid IS NOT NULL;"
```

---

## Rollback Commands (If Needed)

### Local Environment Rollback (Docker)

```bash
# Navigate to project root
cd /Users/tenbyte/Documents/TenByte/Services/flexprice

# Option 1: Using docker compose exec
docker compose exec -T postgres psql -U flexprice -d flexprice < migrations/postgres/V3__add_chartmogul_uuid_columns.down.sql

# Option 2: Using psql from host
export PGPASSWORD=flexprice123
psql -h localhost -p 5432 -U flexprice -d flexprice -f migrations/postgres/V3__add_chartmogul_uuid_columns.down.sql
unset PGPASSWORD

# Verify rollback
docker compose exec -T postgres psql -U flexprice -d flexprice -c "
SELECT column_name 
FROM information_schema.columns 
WHERE table_schema = 'public' 
  AND column_name LIKE '%chartmogul%';
"
# Should return empty if rollback successful
```

### Staging Environment Rollback

```bash
cd /Users/tenbyte/Documents/TenByte/Services/flexprice/migrations/postgres

# Using database URL
export STAGING_DB_URL="postgresql://user:password@staging-db-host:5432/flexprice_staging?sslmode=require"

# Run rollback
psql $STAGING_DB_URL -f V3__add_chartmogul_uuid_columns.down.sql

# Verify rollback
psql $STAGING_DB_URL -c "
SELECT table_name, column_name 
FROM information_schema.columns 
WHERE column_name LIKE '%chartmogul%';
"
# Should return empty
```

---

## Post-Migration Application Restart

### Local Environment

```bash
# If using Docker Compose
cd /Users/tenbyte/Documents/TenByte/Services/flexprice
docker-compose restart

# If using local server
# Stop the server (Ctrl+C or kill process)
# Then start again
go run main.go

# Or if using air for hot reload
air
```

### Staging Environment

```bash
# Using Docker on EC2/VM
ssh staging-server
docker-compose -f docker-compose.staging.yml restart

# Using Kubernetes
kubectl rollout restart deployment/flexprice -n staging

# Using ECS
aws ecs update-service \
  --cluster flexprice-staging \
  --service flexprice-api \
  --force-new-deployment

# Using systemd
ssh staging-server
sudo systemctl restart flexprice
```

---

## Quick Validation Script

Create this script to quickly validate the migration:

```bash
#!/bin/bash
# validate_migration.sh

DB_URL=$1

if [ -z "$DB_URL" ]; then
  echo "Usage: ./validate_migration.sh <database_url>"
  exit 1
fi

echo "=== Validating ChartMogul UUID Migration ==="
echo ""

psql $DB_URL <<EOF
-- Check all columns exist
\echo '=== Checking Columns ==='
SELECT table_name, column_name, data_type 
FROM information_schema.columns 
WHERE table_schema = 'public' 
  AND column_name IN ('chartmogul_uuid', 'chartmogul_invoice_uuid')
ORDER BY table_name, column_name;

\echo ''
\echo '=== Checking Indexes ==='
SELECT indexname, tablename 
FROM pg_indexes 
WHERE indexname LIKE '%chartmogul%';

\echo ''
\echo '=== Checking Data Migration ==='
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
EOF

echo ""
echo "=== Validation Complete ==="
```

Make it executable and use it:

```bash
chmod +x validate_migration.sh

# Local
./validate_migration.sh "postgresql://postgres:password@localhost:5432/flexprice_local"

# Staging
./validate_migration.sh "postgresql://user:pass@staging-host:5432/flexprice_staging"
```

---

## Troubleshooting

### If migration fails midway:

```bash
# Check which tables were modified
psql $DATABASE_URL -c "
SELECT table_name, column_name 
FROM information_schema.columns 
WHERE column_name LIKE '%chartmogul%';
"

# Manually rollback if needed
psql $DATABASE_URL -f V3__add_chartmogul_uuid_columns.down.sql

# Or restore from backup
pg_restore -h localhost -p 5432 -U postgres -d flexprice_local backup_file.dump
```

### Check migration logs:

```bash
# Run migration with verbose output
psql $DATABASE_URL -f V3__add_chartmogul_uuid_columns.up.sql -e -v ON_ERROR_STOP=1
```

---

## Best Practices

1. **Always create a backup before migration**
2. **Test in local first, then staging, then production**
3. **Run validation queries after migration**
4. **Monitor application logs after deployment**
5. **Keep backup for at least 7 days**
6. **Document the migration execution time and any issues**
