# Reliable sync

Status: implemented locally on 2026-09-21. Backend rollout and a physical-device TestFlight smoke test remain deployment checks.

The outcome is a sync that accounts for every discovered activity, survives an
app disconnect or backend restart, and reports clearly what is imported, waiting,
or needs attention. PostgreSQL remains authoritative.

## Product decisions

| Decision | Confirmed scope |
| --- | --- |
| Trigger | Manual Sync button for the first release |
| Initial import | All cycling history, newest first |
| Data changes | New activities and repair of incomplete imports; edits/deletions follow next |

Normal subsequent syncs should find new activities without downloading every complete activity again.
Keep a date-range import/repair option. Incremental discovery must account for
late uploads with old activity dates; do not assume the latest activity timestamp
alone proves all earlier history is complete.

## Findings that expand the original milestone

- `internal/sync/sync_activities.go`: detail/stream download errors are logged
  and then discarded; only save failures enter `FailedActivities`. Retries
  reconstruct summaries from activity IDs, losing athlete identity and metadata.
- `internal/pggeo/insert.go`: summary, geometry, and samples are not committed
  in one transaction. A later failure can leave a summary that the next sync
  considers already imported. Both atomic writes and repair of earlier partial
  imports belong in this milestone.
- `internal/strava/activities.go`: fixed sleeps do not implement quota handling;
  pagination can stop at 100 pages without declaring the import incomplete.
  Downloads hold all activity details in memory before saving.
- `internal/web/mobile.go`: sync returns progress only when its long HTTP
  request finishes. The backend uses its server context, so a disconnected
  client can lose visibility while work continues. There is no durable job ID.
- Strava tokens are currently refreshed at the mobile-request boundary. A job
  that waits on quotas must be able to refresh credentials during execution.

## Delivery sequence

### 1. Correct imports and meaningful failure reporting

- Preserve the complete summary and authenticated athlete identity on retries.
- Record discovery, detail, stream, and database failures consistently. Report
  partial completion explicitly; never label an incomplete import successful.
- Save each activity's summary, geometry, and samples atomically and idempotently.
  Validate ownership before updates; retries must not change activity ownership.
- Distinguish valid activities without GPS/sensor streams from incomplete imports.
  Missing route data alone must not trigger endless retries.
- Add an explicit completeness marker for future imports and a safe repair path
  for existing records whose completeness is unknown.
- Process and commit activities incrementally; remove silent pagination limits.
- Keep Discovered-map refresh as a separately reported, retryable finalization
  step. A failed map refresh must not require downloading the activities again.

### 2. Persistent server jobs and recovery

- Store jobs, per-activity work, progress, checkpoints, and next retry times in
  PostgreSQL. Use the existing backend worker process; no additional queue service.
- Allow one active job per athlete. Repeated taps, reconnects, or two devices
  must attach to existing work rather than launch competing imports.
- Resume interrupted jobs after backend restart. Persist discovery progress and
  unfinished items; mark checkpoints only after the corresponding work commits.
  Use PostgreSQL connection-scoped advisory locks so a disconnected worker cannot overwrite recovered work.
- Retry transient transport/server failures with bounded exponential backoff.
  Stop retries for permanent failures and expose a useful reason.
- Coordinate Strava request budgets across athletes. Respect both overall and
  read-specific quota headers, pause on 429, and show the next permitted attempt.
- Refresh and persist credentials during long-running work, with serialized
  refresh per athlete. Refresh-token rotation must be safe across devices.
  Unrecoverable authorization failure requires reconnecting Strava.
- Add authenticated start/status/retry/cancel job endpoints. Every operation is
  scoped to its athlete. Keep the current mobile API compatible with installed
  TestFlight clients, and route legacy web/CLI sync through the same safeguards.

### 3. iOS progress and recovery

- Start or attach to a server job promptly; poll status while the app is active.
  Recover the current job when the app reopens or connectivity returns.
- Show phase, imported/existing/failed counts, last successful sync, quota waits,
  and a readable explanation when reconnecting Strava is required.
- During discovery, show activities found rather than a fabricated percentage.
- Offer Cancel and Retry failed items. Cancellation preserves already committed
  activities. The user can keep browsing during a sync.
- Fetching server-job progress must not repeatedly trigger a new Strava sync.

## Acceptance checks

1. Inject detail, stream, and database failures: no activity is silently lost,
   failure totals remain accurate, and retry preserves metadata and ownership.
2. Fail midway through saving: no partial replacement is committed; existing
   complete data remains intact.
3. Close/reopen the app and restart the backend during discovery, import, quota
   waiting, and map finalization: unfinished work resumes without duplicate rows.
4. Start from two devices: one active job, consistent progress. Another athlete
   cannot observe, cancel, or retry the job.
5. Exhaust short/daily quotas and expire/rotate credentials: waiting and recovery
   work without a request storm or an unnecessary fresh import.
6. Import indoor rides, rides without GPS, empty histories, and large histories.
   Legitimately absent streams are not reported as corruption.
7. Retry, cancel, and reconnect on iOS against controlled API responses, then
   smoke-test the complete flow on the backend and a TestFlight device.
8. Existing mobile clients and the web/CLI sync paths retain compatible behavior.

## Scope boundaries

Automatic triggers and edit/deletion reconciliation are deferred. Offline
browsing, CloudKit, push notifications, and the broader web
multi-user redesign remain separate work. Job recovery does not depend on the
iPhone keeping the app alive.

## Strava constraints

- Quotas are per application, have both short and daily windows, and include
  separate read budgets: https://developers.strava.com/docs/rate-limits/
- Access tokens expire and refresh tokens can rotate:
  https://developers.strava.com/docs/authentication/
- Webhooks cover activity creation, deletion, selected updates, and revocation:
  https://developers.strava.com/docs/webhooks/

Checked against the current documentation on 2026-09-21. Use response headers
and stored expiration times rather than hard-coded assumptions about quotas.

## Implementation and rollout

- `internal/sync/jobs.go`: persistent jobs/items, progress, retries, cancellation,
  repair, quota waits, and independent map finalization.
- `internal/strava/client.go`: contextual requests, identity checks, typed errors,
  pagination, and overall/read quota-header handling.
- `internal/pggeo/sync_schema.go`: additive migrations; old rows start unverified.
  Activity writes and their job checkpoints commit in the same transaction.
- `internal/web/mobile_sync.go`: authenticated job API, worker, and serialized
  credential refresh shared by devices. The legacy mobile response shape remains.
- iOS Settings: all-history default, optional date range, current/last sync status,
  cancel/retry, and foreground reconnection. Other screens remain usable.

Deploy the backend before publishing the updated iOS app. Server startup applies
additive migrations without resetting activity data. Previously imported rows have
no trustworthy completion marker, so their details/streams are checked again once.
A normal later sync scans summary pages (including old-dated uploads) and downloads
only new/unverified activities. Edits and deletions are deferred as agreed.

No external queue is needed. The web server runs the worker; PostgreSQL advisory
locks serialize import work across server processes and the legacy CLI. A closed
worker connection releases its claim. Cancelling preserves committed activity
imports. If the CLI is interrupted, the web worker can continue when a persisted
Strava session is available; otherwise the job requests a fresh Strava connection.
Indoor sensor samples are retained with a null location and excluded from map
queries. Displaying indoor-only sensor charts is outside the existing route UI.

### Mobile API

All endpoints require the athlete's B11K bearer session. Session restoration,
saved-activity reads, and job status do not refresh Strava credentials, allowing
an authorization-blocked job to remain visible after reopening the app. The B11K
session's own expiry still applies.

| Request | Result |
| --- | --- |
| `POST /api/mobile/sync/jobs` | HTTP 202; start or attach to the active job |
| `GET /api/mobile/sync/jobs` | Active job, otherwise latest job, otherwise `job: null` |
| `GET /api/mobile/sync/jobs/{id}` | Athlete-scoped job and up to 20 failure details |
| `POST /api/mobile/sync/jobs/{id}/cancel` | Stop unfinished work; keep committed imports |
| `POST /api/mobile/sync/jobs/{id}/retry` | Resume unfinished work; retain successes |

Start accepts optional `start` and `end` dates in `YYYY-MM-DD`, inclusive end day
in UTC. The response envelope is `{ "job": ... }`; job summaries include total,
existing, new, success, failed, and pending counts. Waiting jobs expose
`next_attempt_at`. States are queued, running, waiting, needs_auth, complete,
partial, failed, and cancelled.

### Local verification

Run normal Go tests with `go test ./...`. Integration tests opt in through
`B11K_TEST_DATABASE_URL` and create/drop their own randomly named schemas in an
isolated PostGIS database; never point this at production. Run the complete suite
with `B11K_TEST_DATABASE_URL=... go test -race ./...`.

The integration suite exercises failed downloads, retries after reconnecting the
worker, transaction rollback, older partial rows, concurrent starts, athlete
isolation, cancellation in flight, persisted quotas, credential rotation across
devices, absent GPS, map-only recovery, connection-lock recovery, and repeatable
schema migration. iOS tests cover real job-response decoding and the recovery
states, including nanosecond timestamps from Go.
