#!/usr/bin/env bash
# Shared by the backend commands. Bash 3.2+ (including macOS) is supported.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

compose() { docker compose -f "$repo_root/docker-compose.yml" "$@"; }
fail() { echo "$*" >&2; exit 1; }

require_backend() {
  command -v docker >/dev/null || fail "Docker is required."
  docker compose version >/dev/null || fail "Docker Compose v2 is required."
  # Validate without printing the interpolated configuration (it contains secrets).
  compose config --quiet
}

backend_lock() {
  umask 077
  backup_root="$repo_root/backups/backend"
  mkdir -p "$backup_root"
  operation_lock="$backup_root/.operation-lock"
  mkdir "$operation_lock" 2>/dev/null || fail "Another backend operation holds $operation_lock. If interrupted, verify no operation is running before removing that empty directory."
  trap 'rmdir "$operation_lock" 2>/dev/null || true' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
}

wait_healthy() {
  local service="$1" container health elapsed
  local timeout="${BACKEND_HEALTH_TIMEOUT:-180}"
  [[ "$timeout" =~ ^[1-9][0-9]*$ ]] || fail "BACKEND_HEALTH_TIMEOUT must be a positive number of seconds."
  for ((elapsed=0; elapsed<timeout; elapsed++)); do
    container="$(compose ps -a -q "$service")"
    if [[ -n "$container" ]]; then
      health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
      if [[ "$health" == healthy ]]; then return 0; fi
    fi
    sleep 1
  done
  echo "$service did not become healthy within ${timeout}s. Inspect ./scripts/logs-backend." >&2
  return 1
}

ensure_database() {
  # Keep an existing database container and volume; upgrades are a separate task.
  compose up -d --no-recreate b11k-postgis
  wait_healthy b11k-postgis
}

new_snapshot() {
  snapshot_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  snapshot_dir="$backup_root/$snapshot_id"
  mkdir "$snapshot_dir"
  local container previous_image
  container="$(compose ps -a -q b11k-app)"
  if [[ -n "$container" ]]; then
    # Snapshot the actual container's image, even after an earlier failed build.
    previous_image="$(docker inspect --format '{{.Image}}' "$container")"
    docker image tag "$previous_image" "b11k-backend-snapshot:$snapshot_id"
    printf '%s\n' "b11k-backend-snapshot:$snapshot_id" > "$snapshot_dir/previous-image.txt"
  fi
  cp docker-compose.yml "$snapshot_dir/compose.yml"
  cp config.docker.yaml "$snapshot_dir/config.docker.yaml"
}

dump_database() {
  echo "Backing up PostgreSQL to $snapshot_dir/postgres.dump"
  # Credentials stay inside the database container. Custom format is compressed
  # and readable by pg_restore; rename only after pg_dump exits successfully.
  compose exec -T b11k-postgis sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' > "$snapshot_dir/postgres.dump.partial"
  [[ -s "$snapshot_dir/postgres.dump.partial" ]] || fail "PostgreSQL produced an empty backup."
  mv "$snapshot_dir/postgres.dump.partial" "$snapshot_dir/postgres.dump"
  printf '%s\n' "$(date -u +%FT%TZ)" > "$snapshot_dir/backup-complete.txt"
}

select_snapshot() {
  snapshot_id="${1:-}"
  if [[ -z "$snapshot_id" ]]; then
    local candidate
    for candidate in "$backup_root"/*/; do
      [[ -f "$candidate/backup-complete.txt" && -f "$candidate/previous-image.txt" ]] || continue
      if [[ -z "$snapshot_id" || "$candidate/backup-complete.txt" -nt "$backup_root/$snapshot_id/backup-complete.txt" ]]; then
        snapshot_id="$(basename "$candidate")"
      fi
    done
  fi
  [[ "$snapshot_id" =~ ^[0-9]{8}T[0-9]{6}Z-[0-9]+$ ]] || fail "Specify a snapshot from backups/backend/ (YYYYMMDDTHHMMSSZ-PID)."
  snapshot_dir="$backup_root/$snapshot_id"
  [[ -f "$snapshot_dir/backup-complete.txt" ]] || fail "Snapshot $snapshot_id has no completed database backup."
}
