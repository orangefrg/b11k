# b11k - Strava Bike Activity Tracker

A web application for tracking and visualizing Strava bike activities. The application syncs activities from Strava, stores them in a PostgreSQL database with PostGIS support, and provides a web interface for viewing activities, segments, maps, and analytics.

Copyright (c) 2025 github.com/orangefrg

## Features

- **Strava Integration**: Authenticate with Strava and sync your bike activities
- **Activity Visualization**: View detailed activity information with interactive maps
- **Segment Tracking**: Track and analyze favorite segments across activities
- **Spatial Analysis**: Uses PostGIS for advanced geographic queries and operations
- **Web UI**: Modern web interface for browsing activities and segments
- **Real-time Sync**: Server-Sent Events (SSE) for real-time sync progress updates
- **Database Management**: Built-in database schema validation and migration tools

## Prerequisites

- **PostgreSQL** (version 12+) with **PostGIS** extension
- **Strava API credentials** (Client ID and Client Secret)
- **Mapbox token** (optional, for map visualization)
- **Docker** (for containerized deployment)

## Installation

For detailed installation instructions, including PostgreSQL setup, configuration, building, and deployment options, see [INSTALL.md](INSTALL.md).

## Quick Start

### Docker Compose

The Compose stack runs the web app and its own PostGIS database. PostgreSQL is exposed on host port `25432` to avoid colliding with common local Postgres containers.

```bash
cp .env.example .env
# Edit .env with Strava, Mapbox, and database secret values.
docker compose up --build
```

Open `http://localhost:8080`.

### Local Live Testing

Use the live stack while editing UI, templates, or Go code. It starts PostGIS and runs the app from the current checkout instead of from a baked image.

```bash
./live-test.sh
```

This creates `.env` from `.env.example` if it does not exist, starts the web UI at `http://localhost:8080`, and exposes PostGIS on `localhost:25432`. CSS and JavaScript changes are served from disk immediately. Template changes apply on browser refresh because live mode enables `B11K_DEV_RELOAD_TEMPLATES=true`. Go code changes require restarting the app container, for example:

```bash
docker compose -f docker-compose.live.yml restart b11k-live-app
```

For narrow screens, activity and segment detail pages default to showing stats and graphs before the map. Set `B11K_MOBILE_ACTIVITY_ORDER=map_first` in `.env` if you want the map first instead.

### Local Run

1. **Set up PostgreSQL database** (see [INSTALL.md](INSTALL.md#setting-up-postgresql-database))
2. **Create `config.yaml`** (see [INSTALL.md](INSTALL.md#configuration))
3. **Build the application** (see [INSTALL.md](INSTALL.md#building))
4. **Initialize database schema**: `./bin/b11k -setup-db`
5. **Run the application**: `./bin/b11k`
6. **Access the web interface** at `http://localhost:8080`

## Database Management Commands

The application provides several database management commands:

```bash
# Test database connection
./b11k -test-db

# Setup database tables (initial setup)
./b11k -setup-db

# Validate database schema
./b11k -validate-schema

# Force rebuild tables with schema mismatches (WARNING: deletes data)
./b11k -validate-schema -force-rebuild

# Truncate all tables (clear data)
./b11k -truncate-db

# Drop and recreate all tables
./b11k -recreate-db
```

## Usage

1. **Start the application** (see [INSTALL.md](INSTALL.md#running) for running options)
2. **Access the web interface** at `http://localhost:8080`
3. **Authenticate with Strava** by clicking the login button
4. **Sync activities** using the sync feature in the web UI
5. **Browse activities** and view detailed information, maps, and analytics
6. **Explore segments** to see your performance across favorite segments

## Architecture

- **Backend**: Go application with PostgreSQL/PostGIS
- **Frontend**: Server-rendered HTML templates with JavaScript
- **API**: RESTful API endpoints for activities and segments
- **Real-time**: Server-Sent Events for sync progress
- **Spatial Data**: PostGIS for geographic queries and operations

## Development

```bash
# Run locally during development
go run ./cmd

# Run with specific flags
go run ./cmd -test-db
go run ./cmd -setup-db
```

## License

See [LICENSE](LICENSE) file for details.
