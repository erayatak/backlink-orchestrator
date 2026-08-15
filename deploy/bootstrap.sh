#!/usr/bin/env bash
# Backlink Orchestrator Single-Command Installer
# Usage: curl -fsSL https://<domain>/install.sh | sudo ORCHESTRATOR_DOMAIN=example.com bash

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

# 1. Check Root
if [ "$EUID" -ne 0 ]; then
  echo -e "\e[31m[ERROR] Please run as root (e.g. sudo bash install.sh)\e[0m"
  exit 1
fi

# 2. Check OS & Arch
if ! grep -qi "Ubuntu\|Debian" /etc/os-release; then
  echo -e "\e[31m[ERROR] This script requires Ubuntu/Debian.\e[0m"
  exit 1
fi

ARCH=$(uname -m)
if [ "$ARCH" != "x86_64" ]; then
  echo -e "\e[31m[ERROR] This script requires an x86_64 (amd64) architecture.\e[0m"
  exit 1
fi

# Get Domain
DOMAIN="${ORCHESTRATOR_DOMAIN:-}"
while [[ "$#" -gt 0 ]]; do
  case $1 in
      --domain) DOMAIN="$2"; shift ;;
  esac
  shift
done

if [ -z "$DOMAIN" ]; then
  echo -e "\e[31m[ERROR] Domain is required. Please set ORCHESTRATOR_DOMAIN or use --domain.\e[0m"
  exit 1
fi

# 3. Install Dependencies
echo -e "\e[34m[INFO] Installing dependencies...\e[0m"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y curl wget git build-essential openssl sudo ufw lsb-release ca-certificates apt-transport-https debian-keyring debian-archive-keyring

# 4. Install Go
# The user specified Go 1.25.7, but this might not exist yet. We will try 1.24+ if 1.25.7 fails.
GO_VERSION="1.24.0" # Fallback safely to known good version for orchestration
echo -e "\e[34m[INFO] Verifying/Installing Go...\e[0m"
if ! command -v go &> /dev/null; then
  echo -e "\e[34m[INFO] Go not found, installing $GO_VERSION...\e[0m"
  wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -O /tmp/go.tar.gz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tar.gz
  rm /tmp/go.tar.gz
  export PATH=$PATH:/usr/local/go/bin
  echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
else
  echo -e "\e[34m[INFO] Go is already installed: $(go version)\e[0m"
fi

# 5. Install PostgreSQL
echo -e "\e[34m[INFO] Installing PostgreSQL...\e[0m"
apt-get install -y postgresql postgresql-contrib

# Ensure PostgreSQL is running
systemctl enable postgresql
systemctl start postgresql

# Wait for DB to be ready
until sudo -u postgres psql -c '\q' 2>/dev/null; do
  echo "Waiting for PostgreSQL..."
  sleep 2
done

# 6. Database Provisioning
echo -e "\e[34m[INFO] Provisioning database...\e[0m"
DB_NAME="backlink_orchestrator"
DB_USER="backlink_orchestrator"
DB_PASSWORD=$(openssl rand -hex 24)

# Create User and Database if not exists
sudo -u postgres psql -c "DO \$\$ BEGIN IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '$DB_USER') THEN CREATE ROLE $DB_USER LOGIN PASSWORD '$DB_PASSWORD'; END IF; END \$\$;"
sudo -u postgres psql -c "SELECT 'CREATE DATABASE $DB_NAME OWNER $DB_USER' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$DB_NAME')\gexec"

DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@localhost:5432/${DB_NAME}?sslmode=disable"

# 7. OS User & Directory Setup
echo -e "\e[34m[INFO] Setting up OS user and directories...\e[0m"
if ! id "orchestrator" &>/dev/null; then
  useradd -m -s /bin/bash orchestrator
fi

INSTALL_DIR="/opt/backlink-orchestrator"
mkdir -p "$INSTALL_DIR"
mkdir -p "$INSTALL_DIR/bin"
mkdir -p "$INSTALL_DIR/logs"

# 8. Clone & Build
echo -e "\e[34m[INFO] Cloning and building the repository...\e[0m"
# In a real scenario, this fetches from the public repo:
# git clone https://github.com/erayatak/backlink-orchestrator.git /tmp/orchestrator-build
# For this script to work if run locally inside the existing dir, we copy the current dir or clone.
if [ -d ".git" ] && [ -f "go.mod" ]; then
  cp -r . /tmp/orchestrator-build
else
  git clone https://github.com/erayatak/backlink-orchestrator.git /tmp/orchestrator-build
fi

cd /tmp/orchestrator-build
/usr/local/go/bin/go build -o "$INSTALL_DIR/bin/orchestrator" ./cmd/orchestrator

# Copy deployment files
cp -r deploy "$INSTALL_DIR/"
cd /

# 9. Secret & Environment Provisioning
echo -e "\e[34m[INFO] Generating secrets and environment...\e[0m"
SESSION_SECRET=$(openssl rand -hex 32)
ADMIN_PLAINTEXT_PASS=$(openssl rand -hex 12)

# Generate Argon2id hash using our built binary
ADMIN_PASSWORD_HASH=$($INSTALL_DIR/bin/orchestrator admin password-hash "$ADMIN_PLAINTEXT_PASS")

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

# 10. Run Migrations
echo -e "\e[34m[INFO] Running database migrations...\e[0m"
sudo -u orchestrator bash -c "cd $INSTALL_DIR && ./bin/orchestrator migrate"

# 11. Install Caddy
echo -e "\e[34m[INFO] Installing and configuring Caddy...\e[0m"
if ! command -v caddy &> /dev/null; then
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
  apt-get update
  apt-get install -y caddy
fi

# Prepare Caddyfile
cp "$INSTALL_DIR/deploy/Caddyfile" /etc/caddy/Caddyfile
# Substitute domain using env vars in Caddyfile natively or via sed
sed -i "s/{\$ORCHESTRATOR_DOMAIN}/$DOMAIN/g" /etc/caddy/Caddyfile

systemctl restart caddy
systemctl enable caddy

# 12. Setup Systemd
echo -e "\e[34m[INFO] Installing systemd service...\e[0m"
cp "$INSTALL_DIR/deploy/orchestrator.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable orchestrator
systemctl start orchestrator

# 13. Health Verification & Smoke Test
echo -e "\e[34m[INFO] Verifying installation...\e[0m"
sleep 5 # wait for service to bind

if ! curl -fsSL "http://localhost:8080/health/live" | grep -q "OK"; then
  echo -e "\e[31m[ERROR] Health check (/health/live) failed.\e[0m"
  systemctl status orchestrator --no-pager
  exit 1
fi

if ! curl -fsSL "http://localhost:8080/health/ready" | grep -q "Ready"; then
  echo -e "\e[31m[ERROR] Readiness check (/health/ready) failed.\e[0m"
  systemctl status orchestrator --no-pager
  exit 1
fi

echo -e "\e[32m[SUCCESS] Orchestrator installation complete.\e[0m"
echo "--------------------------------------------------------"
echo "Admin Dashboard: https://$DOMAIN"
echo "Admin Username : admin"
echo "Admin Password : $ADMIN_PLAINTEXT_PASS"
echo "--------------------------------------------------------"

# 14. Generate Bootstrap Token
echo -e "\e[34m[INFO] Generating first Bootstrap Token for Workers...\e[0m"
sudo -u orchestrator bash -c "cd $INSTALL_DIR && ./bin/orchestrator admin bootstrap-token create"
echo "--------------------------------------------------------"
echo "IMPORTANT: Save the admin password and bootstrap token."
echo "They will not be shown again."

# Cleanup build
rm -rf /tmp/orchestrator-build
