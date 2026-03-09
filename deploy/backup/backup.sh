#!/bin/bash
set -e

# FlexPrice Database Backup Script
# Usage: ./backup.sh [postgres|clickhouse|all]
# Add to crontab: 0 2 * * * /opt/flexprice/scripts/backup.sh all

BACKUP_DIR="/opt/flexprice/backups"
RETENTION_DAYS=7
DATE=$(date +%Y%m%d_%H%M%S)

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Create backup directory
mkdir -p $BACKUP_DIR/postgres
mkdir -p $BACKUP_DIR/clickhouse

# PostgreSQL credentials
PG_HOST="127.0.0.1"
PG_PORT="5432"
PG_USER="flexprice"
PG_DB="flexprice"
export PGPASSWORD="flexprice123"

# ClickHouse credentials
CH_HOST="127.0.0.1"
CH_PORT="9000"
CH_USER="flexprice"
CH_PASSWORD="flexprice123"
CH_DB="flexprice"

backup_postgres() {
    echo -e "${YELLOW}Backing up PostgreSQL...${NC}"

    BACKUP_FILE="$BACKUP_DIR/postgres/flexprice_${DATE}.sql.gz"

    pg_dump -h $PG_HOST -p $PG_PORT -U $PG_USER -d $PG_DB | gzip > $BACKUP_FILE

    SIZE=$(du -h $BACKUP_FILE | cut -f1)
    echo -e "${GREEN}PostgreSQL backup complete: $BACKUP_FILE ($SIZE)${NC}"
}

backup_clickhouse() {
    echo -e "${YELLOW}Backing up ClickHouse...${NC}"

    BACKUP_FILE="$BACKUP_DIR/clickhouse/flexprice_${DATE}"
    mkdir -p $BACKUP_FILE

    # Get list of tables
    TABLES=$(clickhouse-client --host=$CH_HOST --port=$CH_PORT --user=$CH_USER --password=$CH_PASSWORD \
        --query="SELECT name FROM system.tables WHERE database='$CH_DB'")

    for TABLE in $TABLES; do
        echo "  Backing up table: $TABLE"
        clickhouse-client --host=$CH_HOST --port=$CH_PORT --user=$CH_USER --password=$CH_PASSWORD \
            --query="SELECT * FROM $CH_DB.$TABLE FORMAT Native" > "$BACKUP_FILE/${TABLE}.native"
    done

    # Compress
    tar -czf "${BACKUP_FILE}.tar.gz" -C "$BACKUP_DIR/clickhouse" "flexprice_${DATE}"
    rm -rf $BACKUP_FILE

    SIZE=$(du -h "${BACKUP_FILE}.tar.gz" | cut -f1)
    echo -e "${GREEN}ClickHouse backup complete: ${BACKUP_FILE}.tar.gz ($SIZE)${NC}"
}

cleanup_old_backups() {
    echo -e "${YELLOW}Cleaning up backups older than $RETENTION_DAYS days...${NC}"

    find $BACKUP_DIR/postgres -name "*.sql.gz" -mtime +$RETENTION_DAYS -delete 2>/dev/null || true
    find $BACKUP_DIR/clickhouse -name "*.tar.gz" -mtime +$RETENTION_DAYS -delete 2>/dev/null || true

    echo -e "${GREEN}Cleanup complete${NC}"
}

show_status() {
    echo ""
    echo "========================================"
    echo "Backup Status"
    echo "========================================"
    echo ""
    echo "PostgreSQL backups:"
    ls -lh $BACKUP_DIR/postgres/*.sql.gz 2>/dev/null | tail -5 || echo "  No backups found"
    echo ""
    echo "ClickHouse backups:"
    ls -lh $BACKUP_DIR/clickhouse/*.tar.gz 2>/dev/null | tail -5 || echo "  No backups found"
    echo ""
    echo "Total backup size:"
    du -sh $BACKUP_DIR 2>/dev/null || echo "  N/A"
}

case "${1:-all}" in
    postgres)
        backup_postgres
        cleanup_old_backups
        ;;
    clickhouse)
        backup_clickhouse
        cleanup_old_backups
        ;;
    all)
        backup_postgres
        backup_clickhouse
        cleanup_old_backups
        ;;
    status)
        show_status
        ;;
    *)
        echo "Usage: ./backup.sh [postgres|clickhouse|all|status]"
        exit 1
        ;;
esac

show_status
