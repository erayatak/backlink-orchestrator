#!/usr/bin/env bash
set -euo pipefail

echo -e "\e[34m[INFO] Deploying Backlink Orchestrator update...\e[0m"

INSTALL_DIR="/opt/backlink-orchestrator"
BIN_DIR="$INSTALL_DIR/bin"
BACKUP_BIN="$BIN_DIR/orchestrator.backup"
TARGET_BIN="$BIN_DIR/orchestrator"

# 1. Build new binary
echo -e "\e[34m[INFO] Building new binary...\e[0m"
GOOS=linux GOARCH=amd64 go build -o orchestrator_new ./cmd/orchestrator

# 2. Run migrations with new binary BEFORE touching service
echo -e "\e[34m[INFO] Running migrations...\e[0m"
chmod +x orchestrator_new
# Export env vars for migration from the production env file
set -a
source "$INSTALL_DIR/.env"
set +a

if ! ./orchestrator_new migrate; then
    echo -e "\e[31m[ERROR] Migrations failed. Deployment aborted. Service was not restarted.\e[0m"
    rm orchestrator_new
    exit 1
fi

# 3. Backup and Install
echo -e "\e[34m[INFO] Installing new binary...\e[0m"
if [ -f "$TARGET_BIN" ]; then
    cp "$TARGET_BIN" "$BACKUP_BIN"
fi
mv orchestrator_new "$TARGET_BIN"
chown orchestrator:orchestrator "$TARGET_BIN"
chmod +x "$TARGET_BIN"

# 4. Restart service
echo -e "\e[34m[INFO] Restarting orchestrator service...\e[0m"
systemctl restart orchestrator

# 5. Health Verification Polling
echo -e "\e[34m[INFO] Waiting for service to come up (timeout 60s)...\e[0m"
HEALTH_LIVE=0
HEALTH_READY=0

for i in {1..12}; do
    if curl -fsSL "http://localhost:8080/health/live" | grep -q "OK" 2>/dev/null; then
        HEALTH_LIVE=1
    fi
    if curl -fsSL "http://localhost:8080/health/ready" | grep -q "Ready" 2>/dev/null; then
        HEALTH_READY=1
    fi

    if [ "$HEALTH_LIVE" -eq 1 ] && [ "$HEALTH_READY" -eq 1 ]; then
        break
    fi
    sleep 5
done

if [ "$HEALTH_LIVE" -eq 0 ] || [ "$HEALTH_READY" -eq 0 ]; then
    echo -e "\e[31m[ERROR] Health check failed after deployment! Rolling back...\e[0m"
    if [ -f "$BACKUP_BIN" ]; then
        mv "$BACKUP_BIN" "$TARGET_BIN"
        systemctl restart orchestrator
        
        echo -e "\e[34m[INFO] Rollback initiated. Verifying rollback health (timeout 30s)...\e[0m"
        RB_HEALTH_LIVE=0
        RB_HEALTH_READY=0
        for i in {1..6}; do
            if curl -fsSL "http://localhost:8080/health/live" | grep -q "OK" 2>/dev/null; then RB_HEALTH_LIVE=1; fi
            if curl -fsSL "http://localhost:8080/health/ready" | grep -q "Ready" 2>/dev/null; then RB_HEALTH_READY=1; fi
            if [ "$RB_HEALTH_LIVE" -eq 1 ] && [ "$RB_HEALTH_READY" -eq 1 ]; then break; fi
            sleep 5
        done

        if [ "$RB_HEALTH_LIVE" -eq 0 ] || [ "$RB_HEALTH_READY" -eq 0 ]; then
            echo -e "\e[31m[ERROR] Rollback failed to become healthy. Manual intervention required.\e[0m"
            exit 1
        else
            echo -e "\e[33m[WARN] Rollback complete and healthy. Previous version restored.\e[0m"
            exit 1
        fi
    else
        echo -e "\e[31m[ERROR] No backup binary found for rollback.\e[0m"
        exit 1
    fi
fi

echo -e "\e[32m[SUCCESS] Deployment complete and health verified.\e[0m"
