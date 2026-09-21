# Utilities

Run these from any directory; they resolve the checkout themselves. Backend
operations require Bash and Docker Compose v2. Test commands also require Go;
`release-apple` requires Python 3 and Xcode on macOS.

| Command | Purpose |
| --- | --- |
| `./scripts/rebuild-backend` | Save the current image and a compressed database backup, build, migrate, replace the app, and wait for health |
| `./scripts/backup-backend` | Save a database backup and current image without rebuilding or stopping the app |
| `./scripts/rollback-backend [snapshot]` | Run the image saved before that update; default to the latest completed snapshot with an image |
| `./scripts/status-backend` | Show Compose containers and health |
| `./scripts/logs-backend --follow` | Follow backend logs, starting with the last 100 lines |
| `./scripts/dev-backend` | Run the existing live development stack (`live-test.sh`) |
| `./scripts/build-backend` | Build `bin/b11k` for this host; accepts `GOOS`/`GOARCH` for cross-compilation |
| `./scripts/test-backend` | Run Go tests; database integration tests require an explicit test database |
| `./scripts/test-backend-integration` | Start disposable PostGIS on a free localhost port, run all tests, then remove it |
| `./scripts/test-backend-race` | Run that complete integration suite with Go's race detector |
| `./scripts/release-apple …` | Build, test, archive, and publish iOS; see [Apple release setup](../iosApp/B11k/IOS_DISTRIBUTION.md#command-line-internal-testflight) |

Every command supports `--help`. Test scripts accept Go test arguments, for example
`./scripts/test-backend-race -run TestConcurrentStarts ./internal/sync`.
`GOCACHE` is respected; the default is a temporary-directory cache. The disposable
database commands always override `B11K_TEST_DATABASE_URL` with their own container
address and remove only that container and its anonymous volume. They do not read
the backend `.env` or use its database.

Operational commands explicitly select `docker-compose.yml` and read the checkout's
`.env`. Keep the same checkout directory and `COMPOSE_PROJECT_NAME` as your existing
deployment so Compose continues using the same database volume. The live stack is
separate: restart it after Go edits with
`docker compose -f docker-compose.live.yml restart b11k-live-app`.

The [deployment guide](../deploy/README.md) contains the rsync template, first-time
setup, snapshots, image rollback, and database recovery. Successful rebuilds retain
all snapshots under ignored `backups/backend/`; nothing is pruned automatically.
Set `BACKEND_HEALTH_TIMEOUT=300` for a slower host (default: 180 seconds per service).

These commands adapt Skupobrate's rebuild, backup/rollback, development, and test
workflows to B11K. B11K migrations are applied by `-validate-schema` and at normal
startup, so a separate migration framework or migration command is unnecessary.

The older `./build.sh` delegates to `build-backend` with its original Linux/amd64
default; compilation failures now stop immediately. `./run.sh` runs the current
`./cmd` entrypoint from the checkout root and accepts application flags. Native
runs use root `config.yaml` plus exported environment variables (they do not load
Compose's `.env` automatically); web assets are read from root `web/`.

Utility failure-path and rsync-filter tests use only temporary files and a fake
Docker CLI: `python3 -B -m unittest discover -s scripts/tests -p test_backend_scripts.py`.
