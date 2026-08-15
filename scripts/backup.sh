#!/usr/bin/env bash
set -e

# Load env variables
if [ -f "/opt/backlink-orchestrator/.env" ]; then
    source /opt/backlink-orchestrator/.env
fi

BACKUP_DIR="/var/backups/orchestrator"
mkdir -p "$BACKUP_DIR"

TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_FILE="$BACKUP_DIR/db_backup_$TIMESTAMP.sql.gz"

echo "Backing up database to $BACKUP_FILE..."

# Backup using pg_dump
pg_dump "$DATABASE_URL" | gzip > "$BACKUP_FILE"

# Keep last 7 days of backups
find "$BACKUP_DIR" -type f -name "db_backup_*.sql.gz" -mtime +7 -exec rm {} \;

echo "Backup complete."
