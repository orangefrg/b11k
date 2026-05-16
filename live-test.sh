#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

if ! docker compose version >/dev/null 2>&1; then
  echo "Docker Compose is required for live testing."
  exit 1
fi

if [ ! -f ".env" ]; then
  cp .env.example .env
  echo "Created .env from .env.example."
  echo "Edit .env with Strava values when you need login."
fi

echo "Starting b11k live stack..."
echo "Web UI: http://localhost:${B11K_WEB_HOST_PORT:-8080}"
echo "PostGIS host port: ${B11K_PG_HOST_PORT:-25432}"

docker compose -f docker-compose.live.yml up
