#!/usr/bin/env bash
set -e

if [ -z "$1" ]; then
    echo "Usage: ./restore.sh <backup_file.sql.gz>"
    exit 1
fi

BACKUP_FILE="$1"

if [ ! -f "$BACKUP_FILE" ]; then
    echo "Backup file not found!"
    exit 1
fi

# Load env variables
if [ -f "/opt/backlink-orchestrator/.env" ]; then
    source /opt/backlink-orchestrator/.env
fi

echo "Restoring database from $BACKUP_FILE..."

# Stop orchestrator to prevent writes during restore
sudo systemctl stop orchestrator

# Restore using psql
gunzip -c "$BACKUP_FILE" | psql "$DATABASE_URL"

# Restart orchestrator
sudo systemctl start orchestrator

echo "Restore complete."
