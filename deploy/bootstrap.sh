#!/usr/bin/env bash
set -e

echo "Bootstrapping Backlink Orchestrator server..."

# Assuming Ubuntu/Debian
sudo apt-get update
sudo apt-get install -y postgresql caddy

# Set up orchestrator user
sudo useradd -m -s /bin/bash orchestrator || true

# Set up directory
sudo mkdir -p /opt/backlink-orchestrator
sudo chown orchestrator:orchestrator /opt/backlink-orchestrator

# Setup systemd service
sudo cp deploy/orchestrator.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable orchestrator

# Setup Caddyfile
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo systemctl restart caddy

echo "Bootstrap complete. Please configure /opt/backlink-orchestrator/.env and run deploy.sh."
