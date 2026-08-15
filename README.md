# Backlink Orchestrator

The Backlink Orchestrator is the central brain for a highly distributed, zero-allocation web crawling infrastructure. It manages hundreds of ephemeral, pre-emptible worker instances parsing CommonCrawl WAT files.

## Features

- **High-Concurrency Task Queue**: Built on PostgreSQL `FOR UPDATE SKIP LOCKED`.
- **Worker Lifecycle Management**: Heartbeat monitoring, automatic eviction of dead workers.
- **Job Engine**: Create, Pause, Resume, and Cancel jobs with millions of tasks.
- **Idempotency & Resilience**: Built-in retry logic, strict schema constraints, and task reassignment.
- **Prometheus Metrics**: Real-time observability of throughput, queues, and latency.
- **Dashboard**: HTMX-powered low-overhead web dashboard for administration.

## Requirements

- Go 1.25.7 (Auto-installed by bootstrap)
- PostgreSQL 16+ (Auto-installed by bootstrap)
- Ubuntu/Debian x86_64

## Installation & Deployment

The Orchestrator includes a zero-touch, single-command installer for Ubuntu servers. 
Before running the installer, ensure your domain's DNS is pointing to the server's public IP.

### 1. FRESH INSTALL (First Time)
For the very first installation, you must fetch the installer directly from the GitHub repository to avoid circular dependencies (since your domain isn't serving it yet):

```bash
curl -fsSL https://raw.githubusercontent.com/erayatak/backlink-orchestrator/main/deploy/bootstrap.sh | sudo bash -s -- --domain orchestrator.example.com
```

This will safely provision PostgreSQL, Caddy, Go 1.25.7, create random secure credentials, build the repository, and start the systemd service. 
At the end of the installation, it will print your **Admin Password** and **Bootstrap Token**. Save them!

### 2. RERUN / UPDATE
If you need to update the application to the latest `main` commit, or if the initial installation was interrupted, simply re-run the same command (or fetch from your own domain once Caddy is up):

```bash
curl -fsSL https://orchestrator.example.com/bootstrap.sh | sudo bash -s -- --domain orchestrator.example.com
```
**Idempotency Guarantee**: Running the installer again is 100% safe. It will **preserve** your database, existing `.env` credentials, passwords, and sessions.

### 3. RESET / DESTRUCTIVE
If you want to completely destroy the current configuration and generate new credentials (WARNING: Does not drop the database data, but regenerates all app secrets):
```bash
curl -fsSL https://raw.githubusercontent.com/erayatak/backlink-orchestrator/main/deploy/bootstrap.sh | sudo bash -s -- --domain orchestrator.example.com --force-reset
```

## Architecture

See [docs/architecture.md](docs/architecture.md) for detailed architecture decisions.
See [docs/api.md](docs/api.md) for API contract.
