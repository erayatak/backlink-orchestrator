PROJECT: BACKLINK DATA ENGINE
COMPONENT: ORCHESTRATOR / CONTROL PLANE
DOCUMENT TYPE: MASTER IMPLEMENTATION SCOPE
DOCUMENT STATUS: IMPLEMENTATION AUTHORITY
VERSION: 1.0

============================================================
0. UYGULAMA EMRİ
============================================================

Bu doküman, Backlink Data Engine projesinin Orchestrator bileşeninin
sıfırdan üretim kalitesinde geliştirilmesi için tek yetkili teknik
kapsam dokümanıdır.

Bu dokümanı uygulayan yazılımcı veya AI coding agent:

- mimariyi yeniden yorumlamayacak,
- teknoloji seçimini değiştirmeyecek,
- eksik gördüğü yerleri kendi tercihiyle doldurmayacak,
- aşağıda tanımlanan davranışların dışına çıkmayacak,
- belirtilen bir özellik için "sonra yapılabilir" yaklaşımıyla
  production kritik davranışı atlamayacak,
- TODO, FIXME, placeholder veya sahte implementation bırakmayacak,
- test edilmemiş kodu tamamlanmış kabul etmeyecek.

Amaç:
Yeni bir Linux sunucusuna Orchestrator kurulduğunda sistemin:

1. otomatik başlaması,
2. Worker'ları kaydetmesi,
3. Worker sağlık durumunu izlemesi,
4. task üretmesi ve dağıtması,
5. Worker ölürse task'ı geri alması,
6. başka Worker'a yeniden vermesi,
7. tüm kontrol durumunu PostgreSQL'de kalıcı tutması,
8. Dashboard üzerinden canlı izlenebilmesi,
9. Worker sayısı arttığında manuel task dağıtımı gerektirmemesi,
10. yeniden başlatma veya küçük kesintiler sonrasında kaldığı yerden
    devam edebilmesi

gerekmektedir.

Bu repository backlink verisini işlemez.
Bu repository Common Crawl WAT parse etmez.
Bu repository büyük veri saklamaz.

Orchestrator sadece CONTROL PLANE'dir.


============================================================
1. REPOSITORY
============================================================

GitHub repository adı:

backlink-orchestrator

Repository private olacaktır.

Repository oluşturma:

gh repo create backlink-orchestrator --private --clone

Çalışma branch'i:

main

Branch protection uygulanacaktır.

Pull request + CI geçmeden main'e merge edilmeyecektir.


============================================================
2. TEKNOLOJİ STACK
============================================================

Backend:
Go 1.24+

HTTP server:
Go standard library net/http

API:
REST / JSON over HTTPS

Frontend:
Server-rendered HTML
HTMX
minimal vanilla JavaScript

Amaç:
1 GB RAM'li Orchestrator sunucusunda minimum runtime overhead.

Database:
PostgreSQL 18

ORM:
KULLANILMAYACAK.

SQL migration:
goose veya eşdeğer migration sistemi.
Migration dosyaları repository içerisinde tutulacak.

Queue:
PostgreSQL.

Redis:
KULLANILMAYACAK.

Kafka:
KULLANILMAYACAK.

RabbitMQ:
KULLANILMAYACAK.

WebSocket:
KULLANILMAYACAK.

Dashboard canlı güncellemesi:
Server-Sent Events veya kısa polling.
İlk sürümde SSE tercih edilecek.

Reverse proxy:
Caddy

Process supervision:
systemd

Container:
Orchestrator'ın production çalıştırılması için Docker ZORUNLU DEĞİL.
İlk production deployment:
native Go binary + systemd + native PostgreSQL + Caddy.

Reason:
1 GB RAM'de gereksiz container katmanlarını ve ayrı servisleri
minimumda tutmak.

Development:
Docker Compose yalnızca local development için kullanılabilir.

CI:
GitHub Actions.

Repository:
GitHub private.


============================================================
3. REPOSITORY YAPISI
============================================================

Aşağıdaki yapı zorunludur:

backlink-orchestrator/
│
├── cmd/
│   └── orchestrator/
│       └── main.go
│
├── internal/
│   ├── config/
│   ├── httpapi/
│   ├── auth/
│   ├── workers/
│   ├── tasks/
│   ├── jobs/
│   ├── scheduler/
│   ├── heartbeat/
│   ├── recovery/
│   ├── dashboard/
│   ├── database/
│   ├── events/
│   └── health/
│
├── migrations/
│
├── web/
│   ├── templates/
│   ├── static/
│   └── assets/
│
├── deploy/
│   ├── systemd/
│   ├── caddy/
│   └── scripts/
│
├── scripts/
│   ├── bootstrap.sh
│   ├── deploy.sh
│   ├── backup.sh
│   ├── restore.sh
│   ├── healthcheck.sh
│   └── smoke-test.sh
│
├── tests/
│   ├── integration/
│   └── e2e/
│
├── docs/
│   ├── architecture.md
│   ├── api.md
│   ├── deployment.md
│   ├── operations.md
│   └── recovery.md
│
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
│
├── AGENTS.md
├── README.md
├── Makefile
├── go.mod
└── .env.example


============================================================
4. ANA TASARIM
============================================================

Sistem:

                   ORCHESTRATOR

       ┌─────────────────────────────┐
       │          HTTP API           │
       ├─────────────────────────────┤
       │ Worker Registry             │
       │ Authentication             │
       │ Scheduler                  │
       │ Task Queue                 │
       │ Lease Manager              │
       │ Recovery                   │
       │ Dashboard                  │
       ├─────────────────────────────┤
       │ PostgreSQL                 │
       └─────────────────────────────┘

Worker hiçbir zaman doğrudan Worker -> Worker iletişimi yapmaz.

Worker'lar birbirlerinin durumunu bilmez.

Orchestrator merkezi kontrol otoritesidir.


============================================================
5. CONTROL PLANE VE DATA PLANE AYRIMI
============================================================

Orchestrator:
CONTROL PLANE

Worker:
DATA PLANE

Orchestrator şu verileri ASLA taşımaz:

- milyonlarca backlink
- WAT içeriği
- büyük parsed output
- büyük JSON payload
- ham Common Crawl dosyası

Worker bunları işler.

Worker sonuçları gelecekte R2'ye yazar.

Orchestrator'a yalnızca kontrol metadata gönderilir:

- task started
- task progress
- task completed
- task failed
- output URI
- counters
- worker health


============================================================
6. WORKER KİMLİĞİ
============================================================

Her Worker benzersiz worker_id alır.

Format:

worker_<ULID>

Worker ID server hostname'a bağlanmayacaktır.

Hostname değişse bile Worker ID değişmez.

Worker registration sırasında:

- worker_id
- hostname
- platform
- architecture
- CPU count
- memory MB
- worker version

gönderilir.

Orchestrator worker_id unique constraint uygulayacaktır.


============================================================
7. WORKER DURUMLARI
============================================================

Worker state machine:

PROVISIONING
    ↓
READY
    ↓
BUSY
    ↓
READY

Her state:

READY
BUSY
DEGRADED
OFFLINE
DISABLED

Orchestrator ayrıca registration sırasında:

PROVISIONING

durumunu kullanabilir.

OFFLINE:
heartbeat timeout sonucu oluşur.

DISABLED:
manuel olarak yönetici tarafından kapatılır.

DISABLED Worker task alamaz.


============================================================
8. WORKER HEALTH MODELİ
============================================================

Worker heartbeat interval:

30 saniye

Worker offline threshold:

90 saniye

Worker'ın son heartbeat zamanı:

last_heartbeat_at

Orchestrator health loop:

10 saniyede bir çalışır.

Kurallar:

last_heartbeat_at <= now - 90s

ise Worker:

OFFLINE

olur.

Worker BUSY ise current task lease'i recovery işlemine girer.


============================================================
9. TASK MODELİ
============================================================

İlk data-processing task:

1 Common Crawl WAT file = 1 task.

Örnek:

crawl-data/CC-MAIN-2026-30/...
    file-00001
    file-00002
    file-00003

Her biri bağımsız task.

Task global olarak benzersiz UUID/ULID taşır.

Task durumları:

QUEUED
LEASED
RUNNING
SUCCEEDED
FAILED
RETRY_WAIT
CANCELLED


============================================================
10. TASK LEASE
============================================================

Worker task aldığında Orchestrator task için lease oluşturur.

Initial lease:

10 dakika

Worker task üzerinde çalışırken progress/heartbeat gönderebilir.

Lease worker tarafından uzatılır.

Worker heartbeat gönderemiyorsa lease yenilenmez.

Recovery loop expired lease'leri bulur.

Expired task:

RUNNING / LEASED
      ↓
RETRY_WAIT
      ↓
QUEUED

hale getirilir.

Task aynı anda iki Worker'a verilmemelidir.

Task claim transaction-safe olmalıdır.

PostgreSQL queue locking:

SELECT ... FOR UPDATE SKIP LOCKED

modeli kullanılacaktır.

PostgreSQL, queue benzeri çoklu consumer senaryolarında
SKIP LOCKED desteğini sağlar.


============================================================
11. TASK RETRY POLICY
============================================================

MAX ATTEMPTS:

5

Deneme sayısı:

1..5

Her hata otomatik olarak aynı davranmayacak.

Fatal configuration error:
retry yok.

Transient network error:
retry var.

R2 temporary failure:
retry var.

Worker crash:
retry var.

Malformed source object:
task FAILED olabilir.

5 başarısız denemeden sonra:

FAILED

olarak kalır.

FAILED task Dashboard'da görünür.


============================================================
12. TASK IDEMPOTENCY
============================================================

Aynı source_path iki kez task olarak oluşturulmamalıdır.

Job + source_path unique constraint uygulanacaktır.

Aynı task yeniden çalıştırılırsa:

aynı task_id üzerinden attempt oluşturulacaktır.

Yeni task oluşturulmayacaktır.


============================================================
13. TASK ATTEMPTS
============================================================

Her task'ın denemeleri ayrı tutulacaktır.

task_attempts:

id
task_id
worker_id
attempt_number
started_at
finished_at
status
error_code
error_message
output_uri
processed_records
processed_links
duration_ms

Amaç:

hangi task'ın hangi Worker'da neden başarısız olduğunu
sonradan görebilmek.


============================================================
14. JOB
============================================================

Job = büyük veri işleme operasyonu.

Örnek:

JOB:
CC-MAIN-2026-30

Job:

- dataset
- crawl_id
- total_tasks
- queued_tasks
- running_tasks
- succeeded_tasks
- failed_tasks
- created_at
- started_at
- finished_at

tutar.

Job oluşturulduğunda WAT paths listesi task'lara dönüştürülür.


============================================================
15. JOB STATES
============================================================

CREATED
QUEUED
RUNNING
PAUSED
COMPLETED
FAILED
CANCELLED

Bir job:

COMPLETED

olmak için:

succeeded_tasks == total_tasks

olmalıdır.

Failed task varsa job otomatik COMPLETED sayılmayacaktır.

Job Dashboard'da ayrıca:

partial success

gösterebilir.


============================================================
16. WORKER REGISTRATION API
============================================================

Endpoint:

POST /api/v1/workers/register

Request:

{
  "worker_id": "...",
  "hostname": "...",
  "os": "linux",
  "architecture": "amd64",
  "cpu_count": 2,
  "memory_mb": 1024,
  "version": "1.0.0"
}

Response:

{
  "worker_id": "...",
  "status": "READY",
  "server_time": "...",
  "heartbeat_interval_seconds": 30,
  "heartbeat_timeout_seconds": 90
}


============================================================
17. WORKER AUTHENTICATION
============================================================

Kalıcı master password kullanılmayacaktır.

Registration:

bootstrap token

ile yapılacaktır.

Bootstrap token:

- short lived
- single use
- hashed database'de tutulur
- plaintext database'e yazılmaz
- kullanıldıktan sonra revoke edilir

Registration sonrası Worker:

long-lived worker credential

alır.

Credential yalnızca worker tarafından saklanır.

Orchestrator plaintext secret saklamaz.

Worker credential rotation endpointi bulunacaktır.


============================================================
18. AUTH MODEL
============================================================

Worker API:

Bearer token

kullanacaktır.

Admin Dashboard:

admin session authentication.

Browser'da Worker token hiçbir zaman kullanılmaz.

Worker token'ları Dashboard'a gösterilmez.

API token loglara yazılmaz.


============================================================
19. HEARTBEAT API
============================================================

POST:

/api/v1/workers/heartbeat

Payload:

{
  "worker_id": "...",
  "status": "BUSY",
  "current_task_id": "...",
  "cpu_percent": 62.4,
  "memory_percent": 47.1,
  "tasks_completed": 42,
  "records_processed": 123456,
  "links_extracted": 789012
}

Response:

{
  "ok": true,
  "server_time": "...",
  "task_action": "CONTINUE"
}


============================================================
20. TASK CLAIM API
============================================================

POST:

/api/v1/tasks/claim

Worker:

READY

olduğunda task claim ister.

Body:

{
  "worker_id": "...",
  "capacity": {
    "cpu_count": 2,
    "memory_mb": 1024
  }
}

Response:

{
  "task_id": "...",
  "lease_until": "...",
  "type": "COMMON_CRAWL_WAT",
  "source": {
    "dataset": "CC-MAIN-2026-30",
    "path": "crawl-data/..."
  }
}

Task yoksa:

204 No Content

döner.


============================================================
21. TASK HEARTBEAT
============================================================

POST:

/api/v1/tasks/{task_id}/heartbeat

Worker task hakkında:

progress
elapsed
processed bytes
processed records

gönderir.

Response:

lease_until

güncellenmiş zamanı döndürür.


============================================================
22. TASK COMPLETE
============================================================

POST:

/api/v1/tasks/{task_id}/complete

Payload:

{
  "attempt_id": "...",
  "processed_bytes": 123456789,
  "records_processed": 65215,
  "links_found": 3648725,
  "backlinks_found": 321019,
  "output_uri": "r2://..."
}

Orchestrator:

- attempt = SUCCEEDED
- task = SUCCEEDED
- worker = READY

yapar.


============================================================
23. TASK FAIL
============================================================

POST:

/api/v1/tasks/{task_id}/fail

Payload:

{
  "attempt_id": "...",
  "error_code": "R2_UPLOAD_FAILED",
  "error_message": "...",
  "retryable": true
}

retryable=true:

RETRY_WAIT

retryable=false:

FAILED


============================================================
24. DATABASE
============================================================

PostgreSQL 18.

Foreign keys aktif.

UUID/ULID ID'ler kullanılacaktır.

Timestamp:
TIMESTAMPTZ.

Tüm zaman değerleri UTC.

Tablolar:

workers
jobs
tasks
task_attempts
worker_heartbeat_history
system_events
admin_sessions
bootstrap_tokens

Önceki basit tasarıma ek olarak heartbeat history ve authentication
tabloları zorunlu tutulacaktır.


============================================================
25. WORKERS TABLE
============================================================

workers:

id
worker_id
status
hostname
os
architecture
cpu_count
memory_mb
version
current_task_id
last_heartbeat_at
registered_at
updated_at
disabled_at

Unique:

worker_id


============================================================
26. JOBS TABLE
============================================================

jobs:

id
job_id
dataset
crawl_id
status
total_tasks
queued_tasks
running_tasks
succeeded_tasks
failed_tasks
created_at
started_at
finished_at
updated_at

Unique:

dataset + crawl_id


============================================================
27. TASKS TABLE
============================================================

tasks:

id
task_id
job_id
dataset
source_path
status
assigned_worker_id
current_attempt
lease_until
started_at
finished_at
output_uri
created_at
updated_at

Unique:

job_id + source_path


============================================================
28. TASK ATTEMPTS TABLE
============================================================

task_attempts:

id
task_id
worker_id
attempt_number
status
started_at
finished_at
lease_until
processed_bytes
processed_records
processed_links
output_uri
error_code
error_message

Unique:

task_id + attempt_number


============================================================
29. HEARTBEAT HISTORY
============================================================

worker_heartbeat_history:

id
worker_id
recorded_at
status
cpu_percent
memory_percent
current_task_id
processed_records
processed_links

Retention:

yüksek frekanslı heartbeat history sınırsız tutulmayacak.

Default retention:

7 days

Daha eski kayıtlar aggregate edilip silinebilir.


============================================================
30. EVENTS
============================================================

system_events:

id
severity
event_type
worker_id
task_id
job_id
message
metadata_json
created_at

severity:

INFO
WARN
ERROR
CRITICAL


============================================================
31. ADMIN SESSION
============================================================

Dashboard admin kullanıcıları.

admin_sessions:

id
session_id
user_identifier
token_hash
created_at
expires_at
last_seen_at

Session token plaintext DB'ye yazılmaz.

Session expiration:

24h.

Admin authentication ilk MVP'de tek admin hesabıyla başlatılabilir.

Password:
Argon2id hash.

Multi-user RBAC ilk MVP kapsamı değildir.


============================================================
32. BOOTSTRAP TOKENS
============================================================

bootstrap_tokens:

id
token_hash
created_at
expires_at
used_at
created_by
status

Token:

tek kullanımlık.

Expiration:
15 dakika.

Token plaintext database'e yazılmaz.


============================================================
33. DATABASE INDEXLERİ
============================================================

workers:
worker_id UNIQUE
status
last_heartbeat_at

jobs:
dataset,crawl_id UNIQUE
status

tasks:
job_id,status
status,lease_until
assigned_worker_id
job_id,source_path UNIQUE

task_attempts:
task_id
worker_id
status

heartbeat:
worker_id,recorded_at

events:
created_at
severity
worker_id
task_id

Queue claim işlemi için:

status + lease_until + created_at

indexi oluşturulacaktır.


============================================================
34. TASK CLAIM TRANSACTION
============================================================

Task claim atomik yapılacaktır.

Transaction:

BEGIN

SELECT task
FROM tasks
WHERE status = 'QUEUED'
ORDER BY created_at
FOR UPDATE SKIP LOCKED
LIMIT 1;

UPDATE task
SET:
status = LEASED
assigned_worker_id = worker_id
lease_until = now() + lease_duration

INSERT task_attempt

COMMIT

İki worker aynı task'ı alamaz.

PostgreSQL SKIP LOCKED queue consumer pattern'i bu kullanımın
temel concurrency mekanizmasıdır.


============================================================
35. RECOVERY LOOP
============================================================

Her 10 saniyede:

1. OFFLINE worker'ları bul
2. expired leases bul
3. expired RUNNING task'ları reclaim et
4. retry count kontrol et
5. uygun task'ları QUEUED yap
6. event üret

Recovery idempotent olmalıdır.

Aynı task iki kez reclaim edilmemelidir.


============================================================
36. SCHEDULER
============================================================

Scheduler şu anda karmaşık AI scheduler değildir.

İlk sürüm:

FIFO + worker eligibility

kullanır.

Eligibility:

worker status == READY

veya

worker status == BUSY fakat yeni task capacity bildirirse.

İlk MVP'de CPU/memory tabanlı karmaşık scheduling kullanılmayacak.

Worker kendi kapasitesini bildirir.

İleride Worker capacity scoring eklenebilir.

MVP'de gereksiz karmaşıklık yoktur.


============================================================
37. WORKER CAPACITY
============================================================

Worker registration sırasında:

cpu_count
memory_mb

gönderir.

Task claim sırasında worker:

capacity hint

gönderebilir.

Orchestrator ilk sürümde bunu yalnızca kayıt/izleme amacıyla
kullanacaktır.

Task 1 WAT file granularity ile dağıtıldığı için aynı file task'i
bölünmeyecektir.


============================================================
38. IDEMPOTENT API
============================================================

Bütün state-changing API çağrıları idempotent olacak şekilde tasarlanacaktır.

Complete iki kere gönderilirse:

ilk response ile aynı mantıksal başarı sonucu döndürülür.

Fail, complete olmuş task'ı tekrar FAILED yapamaz.

Heartbeat olmayan worker'ın eski heartbeat'i yeni heartbeat'in yerine
geçmez.


============================================================
39. API ERROR FORMAT
============================================================

Bütün API hataları:

{
  "error": {
    "code": "TASK_NOT_FOUND",
    "message": "Task does not exist",
    "request_id": "..."
  }
}

formatında olacaktır.

HTTP status:

400 bad request
401 unauthorized
403 forbidden
404 not found
409 conflict
422 validation
429 rate limited
500 internal
503 dependency unavailable


============================================================
40. REQUEST ID
============================================================

Her request:

X-Request-ID

taşır.

Yoksa Orchestrator üretir.

Request ID:

- response header
- structured logs
- error events

içinde bulunur.


============================================================
41. LOGGING
============================================================

JSON structured logging.

Her log:

timestamp
level
service
request_id
worker_id
task_id
job_id
message

alanlarını gerektiğinde taşır.

Secret/token/password hiçbir şekilde loglanmaz.


============================================================
42. METRICS
============================================================

Prometheus-compatible metrics.

Minimum:

orchestrator_http_requests_total
orchestrator_http_request_duration_seconds
workers_total
workers_online
workers_offline
tasks_queued
tasks_running
tasks_succeeded_total
tasks_failed_total
tasks_retried_total
task_claim_duration_seconds
task_duration_seconds
task_recovery_total
heartbeat_total
heartbeat_timeout_total
db_query_duration_seconds

Dashboard metrics bu veriler üzerinden üretilebilir.


============================================================
43. HEALTH ENDPOINTS
============================================================

GET /health/live

Process ayakta ise:

200


GET /health/ready

Şunlar çalışıyorsa:

- PostgreSQL bağlantısı
- required configuration
- migration version

200.

Aksi:

503.


============================================================
44. DASHBOARD
============================================================

URL:

/

Dashboard login gerektirir.

Ana sayfa:

Overview

gösterir.

Kartlar:

Workers
Online
Offline
Busy

Tasks
Queued
Running
Succeeded
Failed

Jobs
Active
Completed
Failed

Throughput

Errors

R2 Output

Dashboard polling veya SSE ile canlı güncellenir.


============================================================
45. WORKER DASHBOARD
============================================================

/workers

Tablo:

worker_id
status
version
CPU
RAM
last heartbeat
current task
processed records
processed links

Worker detail:

/workers/{id}

gösterir.


============================================================
46. TASK DASHBOARD
============================================================

/tasks

Filtre:

status
job
worker

Task detail:

task_id
source path
worker
attempt
lease
started
finished
output
errors


============================================================
47. JOB DASHBOARD
============================================================

/jobs

Job detail:

total
queued
running
completed
failed
progress %
ETA

ETA ilk sürümde:

completed rate

üzerinden hesaplanır.


============================================================
48. ADMIN OPERATIONS
============================================================

Dashboard üzerinden:

Worker disable
Worker enable

Job pause
Job resume
Job cancel

Task retry

işlemleri yapılabilir.

Manuel task assignment MVP'de yapılmaz.


============================================================
49. SECURITY
============================================================

HTTPS zorunlu.

Caddy TLS termination yapar.

Backend yalnızca localhost bind edebilir.

Örnek:

127.0.0.1:8080

Public port:

443

Caddy -> Orchestrator

Worker API:

HTTPS üzerinden.


============================================================
50. RATE LIMITING
============================================================

Worker registration:
IP + bootstrap token bazlı rate limit.

Heartbeat:
worker bazlı rate limit.

Admin login:
IP bazlı rate limit.

Amaç:
Orchestrator'ın küçük kaynaklarını kötü/yanlış kullanımın tüketmesini
önlemek.


============================================================
51. CONFIGURATION
============================================================

Environment variables:

APP_ENV
APP_PORT
DATABASE_URL
PUBLIC_BASE_URL

SESSION_SECRET
ADMIN_USERNAME
ADMIN_PASSWORD_HASH

BOOTSTRAP_TOKEN_TTL

HEARTBEAT_INTERVAL
HEARTBEAT_TIMEOUT

TASK_LEASE_DURATION
TASK_MAX_ATTEMPTS

LOG_LEVEL

R2_ENDPOINT
R2_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY
R2_BUCKET

Orchestrator kendi R2 credentiallarını Worker veri işleme amacıyla
kullanmaz.

R2 worker output için kullanılır.


============================================================
52. R2
============================================================

Orchestrator R2'de backlink data tutmaz.

Orchestrator yalnızca:

output_uri

metadata'sını saklar.

Örnek:

r2://bucket/jobs/<job-id>/tasks/<task-id>/output.parquet.zst

R2 API erişimi S3-compatible API üzerinden yapılabilir.


============================================================
53. DEPLOYMENT
============================================================

Production server:

Ubuntu LTS

Kurulum scripti:

deploy/scripts/bootstrap.sh

Görevleri:

- apt update
- PostgreSQL kur
- Caddy kur
- application user oluştur
- application dizinleri oluştur
- binary kur
- systemd unit kur
- environment file oluştur
- migration çalıştır
- health check çalıştır
- service enable
- service start


============================================================
54. SYSTEMD
============================================================

Service:

backlink-orchestrator.service

Restart:

always

RestartSec:

5

Memory accounting:

aktif

NoNewPrivileges:

yes

ProtectSystem:

strict

ProtectHome:

yes

PrivateTmp:

yes


============================================================
55. CADDY
============================================================

Caddy:

HTTPS
reverse proxy
access logs

sağlar.

Caddy backend:

localhost:8080

üzerine proxy yapar.

Domain:

daha sonra kesin hostname ile environment üzerinden belirlenebilir.


============================================================
56. DATABASE BACKUP
============================================================

Orchestrator PostgreSQL günlük backup üretir.

scripts/backup.sh

pg_dump custom format kullanır.

Backup:

/var/backups/backlink-orchestrator/

MVP'de local backup + R2 offsite backup.

Retention:

7 daily
4 weekly

Restore script:

scripts/restore.sh


============================================================
57. DEPLOYMENT AUTOMATION
============================================================

scripts/deploy.sh

şunları sırayla yapar:

git pull
go build
run tests
database migration
install binary
restart service
health check

Health check fail olursa deployment başarısız sayılır.


============================================================
58. ZERO-DOWNTIME OLMAK ZORUNDA DEĞİL
============================================================

MVP'de deployment sırasında birkaç saniyelik Orchestrator downtime
kabul edilebilir.

Worker'lar Orchestrator ulaşılmazsa:

- task'ı hemen başarısız saymaz
- local task state'i korur
- exponential retry yapar
- Orchestrator geri gelince devam eder

Bu davranış Worker repository'de uygulanacaktır.


============================================================
59. CI
============================================================

Her PR:

go fmt check
go vet
go test
go test -race
go build
migration validation
frontend asset build

çalıştırılacaktır.

CI başarısızsa merge yasaktır.


============================================================
60. TEST KAPSAMI
============================================================

Unit tests:

- task state transitions
- lease calculation
- retry policy
- worker state transitions
- auth
- validation
- URL/API parsing
- scheduler

Integration:

- PostgreSQL
- registration
- task claim concurrency
- heartbeat
- lease expiry
- retry
- recovery

E2E:

1 worker register
1 worker heartbeat
1 task claim
task complete
dashboard shows completed

Failure E2E:

worker claims task
worker disappears
lease expires
task requeued
second worker claims task


============================================================
61. CRITICAL CONCURRENCY TEST
============================================================

100 concurrent simulated workers.

Queue:

1000 tasks.

Expected:

- no task double ownership
- no lost task
- no duplicate successful completion
- all 1000 tasks eventually SUCCEEDED

PostgreSQL locking strategy doğrulanacaktır.


============================================================
62. FAILURE TESTS
============================================================

Test:

Worker dies after claim.

Expected:

task recovered.

Worker dies after output upload but before complete.

Expected:

completion idempotency mechanism duplicate processing riskini
yönetir.

Worker sends duplicate complete.

Expected:

no duplicate task completion state.

Orchestrator restart during processing.

Expected:

PostgreSQL'deki task state korunur.


============================================================
63. OUTPUT COMMIT SEMANTICS
============================================================

Worker output'u upload etmeden:

TASK COMPLETED

diyemez.

Doğru sıra:

process
→ output upload
→ output verify
→ complete task

Output URI task attempt'e yazılır.

Task complete sırasında output URI zorunlu.


============================================================
64. FUTURE-PROOFING
============================================================

Aşağıdakiler şu an IMPLEMENT EDİLMEYECEK:

Kubernetes
Kafka
NATS
Redis
multi-master
multi-region
autoscaling
AI scheduling
crawler network
mobile worker
cross-cloud provisioning

Fakat core architecture bunların ileride eklenmesine engel olmamalıdır.


============================================================
65. DASHBOARD İŞLETME HEDEFİ
============================================================

Kurucu terminal açmadan:

- kaç Worker var?
- hangileri aktif?
- hangileri ölü?
- kaç task kaldı?
- kaç task başarısız?
- hangi Worker hangi task'ta?
- sistem ne kadar throughput üretiyor?
- son hatalar neler?
- Job ne zaman bitecek?

sorularını cevaplayabilmelidir.


============================================================
66. OPERASYONEL DAVRANIŞ
============================================================

Orchestrator yeniden başlatıldığında:

1. DB bağlantısı
2. migrations
3. recovery scan
4. expired lease recovery
5. worker state refresh
6. scheduler start
7. HTTP server ready

sırası uygulanır.

Worker'lar yeniden bağlanır.


============================================================
67. NO HIDDEN MANUAL STEPS
============================================================

Production deployment tamamlandıktan sonra:

- task queue oluşturmak
- worker kaydetmek
- worker task vermek
- worker health izlemek
- failed task retry etmek

manuel SQL veya SSH ile yapılmayacaktır.

Dashboard/API üzerinden yapılacaktır.


============================================================
68. DOCUMENTATION
============================================================

README:

- architecture
- local setup
- production deployment
- configuration
- migration
- backup
- restore
- troubleshooting

AGENTS.md:

AI agent çalışma kuralları.

docs/architecture.md:

detaylı architecture.

docs/api.md:

endpoint contract.

docs/operations.md:

operasyon el kitabı.

docs/recovery.md:

failure/recovery davranışları.


============================================================
69. DEFINITION OF DONE
============================================================

Orchestrator tamamlandı sayılması için:

[ ] GitHub repository oluşturuldu
[ ] README hazır
[ ] AGENTS.md hazır
[ ] Go application çalışıyor
[ ] PostgreSQL migration sistemi çalışıyor
[ ] Production schema oluşuyor
[ ] Worker registration çalışıyor
[ ] Worker auth çalışıyor
[ ] Heartbeat çalışıyor
[ ] Worker offline detection çalışıyor
[ ] Task queue çalışıyor
[ ] Task claim concurrency güvenli
[ ] Lease çalışıyor
[ ] Task recovery çalışıyor
[ ] Retry çalışıyor
[ ] Job yönetimi çalışıyor
[ ] Dashboard çalışıyor
[ ] SSE/polling çalışıyor
[ ] Health endpoints çalışıyor
[ ] Structured logging çalışıyor
[ ] Metrics çalışıyor
[ ] Caddy HTTPS çalışıyor
[ ] systemd çalışıyor
[ ] backup script çalışıyor
[ ] restore script çalışıyor
[ ] deploy script çalışıyor
[ ] CI çalışıyor
[ ] unit tests geçiyor
[ ] integration tests geçiyor
[ ] race detector geçiyor
[ ] concurrency test geçiyor
[ ] failure recovery testi geçiyor
[ ] production smoke test geçiyor


============================================================
70. IMPLEMENTATION ORDER
============================================================

AŞAMA 1
Repository + Go project + config + logger

AŞAMA 2
PostgreSQL + migrations + models

AŞAMA 3
Worker registration + authentication

AŞAMA 4
Heartbeat + health monitoring

AŞAMA 5
Tasks + queue + claim

AŞAMA 6
Lease + retries + recovery

AŞAMA 7
Jobs

AŞAMA 8
Dashboard

AŞAMA 9
Metrics + logs + health

AŞAMA 10
Deployment + Caddy + systemd

AŞAMA 11
Backup/restore

AŞAMA 12
Full integration + failure testing

AŞAMA 13
Production smoke test


============================================================
71. FIRST COMMIT SEQUENCE
============================================================

Commit 1:

chore: initialize orchestrator repository

Commit 2:

feat: add configuration and structured logging

Commit 3:

feat: add postgres migrations and control schema

Commit 4:

feat: add worker registration and authentication

Commit 5:

feat: add worker heartbeat and health monitoring

Commit 6:

feat: add task queue and task leasing

Commit 7:

feat: add task recovery and retry handling

Commit 8:

feat: add job management

Commit 9:

feat: add live dashboard

Commit 10:

feat: add production deployment and systemd

Commit 11:

test: add orchestration integration tests

Commit 12:

test: add worker failure and recovery tests

Commit 13:

chore: production readiness hardening


============================================================
72. FINAL ARCHITECTURAL RULE
============================================================

ORCHESTRATOR = CONTROL

WORKER = COMPUTE

POSTGRESQL = CONTROL STATE

R2 = LARGE DATA STORAGE

DASHBOARD = HUMAN CONTROL INTERFACE

COMMON CRAWL = DATA SOURCE

Orchestrator'ın görevi:

"Bu büyük veriyi işlemek" değildir.

Görevi:

"Bu büyük veriyi işleyen yüzlerce Worker'ın güvenilir biçimde
çalışmasını sağlamak"tır.


============================================================
73. ABSOLUTE PRIORITY
============================================================

Öncelik sırası:

1. correctness
2. task durability
3. worker recovery
4. security
5. observability
6. operational simplicity
7. performance
8. feature breadth

Sistem daha hızlı olsun diye task kaybetmek veya duplicate işlemek
kabul edilemez.

============================================================
74. IMPLEMENTATION AUTHORITY
============================================================

Bu dokümanda açıkça belirtilmeyen bir davranış ortaya çıkarsa:

1. mevcut state machine,
2. idempotency,
3. durability,
4. security,
5. recovery

önceliklerine göre karar verilir.

Ancak kritik davranışlarda varsayım yapılmadan implementation öncesi
ilgili karar docs/architecture.md içerisine açıkça yazılır.

Kod ile dokümantasyon çelişemez.

Production davranışı testlerle doğrulanmadan tamamlanmış sayılmaz.