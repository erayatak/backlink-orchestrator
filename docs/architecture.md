# Architecture

## Core Principles
1. **Zero State Orchestrator**: The orchestrator application holds zero in-memory state regarding jobs or tasks. The PostgreSQL database is the single source of truth.
2. **PostgreSQL as a Queue**: We use `SELECT ... FOR UPDATE SKIP LOCKED` to achieve high-concurrency queueing without needing an external message broker like Kafka or Redis.
3. **Pull-based Task Execution**: Workers pull tasks, rather than the orchestrator pushing tasks.

## Components
- **HTTP Server**: Serves the Worker API and Dashboard.
- **Task Claiming Engine**: Uses `SKIP LOCKED` to atomically assign tasks.
- **Recovery Background Loop**: Runs every 10 seconds to scan for dead workers (`last_heartbeat_at`) and expired tasks (`lease_until`), re-queueing them.
- **Admin Dashboard**: HTMX based SSR UI.
