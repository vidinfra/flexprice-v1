#!/bin/bash
set -e

# FlexPrice Database Restore Script
# Usage: ./restore.sh postgres <backup_file>
# Usage: ./restore.sh clickhouse <backup_file>

BACKUP_DIR="/opt/flexprice/backups"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

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

restore_postgres() {
    local BACKUP_FILE=$1

    if [ -z "$BACKUP_FILE" ]; then
        echo "Available PostgreSQL backups:"
        ls -lh $BACKUP_DIR/postgres/*.sql.gz 2>/dev/null || echo "No backups found"
        echo ""
        echo "Usage: ./restore.sh postgres <backup_file>"
        exit 1
    fi

    if [ ! -f "$BACKUP_FILE" ]; then
        echo -e "${RED}Backup file not found: $BACKUP_FILE${NC}"
        exit 1
    fi

    echo -e "${YELLOW}WARNING: This will overwrite the current database!${NC}"
    read -p "Are you sure? (yes/no): " confirm
    if [ "$confirm" != "yes" ]; then
        echo "Aborted"
        exit 0
    fi

    echo -e "${YELLOW}Stopping FlexPrice services...${NC}"
    systemctl stop flexprice-api flexprice-consumer flexprice-worker 2>/dev/null || true

    echo -e "${YELLOW}Restoring PostgreSQL from $BACKUP_FILE...${NC}"

    # Drop and recreate database
    sudo -u postgres psql -c "DROP DATABASE IF EXISTS $PG_DB;"
    sudo -u postgres psql -c "CREATE DATABASE $PG_DB OWNER $PG_USER;"

    # Restore
    gunzip -c $BACKUP_FILE | psql -h $PG_HOST -p $PG_PORT -U $PG_USER -d $PG_DB

    echo -e "${YELLOW}Starting FlexPrice services...${NC}"
    systemctl start flexprice-api flexprice-consumer flexprice-worker

    echo -e "${GREEN}PostgreSQL restore complete${NC}"
}

restore_clickhouse() {
    local BACKUP_FILE=$1

    if [ -z "$BACKUP_FILE" ]; then
        echo "Available ClickHouse backups:"
        ls -lh $BACKUP_DIR/clickhouse/*.tar.gz 2>/dev/null || echo "No backups found"
        echo ""
        echo "Usage: ./restore.sh clickhouse <backup_file>"
        exit 1
    fi

    if [ ! -f "$BACKUP_FILE" ]; then
        echo -e "${RED}Backup file not found: $BACKUP_FILE${NC}"
        exit 1
    fi

    echo -e "${YELLOW}WARNING: This will overwrite the current ClickHouse data!${NC}"
    read -p "Are you sure? (yes/no): " confirm
    if [ "$confirm" != "yes" ]; then
        echo "Aborted"
        exit 0
    fi

    echo -e "${YELLOW}Stopping FlexPrice services...${NC}"
    systemctl stop flexprice-consumer 2>/dev/null || true

    echo -e "${YELLOW}Restoring ClickHouse from $BACKUP_FILE...${NC}"

    # Extract backup
    TEMP_DIR=$(mktemp -d)
    tar -xzf $BACKUP_FILE -C $TEMP_DIR

    BACKUP_NAME=$(basename $BACKUP_FILE .tar.gz)

    # Restore each table
    for FILE in $TEMP_DIR/$BACKUP_NAME/*.native; do
        TABLE=$(basename $FILE .native)
        echo "  Restoring table: $TABLE"

        # Truncate and restore
        clickhouse-client --host=$CH_HOST --port=$CH_PORT --user=$CH_USER --password=$CH_PASSWORD \
            --query="TRUNCATE TABLE IF EXISTS $CH_DB.$TABLE"

        clickhouse-client --host=$CH_HOST --port=$CH_PORT --user=$CH_USER --password=$CH_PASSWORD \
            --query="INSERT INTO $CH_DB.$TABLE FORMAT Native" < $FILE
    done

    rm -rf $TEMP_DIR

    echo -e "${YELLOW}Starting FlexPrice services...${NC}"
    systemctl start flexprice-consumer

    echo -e "${GREEN}ClickHouse restore complete${NC}"
}

case "$1" in
    postgres)
        restore_postgres "$2"
        ;;
    clickhouse)
        restore_clickhouse "$2"
        ;;
    *)
        echo "FlexPrice Database Restore"
        echo ""
        echo "Usage:"
        echo "  ./restore.sh postgres <backup_file>   # Restore PostgreSQL"
        echo "  ./restore.sh clickhouse <backup_file> # Restore ClickHouse"
        echo ""
        echo "Examples:"
        echo "  ./restore.sh postgres /opt/flexprice/backups/postgres/flexprice_20260217.sql.gz"
        echo "  ./restore.sh clickhouse /opt/flexprice/backups/clickhouse/flexprice_20260217.tar.gz"
        ;;
esac
