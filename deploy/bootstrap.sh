#!/usr/bin/env bash
# Backlink Orchestrator Single-Command Installer
#
# Usage:
# curl -fsSL https://raw.githubusercontent.com/erayatak/backlink-orchestrator/main/deploy/bootstrap.sh | sudo bash -s -- --domain example.com

set -Eeuo pipefail

trap cleanup SIGINT SIGTERM ERR EXIT

cleanup() {
  trap - SIGINT SIGTERM ERR EXIT
  local exit_code=$?
  if [ $exit_code -ne 0 ]; then
    echo -e "\e[31m[ERROR] Orchestrator installation failed.\e[0m"
  fi
  exit $exit_code
}

echo -e "\e[34m[INFO] Starting Backlink Orchestrator installation...\e[0m"

# 1. Parse Arguments
DOMAIN="${ORCHESTRATOR_DOMAIN:-}"
FORCE_RESET=0
COMMIT_REF="main"

while [[ "$#" -gt 0 ]]; do
  case $1 in
      --domain) DOMAIN="$2"; shift ;;
      --force-reset) FORCE_RESET=1 ;;
      --ref) COMMIT_REF="$2"; shift ;;
  esac
  shift
done

# 2. Check Root
if [ "$EUID" -ne 0 ]; then
  echo -e "\e[31m[ERROR] Please run as root (e.g. sudo bash bootstrap.sh)\e[0m"
  exit 1
fi

# 3. Check OS & Arch
if ! grep -qi "Ubuntu\|Debian" /etc/os-release 2>/dev/null; then
  echo -e "\e[31m[ERROR] This script requires Ubuntu/Debian.\e[0m"
  exit 1
fi

ARCH=$(uname -m)
if [ "$ARCH" != "x86_64" ]; then
  echo -e "\e[31m[ERROR] This script requires an x86_64 (amd64) architecture.\e[0m"
  exit 1
fi

if [ -z "$DOMAIN" ]; then
  echo -e "\e[31m[ERROR] Domain is required. Please set ORCHESTRATOR_DOMAIN or use --domain.\e[0m"
  exit 1
fi

INSTALL_DIR="/opt/backlink-orchestrator"

# Handle --force-reset
if [ "$FORCE_RESET" -eq 1 ]; then
  echo -e "\e[31m[WARN] Force reset initiated. Destroying existing configuration...\e[0m"
  rm -f "$INSTALL_DIR/.env"
fi

# Load existing .env if present (Idempotency)
if [ -f "$INSTALL_DIR/.env" ]; then
  echo -e "\e[34m[INFO] Existing installation found. Preserving credentials...\e[0m"
  set -a
  source "$INSTALL_DIR/.env"
  set +a
  IS_RERUN=1
else
  echo -e "\e[34m[INFO] Fresh installation mode.\e[0m"
  IS_RERUN=0
fi

# 4. Install Dependencies
echo -e "\e[34m[INFO] Installing dependencies...\e[0m"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y curl wget git build-essential openssl sudo ufw lsb-release ca-certificates apt-transport-https debian-keyring debian-archive-keyring postgresql postgresql-contrib

# 5. Install Go
GO_VERSION="1.25.7"
echo -e "\e[34m[INFO] Verifying/Installing Go $GO_VERSION...\e[0m"
if command -v go &> /dev/null && go version | grep -q "$GO_VERSION"; then
  echo -e "\e[34m[INFO] Go $GO_VERSION is already installed.\e[0m"
else
  echo -e "\e[34m[INFO] Installing Go $GO_VERSION...\e[0m"
  wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz || {
      echo -e "\e[31m[ERROR] Failed to download Go $GO_VERSION. Verify version exists.\e[0m"
      exit 1
  }
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tar.gz
  rm /tmp/go.tar.gz
  export PATH=$PATH:/usr/local/go/bin
  echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
fi

if ! command -v go &> /dev/null; then
  export PATH=$PATH:/usr/local/go/bin
fi

# 6. Database Provisioning
echo -e "\e[34m[INFO] Provisioning database...\e[0m"
systemctl enable postgresql
systemctl start postgresql

until sudo -u postgres psql -c '\q' 2>/dev/null; do
  echo "Waiting for PostgreSQL..."
  sleep 2
done

DB_NAME="backlink_orchestrator"
DB_USER="backlink_orchestrator"

# Idempotent Role Creation
if sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='$DB_USER'" | grep -q 1; then
  echo -e "\e[34m[INFO] PostgreSQL role '$DB_USER' already exists. Password unchanged.\e[0m"
else
  echo -e "\e[34m[INFO] Creating PostgreSQL role '$DB_USER'...\e[0m"
  NEW_DB_PASSWORD=$(openssl rand -hex 24)
  sudo -u postgres psql -c "CREATE ROLE $DB_USER LOGIN PASSWORD '$NEW_DB_PASSWORD';"
  export DATABASE_URL="postgres://${DB_USER}:${NEW_DB_PASSWORD}@localhost:5432/${DB_NAME}?sslmode=disable"
fi

if sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='$DB_NAME'" | grep -q 1; then
  echo -e "\e[34m[INFO] PostgreSQL database '$DB_NAME' already exists.\e[0m"
else
  echo -e "\e[34m[INFO] Creating PostgreSQL database '$DB_NAME'...\e[0m"
  sudo -u postgres psql -c "CREATE DATABASE $DB_NAME OWNER $DB_USER;"
fi

# 7. OS User & Directory Setup
echo -e "\e[34m[INFO] Setting up OS user and directories...\e[0m"
if ! id "orchestrator" &>/dev/null; then
  useradd -m -s /bin/bash orchestrator
fi

mkdir -p "$INSTALL_DIR/bin" "$INSTALL_DIR/logs"

# 8. Clone & Build
echo -e "\e[34m[INFO] Cloning repository ($COMMIT_REF)...\e[0m"
rm -rf /tmp/orchestrator-build
git clone -b "$COMMIT_REF" https://github.com/erayatak/backlink-orchestrator.git /tmp/orchestrator-build

cd /tmp/orchestrator-build
CURRENT_SHA=$(git rev-parse HEAD)
echo -e "\e[34m[INFO] Building from commit: $CURRENT_SHA\e[0m"

go build -o "$INSTALL_DIR/bin/orchestrator" ./cmd/orchestrator
cp -r deploy "$INSTALL_DIR/"
cd /

# 9. Secret & Environment Provisioning
echo -e "\e[34m[INFO] Configuring environment...\e[0m"

if [ "$IS_RERUN" -eq 0 ] || [ "$FORCE_RESET" -eq 1 ]; then
  SESSION_SECRET=$(openssl rand -hex 32)
  ADMIN_PLAINTEXT_PASS=$(openssl rand -hex 12)
  ADMIN_PASSWORD_HASH=$("$INSTALL_DIR/bin/orchestrator" admin password-hash "$ADMIN_PLAINTEXT_PASS")

  cat > "$INSTALL_DIR/.env" <<EOF
APP_ENV=production
APP_PORT=8080
PUBLIC_BASE_URL=https://$DOMAIN
DATABASE_URL=$DATABASE_URL
SESSION_SECRET=$SESSION_SECRET
ADMIN_PASSWORD_HASH=$ADMIN_PASSWORD_HASH
ORCHESTRATOR_DOMAIN=$DOMAIN
EOF
  chown -R orchestrator:orchestrator "$INSTALL_DIR"
  chmod 600 "$INSTALL_DIR/.env"
  echo -e "\e[32m[SUCCESS] New credentials generated.\e[0m"
else
  echo -e "\e[34m[INFO] Using existing .env secrets.\e[0m"
  sed -i "s/^ORCHESTRATOR_DOMAIN=.*/ORCHESTRATOR_DOMAIN=$DOMAIN/" "$INSTALL_DIR/.env"
  sed -i "s|^PUBLIC_BASE_URL=.*|PUBLIC_BASE_URL=https://$DOMAIN|" "$INSTALL_DIR/.env"
fi

# 10. Run Migrations
echo -e "\e[34m[INFO] Running database migrations...\e[0m"
if ! sudo -u orchestrator bash -c "cd $INSTALL_DIR && ./bin/orchestrator migrate"; then
  echo -e "\e[31m[ERROR] Database migrations failed.\e[0m"
  exit 1
fi

# 11. Install Caddy
echo -e "\e[34m[INFO] Installing and configuring Caddy...\e[0m"
if ! command -v caddy &> /dev/null; then
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
  apt-get update
  apt-get install -y caddy
fi

cp "$INSTALL_DIR/deploy/Caddyfile" /etc/caddy/Caddyfile
sed -i "s/{\$ORCHESTRATOR_DOMAIN}/$DOMAIN/g" /etc/caddy/Caddyfile
systemctl restart caddy
systemctl enable caddy

# 12. Setup Systemd
echo -e "\e[34m[INFO] Configuring systemd...\e[0m"
cp "$INSTALL_DIR/deploy/orchestrator.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable orchestrator
systemctl restart orchestrator

# 13. Health Verification Polling
echo -e "\e[34m[INFO] Verifying health checks (timeout 60s)...\e[0m"
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
  echo -e "\e[31m[ERROR] Orchestrator failed to become healthy.\e[0m"
  systemctl status orchestrator --no-pager
  exit 1
fi

# 14. Comprehensive Final Check
echo -e "\e[34m[INFO] Performing comprehensive checks...\e[0m"
systemctl is-active --quiet postgresql || { echo -e "\e[31m[ERROR] PostgreSQL not active.\e[0m"; exit 1; }
systemctl is-active --quiet caddy || { echo -e "\e[31m[ERROR] Caddy not active.\e[0m"; exit 1; }
systemctl is-active --quiet orchestrator || { echo -e "\e[31m[ERROR] Orchestrator not active.\e[0m"; exit 1; }

echo -e "\e[32m[SUCCESS] Orchestrator installation complete.\e[0m"
echo "--------------------------------------------------------"
if [ "$IS_RERUN" -eq 0 ]; then
  echo "Admin Dashboard: https://$DOMAIN"
  echo "Admin Username : admin"
  echo "Admin Password : $ADMIN_PLAINTEXT_PASS"
  echo "--------------------------------------------------------"
  echo -e "\e[34m[INFO] Generating first Bootstrap Token for Workers...\e[0m"
  sudo -u orchestrator bash -c "cd $INSTALL_DIR && ./bin/orchestrator admin bootstrap-token create"
  echo "--------------------------------------------------------"
  echo "IMPORTANT: Save the admin password and bootstrap token."
  echo "They will not be shown again."
else
  echo "Admin Dashboard: https://$DOMAIN"
  echo "Existing credentials and tokens have been preserved."
  echo "--------------------------------------------------------"
fi

rm -rf /tmp/orchestrator-build
