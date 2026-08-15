#!/usr/bin/env bash
set -e

echo "Deploying Backlink Orchestrator..."

# Build
GOOS=linux GOARCH=amd64 go build -o orchestrator ./cmd/orchestrator

# Copy to destination
sudo cp orchestrator /opt/backlink-orchestrator/
sudo chown orchestrator:orchestrator /opt/backlink-orchestrator/orchestrator
sudo chmod +x /opt/backlink-orchestrator/orchestrator

# Restart service
sudo systemctl restart orchestrator

echo "Deployment complete."
