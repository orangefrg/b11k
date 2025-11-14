# b11k - Strava Bike Activity Tracker

A web application for tracking and visualizing Strava bike activities. The application syncs activities from Strava, stores them in a PostgreSQL database with PostGIS support, and provides a web interface for viewing activities, segments, maps, and analytics.

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

## Setting up PostgreSQL Database

Before creating your `config.yaml`, you need to set up a PostgreSQL database. This will provide the values you'll use in your configuration file.

### Option 1: Using Local PostgreSQL

1. **Install PostgreSQL with PostGIS** (if not already installed):
   ```bash
   # Ubuntu/Debian
   sudo apt-get install postgresql postgresql-contrib postgis
   
   # macOS (using Homebrew)
   brew install postgresql postgis
   
   # Or use Docker
   docker run -d --name postgres-b11k \
     -e POSTGRES_USER=b11k \
     -e POSTGRES_PASSWORD=your_password \
     -e POSTGRES_DB=b11k_db \
     -p 5432:5432 \
     postgis/postgis:15-3.3
   ```

2. **Create the database and user** (if using local installation):
   ```bash
   # Connect to PostgreSQL as superuser
   sudo -u postgres psql
   ```
   
   Then run:
   ```sql
   -- Create user (if it doesn't exist)
   CREATE USER b11k WITH PASSWORD 'your_password';
   
   -- Create database
   CREATE DATABASE b11k_db OWNER b11k;
   
   -- Connect to the new database
   \c b11k_db
   
   -- Enable PostGIS extension
   CREATE EXTENSION IF NOT EXISTS postgis;
   
   -- Grant privileges
   GRANT ALL PRIVILEGES ON DATABASE b11k_db TO b11k;
   
   -- Exit
   \q
   ```

3. **Note the connection details** for your `config.yaml`:
   - **pg_ip**: `localhost` (or `127.0.0.1`)
   - **pg_port**: `5432` (default PostgreSQL port)
   - **pg_db**: `b11k_db` (or your chosen database name)
   - **pg_user**: `b11k` (or your chosen username)
   - **pg_secret**: `your_password` (the password you set)

### Option 2: Using Docker PostgreSQL

If you prefer to run PostgreSQL in Docker:

```bash
docker run -d \
  --name postgres-b11k \
  -e POSTGRES_USER=b11k \
  -e POSTGRES_PASSWORD=your_password \
  -e POSTGRES_DB=b11k_db \
  -p 5432:5432 \
  -v postgres-b11k-data:/var/lib/postgresql/data \
  postgis/postgis:15-3.3
```

Then enable PostGIS:
```bash
docker exec -it postgres-b11k psql -U b11k -d b11k_db -c "CREATE EXTENSION IF NOT EXISTS postgis;"
```

**Connection details for config.yaml**:
- **pg_ip**: `localhost` (when accessing from host) or `postgres-b11k` (when accessing from another Docker container)
- **pg_port**: `5432`
- **pg_db**: `b11k_db`
- **pg_user**: `b11k`
- **pg_secret**: `your_password`

### Option 3: Using Existing PostgreSQL Server

If you have an existing PostgreSQL server:

1. **Connect to your PostgreSQL server**
2. **Create the database and enable PostGIS**:
   ```sql
   CREATE DATABASE b11k_db;
   \c b11k_db
   CREATE EXTENSION IF NOT EXISTS postgis;
   ```
3. **Create a user** (if needed):
   ```sql
   CREATE USER b11k WITH PASSWORD 'your_password';
   GRANT ALL PRIVILEGES ON DATABASE b11k_db TO b11k;
   ```
4. **Use your server's connection details** in `config.yaml`

## Configuration

### Creating config.yaml

You need to create a `config.yaml` file in the root directory of the project. You can do this manually or use the template as a starting point.

#### Option 1: Copy from Template

```bash
cp config.yaml.template config.yaml
# Then edit config.yaml with your actual values
```

#### Option 2: Create Manually

Create a new file named `config.yaml` in the project root with the following content:

```yaml
strava_client_id: your_strava_client_id
strava_client_secret: your_strava_client_secret
strava_redirect_uri: http://localhost:8080/strava/callback
mapbox_token: your_mapbox_token
pg_ip: localhost
pg_port: 5432
pg_db: b11k_db
pg_user: b11k
pg_secret: your_password
web_port: 8080
```

#### Configuration Fields

- **strava_client_id**: Your Strava API Client ID (see below for how to get it)
- **strava_client_secret**: Your Strava API Client Secret
- **strava_redirect_uri**: The callback URL after Strava authentication (must match your Strava app settings)
- **mapbox_token**: Your Mapbox access token for map visualization (optional but recommended)
- **pg_ip**: PostgreSQL server hostname or IP address
- **pg_port**: PostgreSQL server port (default: 5432)
- **pg_db**: PostgreSQL database name
- **pg_user**: PostgreSQL username
- **pg_secret**: PostgreSQL password
- **web_port**: Port for the web server (default: 8080)

**Important**: Replace all placeholder values with your actual credentials and database information.

### Getting Strava API Credentials

1. Go to [Strava API Settings](https://www.strava.com/settings/api)
2. Create a new application
3. Set the Authorization Callback Domain (e.g., `localhost:8080`)
4. Copy your Client ID and Client Secret

### Getting Mapbox Token

1. Sign up at [Mapbox](https://account.mapbox.com/)
2. Create an access token at [Mapbox Access Tokens](https://account.mapbox.com/access-tokens/)
3. Copy your token

## Building

### Local Build

```bash
# Build the application
./build.sh

# Or manually:
go build -o bin/b11k ./cmd
```

### Docker Build

```bash
# Build the Docker image
docker build -t b11k:latest .

# Or with a specific tag
docker build -t b11k:v1.0.0 .
```

## Database Schema Initialization

After setting up your PostgreSQL database (see [Setting up PostgreSQL Database](#setting-up-postgresql-database) above) and creating your `config.yaml`, you need to initialize the database schema:

```bash
# Using Docker
docker run --rm -v $(pwd)/config.yaml:/app/config.yaml --network host b11k:latest -setup-db

# Using local binary
./bin/b11k -setup-db
```

## Running

### Local Execution

1. Ensure your PostgreSQL database is running and accessible
2. Create your `config.yaml` file
3. Run the application:
   ```bash
   ./bin/b11k
   ```
   Or use the run script:
   ```bash
   ./run.sh
   ```

4. Access the web interface at `http://localhost:8080`

### Docker Deployment

**Config File Handling**: The `config.yaml` file can be either:
- **Copied into the image** during build (if `config.yaml` exists in the build context)
- **Mounted as a volume** at runtime (recommended for production to keep secrets out of the image)

#### Using Docker Run

```bash
# Run the container with config.yaml mounted as volume (recommended)
docker run -d \
  --name b11k \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  --network host \
  b11k:latest
```

**Note**: Using `--network host` allows the container to access the PostgreSQL database on the host. If your database is in a separate container, use Docker networks instead.

If `config.yaml` was copied into the image during build, you can omit the volume mount:
```bash
docker run -d \
  --name b11k \
  -p 8080:8080 \
  --network host \
  b11k:latest
```

#### Using Docker Compose

Create a `docker-compose.yml`:

```yaml
version: '3.8'

services:
  postgres:
    image: postgis/postgis:15-3.3
    environment:
      POSTGRES_DB: b11k_db
      POSTGRES_USER: b11k
      POSTGRES_PASSWORD: your_password
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U b11k"]
      interval: 10s
      timeout: 5s
      retries: 5

  b11k:
    build: .
    image: b11k:latest
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
    environment:
      - PGIP=postgres
    restart: unless-stopped
    command: >
      sh -c "
        sleep 5 &&
        ./b11k -setup-db &&
        ./b11k
      "

volumes:
  postgres_data:
```

Then run:
```bash
docker-compose up -d
```

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

1. **Start the application** (see Running section above)
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

