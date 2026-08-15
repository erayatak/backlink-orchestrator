-- +goose Up
-- +goose StatementBegin
CREATE TABLE workers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    worker_id VARCHAR NOT NULL UNIQUE,
    status VARCHAR NOT NULL,
    hostname VARCHAR NOT NULL,
    os VARCHAR NOT NULL,
    architecture VARCHAR NOT NULL,
    cpu_count INT NOT NULL,
    memory_mb INT NOT NULL,
    version VARCHAR NOT NULL,
    token_hash VARCHAR NOT NULL,
    current_task_id UUID,
    last_heartbeat_at TIMESTAMPTZ NOT NULL,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disabled_at TIMESTAMPTZ
);

CREATE INDEX idx_workers_status ON workers(status);
CREATE INDEX idx_workers_last_heartbeat_at ON workers(last_heartbeat_at);

CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id VARCHAR NOT NULL UNIQUE,
    dataset VARCHAR NOT NULL,
    crawl_id VARCHAR NOT NULL,
    status VARCHAR NOT NULL,
    total_tasks INT NOT NULL DEFAULT 0,
    queued_tasks INT NOT NULL DEFAULT 0,
    running_tasks INT NOT NULL DEFAULT 0,
    succeeded_tasks INT NOT NULL DEFAULT 0,
    failed_tasks INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (dataset, crawl_id)
);

CREATE INDEX idx_jobs_status ON jobs(status);

CREATE TABLE tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id VARCHAR NOT NULL UNIQUE,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    dataset VARCHAR NOT NULL,
    source_path VARCHAR NOT NULL,
    status VARCHAR NOT NULL,
    assigned_worker_id VARCHAR,
    current_attempt INT NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    output_uri VARCHAR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, source_path)
);

CREATE INDEX idx_tasks_job_id_status ON tasks(job_id, status);
CREATE INDEX idx_tasks_status_lease_until ON tasks(status, lease_until);
CREATE INDEX idx_tasks_assigned_worker_id ON tasks(assigned_worker_id);
-- Index for queue claim operation as defined in Briefing.md
CREATE INDEX idx_tasks_queue_claim ON tasks(status, lease_until, created_at);

CREATE TABLE task_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    worker_id VARCHAR NOT NULL,
    attempt_number INT NOT NULL,
    status VARCHAR NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    lease_until TIMESTAMPTZ,
    processed_bytes BIGINT DEFAULT 0,
    processed_records INT DEFAULT 0,
    processed_links INT DEFAULT 0,
    output_uri VARCHAR,
    error_code VARCHAR,
    error_message TEXT,
    UNIQUE (task_id, attempt_number)
);

CREATE INDEX idx_task_attempts_task_id ON task_attempts(task_id);
CREATE INDEX idx_task_attempts_worker_id ON task_attempts(worker_id);
CREATE INDEX idx_task_attempts_status ON task_attempts(status);

CREATE TABLE worker_heartbeat_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    worker_id VARCHAR NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status VARCHAR NOT NULL,
    cpu_percent FLOAT NOT NULL,
    memory_percent FLOAT NOT NULL,
    current_task_id UUID,
    processed_records INT NOT NULL DEFAULT 0,
    processed_links INT NOT NULL DEFAULT 0
);

CREATE INDEX idx_heartbeat_history_worker_time ON worker_heartbeat_history(worker_id, recorded_at);

CREATE TABLE system_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    severity VARCHAR NOT NULL,
    event_type VARCHAR NOT NULL,
    worker_id VARCHAR,
    task_id UUID,
    job_id UUID,
    message TEXT NOT NULL,
    metadata_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_events_created_at ON system_events(created_at);
CREATE INDEX idx_events_severity ON system_events(severity);
CREATE INDEX idx_events_worker_id ON system_events(worker_id);
CREATE INDEX idx_events_task_id ON system_events(task_id);

CREATE TABLE admin_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id VARCHAR NOT NULL UNIQUE,
    user_identifier VARCHAR NOT NULL,
    token_hash VARCHAR NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE bootstrap_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash VARCHAR NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_by VARCHAR NOT NULL,
    status VARCHAR NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE bootstrap_tokens;
DROP TABLE admin_sessions;
DROP TABLE system_events;
DROP TABLE worker_heartbeat_history;
DROP TABLE task_attempts;
DROP TABLE tasks;
DROP TABLE jobs;
DROP TABLE workers;
-- +goose StatementEnd
