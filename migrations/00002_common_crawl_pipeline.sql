-- +goose Up
-- +goose StatementBegin
CREATE TABLE crawl_archives (
    crawl_id VARCHAR PRIMARY KEY,
    display_name VARCHAR NOT NULL,
    from_date TIMESTAMPTZ,
    to_date TIMESTAMPTZ,
    status VARCHAR NOT NULL,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_sync_at TIMESTAMPTZ
);

CREATE TABLE crawl_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    crawl_id VARCHAR NOT NULL REFERENCES crawl_archives(crawl_id) ON DELETE CASCADE,
    path VARCHAR NOT NULL,
    status VARCHAR NOT NULL,
    task_id VARCHAR,
    compressed_size BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(crawl_id, path)
);

ALTER TABLE jobs ADD COLUMN pipeline_version VARCHAR NOT NULL DEFAULT 'backlink-v1';
ALTER TABLE jobs DROP CONSTRAINT jobs_dataset_crawl_id_key;
ALTER TABLE jobs ADD CONSTRAINT jobs_dataset_crawl_id_pipeline_key UNIQUE (dataset, crawl_id, pipeline_version);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE jobs DROP CONSTRAINT jobs_dataset_crawl_id_pipeline_key;
ALTER TABLE jobs ADD CONSTRAINT jobs_dataset_crawl_id_key UNIQUE (dataset, crawl_id);
ALTER TABLE jobs DROP COLUMN pipeline_version;
DROP TABLE crawl_files;
DROP TABLE crawl_archives;
-- +goose StatementEnd
