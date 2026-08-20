# Worker API Contract: Common Crawl Orchestration

This document defines the exact contract between the Backlink Orchestrator Control Plane and the worker executing the Common Crawl data processing pipeline.

## 1. Task Claim Payload (Response from Orchestrator)
When a worker claims a task, the Orchestrator responds with a payload defining the workload.

**Endpoint:** `POST /api/v1/tasks/claim`

**Expected JSON Response:**
```json
{
  "task_id": "cc-CC-MAIN-2026-30-backlink-v1-task-42",
  "lease_until": "2026-08-20T21:45:00Z",
  "type": "COMMON_CRAWL_WAT",
  "crawl_id": "CC-MAIN-2026-30",
  "pipeline_version": "backlink-v1",
  "wat_path": "crawl-data/CC-MAIN-2026-30/segments/17150123/wat/CC-MAIN-2026-30-17150123.wat.gz",
  "input_url": "https://data.commoncrawl.org/crawl-data/CC-MAIN-2026-30/segments/17150123/wat/CC-MAIN-2026-30-17150123.wat.gz"
}
```

## 2. Artifact Upload URL Retrieval
Before completing the task, the worker must obtain a presigned URL to upload the resulting output (e.g., Parquet file) to the Cloudflare R2 bucket.

**Endpoint:** `POST /api/v1/tasks/{task_id}/artifact-upload`

**Request JSON:**
```json
{
  "crawl_id": "CC-MAIN-2026-30",
  "pipeline_version": "backlink-v1"
}
```

**Expected JSON Response:**
```json
{
  "upload_url": "https://<bucket>.r2.cloudflarestorage.com/staging/backlinks/crawl=CC-MAIN-2026-30/pipeline=backlink-v1/task=cc-CC-MAIN-2026-30-backlink-v1-task-42.parquet?X-Amz-Algorithm=AWS4-HMAC-SHA256&...",
  "object_key": "staging/backlinks/crawl=CC-MAIN-2026-30/pipeline=backlink-v1/task=cc-CC-MAIN-2026-30-backlink-v1-task-42.parquet"
}
```

*Note: The returned presigned URL expects an HTTP `PUT` request with the artifact body. The URL is valid for 2 hours.*

## 3. Task Completion Request
After the artifact is fully uploaded to R2, the worker signals task completion to the orchestrator.

**Endpoint:** `POST /api/v1/tasks/{task_id}/complete`

**Request JSON:**
```json
{
  "attempt_id": "1",
  "processed_bytes": 124059030,
  "records_processed": 450120,
  "links_found": 1500045,
  "backlinks_found": 23410,
  "output_uri": "staging/backlinks/crawl=CC-MAIN-2026-30/pipeline=backlink-v1/task=cc-CC-MAIN-2026-30-backlink-v1-task-42.parquet"
}
```
*Note: The `output_uri` must exactly match the `object_key` returned during the artifact upload step.*

**Validation:**
Upon receiving the completion request, the Orchestrator executes a synchronous S3 `HeadObject` check against Cloudflare R2.
- If the object does not exist, the Orchestrator rejects the completion with `502 Bad Gateway` (`VERIFICATION_FAILED`).
- If successful, the Orchestrator marks the task as `SUCCEEDED`.

**Expected JSON Response:**
```json
{
  "ok": true
}
```

## 4. Finalization Pipeline Semantics
Once all tasks for a given crawl/job transition to `SUCCEEDED` (or `FAILED`), the `internal/jobs/finalizer.go` background loop takes over:
1. Detects `(succeeded_tasks + failed_tasks) == total_tasks`.
2. Transitions the Job state from `RUNNING/QUEUED` to `FINALIZING`.
3. Consolidates outputs.
4. Transitions the Job state to `COMPLETED` (if all tasks succeeded) or `PARTIAL` (if any tasks failed).
