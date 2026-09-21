# Backend updates

Use `./scripts/rebuild-backend` on the server after uploading backend sources. It
captures the existing container's image, builds the new image, writes a compressed
PostgreSQL backup, stops only the app, applies migrations, starts the app, and waits
for its Docker healthcheck. The database container and its named volume are kept.
Build or backup failures leave the existing app running. Migration failures leave
the app stopped with the backup available for investigation. The command never
passes `-force-rebuild`, truncates tables, or removes a database volume.

## Copy from your Mac

Replace the SSH alias and destination below. Use the **existing checkout directory**
on an already deployed host: Compose's project name determines the database volume.
If the deployment uses `COMPOSE_PROJECT_NAME`, keep that same value on the server.

```bash
cd ~/git/b11k
BACKEND_SSH='user@your-backend-host'
BACKEND_DIR='/srv/b11k'

# Preview the file transfer, including obsolete backend-source deletions.
rsync -az --itemize-changes --dry-run --delete \
  --filter='merge deploy/backend.rsync-filter' \
  ./ "$BACKEND_SSH:$BACKEND_DIR/"

# Apply the reviewed transfer.
rsync -az --itemize-changes --delete \
  --filter='merge deploy/backend.rsync-filter' \
  ./ "$BACKEND_SSH:$BACKEND_DIR/"
```

The destination must exist and be writable by the SSH user. The checked-in filter
is an allowlist: Go runtime sources, web assets, module files, Docker build/Compose
files, non-secret defaults, operational scripts, and this guide. It excludes iOS,
TestFlight archives, build output, Git/editor state, tests, development/release
utilities, credentials, local config, logs, and backups. Server `.env`, `config.yaml`,
and `backups/` are protected from both upload and deletion. Keep the filter updated
if the backend starts requiring a new top-level directory. Do not add
`--delete-excluded`, which would remove protected server files.

## First-time host setup

Install Docker with Compose v2. SSH to the host, then:

```bash
cd /srv/b11k
umask 077
cp -n .env.example .env
chmod 600 .env
# Edit .env for this host: database password, Strava credentials/callbacks,
# public hostnames, HTTPS, and B11K_TOKEN_ENCRYPTION_KEY.
./scripts/rebuild-backend
./scripts/status-backend
```

Use the production exposure settings in [DEPLOYMENT_SECURITY.md](../DEPLOYMENT_SECURITY.md).
Do not overwrite an existing `.env` or regenerate an
existing token-encryption key; encrypted Strava credentials need the original key.
The server does not need Go, Xcode, Python, or Apple signing credentials to rebuild.

## Later updates

After rsync, on the host:

```bash
cd /srv/b11k
./scripts/rebuild-backend
./scripts/status-backend
./scripts/logs-backend --since 10m
# Follow logs when diagnosing a problem:
./scripts/logs-backend --follow
```

For Reliable sync, deploy the backend before the updated iOS client. Interrupted
jobs resume from PostgreSQL when the server restarts.

## Backups and rollback

```bash
# Make a backup without deploying:
./scripts/backup-backend

# List snapshot IDs (YYYYMMDDTHHMMSSZ-PID):
ls backups/backend

# Use the image saved in a particular snapshot, or omit the ID for the latest:
./scripts/rollback-backend 20260921T120000Z-12345
```

Each completed snapshot contains `postgres.dump` (compressed pg_dump custom format),
the Compose/non-secret config files, a completion timestamp, and the previous image
reference when an app container existed. Files are private to the invoking user.
Images are tagged `b11k-backend-snapshot:<snapshot>`. First deployments have no
previous image. Failed/incomplete backups are never selected automatically.

Rollback replaces only the app image, using the current Compose configuration and
server `.env`; it preserves database writes made since the backup. Review schema
compatibility before rolling back across a schema-changing release. Subsequent
`rebuild-backend` commands build current source again. Use the scripts consistently:
a bare `docker compose up` can replace a rolled-back image with the last build.

Snapshots are retained until you remove them explicitly. Keep enough free disk
space, copy needed backups off-host, and retain the token-encryption key separately.
Image tags protect snapshots from ordinary dangling-image pruning; an explicit
`docker image prune -a` can remove them. Backup directories can be removed manually
once no longer needed, together with their matching image tags.

Concurrent backup/rebuild/rollback commands are blocked by
`backups/backend/.operation-lock`. Normal exit removes the lock. After a forced kill,
verify no command remains active before removing that empty directory with `rmdir`.

## Restore a database snapshot

This is a separate, deliberate recovery operation: it replaces database contents
and loses changes after the backup. Stop all B11K writers, choose the snapshot,
keep a current backup, and restore with the database container's own tools:

```bash
SNAPSHOT='20260921T120000Z-12345'
./scripts/backup-backend
docker compose -f docker-compose.yml stop b11k-app
docker compose -f docker-compose.yml exec -T b11k-postgis sh -c \
  'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --exit-on-error --single-transaction' \
  < "backups/backend/$SNAPSHOT/postgres.dump"
# Only after a successful restore, select a compatible app image:
./scripts/rollback-backend "$SNAPSHOT"
```

`pg_restore --clean` replaces objects present in the dump; it does not remove
unrelated objects added by a later migration. For a complete return across such a
migration, restore into a separate empty PostGIS database and verify it before
switching the app. Never use `docker compose down -v` as an update step.
