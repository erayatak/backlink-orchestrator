#!/usr/bin/env bash
# Integration test for bootstrap idempotency
# This should be run on a disposable Ubuntu environment or Docker container.

set -euo pipefail

echo -e "\e[34m[TEST] Starting bootstrap idempotency test...\e[0m"

# Ensure we are root
if [ "$EUID" -ne 0 ]; then
  echo -e "\e[31m[ERROR] Test must run as root.\e[0m"
  exit 1
fi

DOMAIN="test.orchestrator.local"
INSTALL_DIR="/opt/backlink-orchestrator"
BOOTSTRAP_SCRIPT="$(pwd)/deploy/bootstrap.sh"

if [ ! -f "$BOOTSTRAP_SCRIPT" ]; then
    echo -e "\e[31m[ERROR] bootstrap.sh not found at $BOOTSTRAP_SCRIPT\e[0m"
    exit 1
fi

# Cleanup previous test if any
rm -rf "$INSTALL_DIR"
# Drop DB and Role if they exist
sudo -u postgres psql -c "DROP DATABASE IF EXISTS backlink_orchestrator;" 2>/dev/null || true
sudo -u postgres psql -c "DROP ROLE IF EXISTS backlink_orchestrator;" 2>/dev/null || true

# --- RUN 1: FRESH INSTALL ---
echo -e "\e[34m[TEST] RUN 1: Fresh Installation\e[0m"
bash "$BOOTSTRAP_SCRIPT" --domain "$DOMAIN"

if [ ! -f "$INSTALL_DIR/.env" ]; then
    echo -e "\e[31m[FAIL] RUN 1: .env was not created\e[0m"
    exit 1
fi

# Capture state after Run 1
source "$INSTALL_DIR/.env"
RUN1_SESSION_SECRET="$SESSION_SECRET"
RUN1_ADMIN_HASH="$ADMIN_PASSWORD_HASH"
RUN1_DB_URL="$DATABASE_URL"

# Verify DB connectivity
if ! sudo -u orchestrator "$INSTALL_DIR/bin/orchestrator" migrate; then
    echo -e "\e[31m[FAIL] RUN 1: DB Migration failed, database might not be reachable.\e[0m"
    exit 1
fi

echo -e "\e[32m[PASS] RUN 1 verified.\e[0m"

# --- RUN 2: RERUN ---
echo -e "\e[34m[TEST] RUN 2: Rerun Installation\e[0m"
bash "$BOOTSTRAP_SCRIPT" --domain "$DOMAIN"

# Capture state after Run 2
unset SESSION_SECRET
unset ADMIN_PASSWORD_HASH
unset DATABASE_URL
source "$INSTALL_DIR/.env"
RUN2_SESSION_SECRET="$SESSION_SECRET"
RUN2_ADMIN_HASH="$ADMIN_PASSWORD_HASH"
RUN2_DB_URL="$DATABASE_URL"

# Assertions
if [ "$RUN1_SESSION_SECRET" != "$RUN2_SESSION_SECRET" ]; then
    echo -e "\e[31m[FAIL] SESSION_SECRET changed during rerun!\e[0m"
    exit 1
fi

if [ "$RUN1_ADMIN_HASH" != "$RUN2_ADMIN_HASH" ]; then
    echo -e "\e[31m[FAIL] ADMIN_PASSWORD_HASH changed during rerun!\e[0m"
    exit 1
fi

if [ "$RUN1_DB_URL" != "$RUN2_DB_URL" ]; then
    echo -e "\e[31m[FAIL] DATABASE_URL changed during rerun!\e[0m"
    exit 1
fi

echo -e "\e[32m[PASS] RUN 2 idempotency verified. Credentials preserved.\e[0m"

# --- RUN 3: FORCE RESET ---
echo -e "\e[34m[TEST] RUN 3: Force Reset\e[0m"
bash "$BOOTSTRAP_SCRIPT" --domain "$DOMAIN" --force-reset

# Capture state after Run 3
unset SESSION_SECRET
unset ADMIN_PASSWORD_HASH
unset DATABASE_URL
source "$INSTALL_DIR/.env"
RUN3_SESSION_SECRET="$SESSION_SECRET"

if [ "$RUN1_SESSION_SECRET" == "$RUN3_SESSION_SECRET" ]; then
    echo -e "\e[31m[FAIL] SESSION_SECRET did NOT change after --force-reset!\e[0m"
    exit 1
fi

echo -e "\e[32m[PASS] RUN 3 force reset verified. New credentials generated.\e[0m"

echo -e "\e[32m[SUCCESS] All idempotency tests passed.\e[0m"
