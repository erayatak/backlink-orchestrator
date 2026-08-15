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

- Go 1.24+
- PostgreSQL 18+

## Architecture

See [docs/architecture.md](docs/architecture.md) for detailed architecture decisions.
See [docs/api.md](docs/api.md) for API contract.
