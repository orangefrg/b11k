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

