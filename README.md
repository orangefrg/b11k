# B11K - Strava Activity Tracker

B11K is a self-hosted Strava activity tracker with a Go/PostGIS backend, a web
UI, and a native iOS app. It syncs Strava bike activities, stores route and
metric data locally, and exposes activity maps, profiles, segments, matched
segment efforts, and a fog-of-war Discovered map.

Copyright (c) 2025 B11K contributors

## What It Does

- Syncs Strava activities through backend OAuth.
- Stores activities, streams, athlete/profile data, segments, and discovered
  coverage in PostgreSQL with PostGIS.
- Serves a web UI for activities, profile, segments, segment efforts, and the
  Discovered map.
- Provides a bearer-token mobile API for the iOS app.
- Runs a SwiftUI iOS app for activities, profile, segments, matched activities,
  effort details, sync, settings, and Discovered map metadata/map views.
- Uses MapLibre GL JS, Chart.js, server-rendered templates, and static assets.
- Supports live development with Docker Compose and a mounted checkout.

## Project Layout

```text
cmd/                         Go entrypoint and config/env loading
internal/pggeo/              PostGIS schema and geospatial queries
internal/strava/             Strava OAuth/API client
internal/sync/               Activity sync pipeline
internal/web/                Web UI, mobile API, auth, security middleware
web/templates/               Server-rendered HTML templates
web/static/                  CSS, JS, icons, local map style
iosApp/B11k/                 SwiftUI iOS app and iOS docs
```

## Prerequisites

- Docker and Docker Compose for the fastest local stack.
- PostgreSQL 12+ with PostGIS if running without Compose.
- Strava API credentials: client ID and client secret.
- Go 1.25+ for local builds; Go 1.26.4+ is recommended for production parity.
- Xcode for the iOS app.
- Optional release checks: `gitleaks`, `govulncheck`, `gosec`, `semgrep`,
  `trivy`, `osv-scanner`, and iOS static analysis tools.

## Quick Start

The default Compose stack runs the web app and a dedicated PostGIS database.
PostgreSQL is exposed on host port `25432` so it does not collide with common
local Postgres installs.

```bash
cp .env.example .env
# Edit .env with Strava credentials and a real database password.
docker compose up --build
```

Open `http://localhost:8080`.

Minimum `.env` values for local web testing:

```env
B11K_STRAVA_CLIENT_ID=...
B11K_STRAVA_CLIENT_SECRET=...
B11K_PG_PASSWORD=change-this-password
B11K_WEB_HOST=localhost
B11K_WEB_PROTOCOL=http
```

## Live Development

Use the live stack while editing Go, templates, CSS, or JavaScript:

```bash
./live-test.sh
```

This creates `.env` from `.env.example` if missing, starts the app at
`http://localhost:8080`, exposes PostGIS on `localhost:25432`, and runs the app
from the current checkout. CSS and JavaScript are served from disk immediately.
Template edits apply on refresh because live mode sets
`B11K_DEV_RELOAD_TEMPLATES=true`. Restart the app container after Go changes:

```bash
docker compose -f docker-compose.live.yml restart b11k-live-app
```

## iOS App

The iOS app lives in `iosApp/B11k/B11k.xcodeproj`.

For local device testing:

1. Start the backend on the same LAN as the iPhone.
2. Set `B11K_IOS_REDIRECT_URI` to the backend callback URL:

   ```env
   B11K_IOS_REDIRECT_URI=http://<your-lan-ip>:8080/api/mobile/auth/callback
   ```

3. In Strava app settings, set the callback domain to `<your-lan-ip>`.
4. Open the Xcode project, choose your local signing team, and set your own
   bundle identifier.
5. Run on device, open Settings in the app, enter the backend URL, and connect
   Strava.

The app enforces HTTPS for authenticated backend traffic in release builds.
Debug builds allow `http://localhost`, private LAN IPs, and `.local` hosts for
development. App session tokens are stored in the iOS Keychain.

For publishing details, see:

- `iosApp/B11k/IOS_DISTRIBUTION.md`
- `DEPLOYMENT_SECURITY.md`

## Web UI

Main pages:

- `/` - activities list
- `/activity/{id}` - activity detail, map, streams, graphs, segment creation
- `/profile` - athlete/profile summary
- `/segments` - segment list
- `/segment/{id}` - segment detail and matched activities
- `/discovered` - fog-of-war Discovered map when enabled

The web UI is intentionally single-user/self-hosted today. If exposed publicly,
keep it behind Cloudflare Access or an equivalent SSO gate unless web sessions
are redesigned for multi-user access.

## Mobile API

The native app uses `/api/mobile/*` endpoints. Auth starts through:

- `GET /api/mobile/auth/start`
- `POST /api/mobile/auth/exchange`
- `GET /api/mobile/auth/callback`
- `GET /api/mobile/auth/session`

Authenticated endpoints include:

- `GET /api/mobile/me`
- `GET /api/mobile/profile`
- `POST /api/mobile/logout`
- `POST /api/mobile/sync`
- `GET /api/mobile/activities`
- `GET /api/mobile/activities/{id}/route`
- `GET/POST /api/mobile/segments`
- `GET/PUT/DELETE /api/mobile/segments/{id}`
- `GET /api/mobile/segments/{id}/activities`
- `GET /api/mobile/segments/{id}/activities/{activity_id}`
- `GET/POST /api/mobile/discovered/*` when the Discovered map is enabled

All non-auth mobile endpoints require a B11K bearer session token. Browser
origins are rejected for mobile bearer endpoints, mobile API responses are
marked `no-store`, and public host separation is enforced when
`B11K_PUBLIC_API_HOST` is configured.

## Configuration

The app reads `config.yaml` first, then applies `B11K_*` environment overrides.
Docker uses `config.docker.yaml` for non-secret defaults and `.env` for secrets.

Common environment variables:

| Variable | Purpose |
| --- | --- |
| `B11K_STRAVA_CLIENT_ID` | Strava OAuth client ID |
| `B11K_STRAVA_CLIENT_SECRET` | Strava OAuth client secret, backend only |
| `B11K_STRAVA_REDIRECT_URI` | Web OAuth callback override |
| `B11K_IOS_REDIRECT_URI` | iOS/mobile OAuth callback |
| `B11K_PG_HOST`, `B11K_PG_PORT` | PostgreSQL host and port |
| `B11K_PG_DATABASE`, `B11K_PG_USER`, `B11K_PG_PASSWORD` | Database credentials |
| `B11K_WEB_HOST` | Public web host allowed by the backend |
| `B11K_PUBLIC_API_HOST` | Public mobile API host allowed by the backend |
| `B11K_WEB_PROTOCOL` | `http` for local, `https` for production |
| `B11K_WEB_HOST_PORT` | Host port for Docker Compose |
| `B11K_TOKEN_ENCRYPTION_KEY` | Base64 32-byte key for Strava token encryption |
| `B11K_DEV_RELOAD_TEMPLATES` | Reload templates from disk on refresh |
| `B11K_MOBILE_ACTIVITY_ORDER` | `stats_first` or `map_first` on narrow screens |
| `B11K_DISCOVERED_MAP_ENABLED` | Enables web/mobile Discovered map endpoints |
| `B11K_DISCOVERED_REVEAL_RADIUS_METERS` | Discovered reveal radius around routes |
| `B11K_DISCOVERED_SAMPLE_DISTANCE_METERS` | Discovered route sampling interval |

For production, generate a token encryption key and keep it in `.env` or your
secret manager:

```bash
openssl rand -base64 32
```

## Database Commands

Build the binary first, then run management commands from the repo root.

```bash
# Build
go build -o bin/b11k ./cmd

# Test database connection
./bin/b11k -test-db

# Setup database tables
./bin/b11k -setup-db

# Validate database schema
./bin/b11k -validate-schema

# Force rebuild tables with schema mismatches. This can delete data.
./bin/b11k -validate-schema -force-rebuild

# Truncate all tables
./bin/b11k -truncate-db

# Drop and recreate all tables
./bin/b11k -recreate-db
```

## Development Checks

```bash
# Go tests
go test ./...

# iOS simulator build without signing
xcodebuild \
  -project iosApp/B11k/B11k.xcodeproj \
  -scheme B11k \
  -sdk iphonesimulator \
  -configuration Debug \
  -derivedDataPath /tmp/b11k-xcodebuild \
  CODE_SIGNING_ALLOWED=NO \
  build

# iOS static analyzer
xcodebuild \
  -project iosApp/B11k/B11k.xcodeproj \
  -scheme B11k \
  -sdk iphonesimulator \
  -configuration Debug \
  -derivedDataPath /tmp/b11k-xcode-analyze \
  CODE_SIGNING_ALLOWED=NO \
  analyze
```

## Security Posture

Current hardening includes:

- Separate public web and native API host policy.
- HTTPS enforcement for public deployments.
- B11K bearer sessions for mobile API access.
- SHA-256 storage keys for mobile session lookup instead of raw bearer-token
  storage keys.
- Optional Strava token encryption at rest with `B11K_TOKEN_ENCRYPTION_KEY`.
- In-process rate limits for auth, mobile sync, segment/discovered reads, and
  expensive rebuild paths.
- Mobile API `Cache-Control: no-store`.
- Browser-origin rejection for bearer mobile API endpoints.
- iOS release protection against plain HTTP bearer requests.
- iOS ATS local-network exception instead of global arbitrary loads.
- Docker runtime image pinned, non-root user, and healthcheck.
- SRI and `crossorigin` on pinned CDN assets used by templates.

For recommended public exposure, see `DEPLOYMENT_SECURITY.md`. The intended
shape is:

- Web UI: `https://b11k.example.com`, protected by Cloudflare Access or
  equivalent SSO.
- Native API: `https://api.b11k.example.com`, not protected by browser SSO, and
  protected by B11K bearer sessions.

## Publishing Checklist

Before installing on another phone or shipping beyond local development:

1. Choose a real bundle identifier and Apple signing team in Xcode.
2. Use a production HTTPS backend URL.
3. Set production Strava callback domain and redirect URIs.
4. Set `B11K_WEB_PROTOCOL=https`.
5. Set `B11K_PUBLIC_API_HOST` for the native API host.
6. Set `B11K_WEB_HOST` for the web UI host.
7. Set `B11K_TOKEN_ENCRYPTION_KEY`.
8. Keep the web UI behind SSO unless web sessions are redesigned.
9. Run `go test ./...`, a secret scan, and an iOS build/analyze pass.

## More Docs

- `INSTALL.md` - detailed installation and PostgreSQL setup.
- `DEPLOYMENT_SECURITY.md` - public exposure, Cloudflare, and production smoke
  tests.
- `iosApp/B11k/IOS_DISTRIBUTION.md` - personal device, TestFlight, and App
  Store notes.

## License

See `LICENSE` for details.
