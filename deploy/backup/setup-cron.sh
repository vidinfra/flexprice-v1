#!/bin/bash

# Setup nightly backup cron job
# Run: sudo ./setup-cron.sh

SCRIPT_DIR="/opt/flexprice/scripts"
BACKUP_SCRIPT="$SCRIPT_DIR/backup.sh"

# Copy backup scripts to server
mkdir -p $SCRIPT_DIR
cp backup.sh restore.sh $SCRIPT_DIR/
chmod +x $SCRIPT_DIR/*.sh

# Add cron job - runs at 2 AM every night
CRON_JOB="0 2 * * * $BACKUP_SCRIPT all >> /var/log/flexprice-backup.log 2>&1"

# Check if cron job already exists
if crontab -l 2>/dev/null | grep -q "backup.sh"; then
    echo "Cron job already exists"
else
    (crontab -l 2>/dev/null; echo "$CRON_JOB") | crontab -
    echo "Cron job added: Daily backup at 2 AM"
fi

echo ""
echo "Current cron jobs:"
crontab -l

echo ""
echo "Backup logs: /var/log/flexprice-backup.log"
echo "Backups stored: /opt/flexprice/backups/"
