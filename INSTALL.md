# Installation Guide

This guide will walk you through installing and setting up b11k on your system.

Copyright (c) 2025 github.com/orangefrg

## Prerequisites

- **PostgreSQL** (version 12+) with **PostGIS** extension
- **Strava API credentials** (Client ID and Client Secret)
- **Mapbox token** (optional, for map visualization)
- **Docker** (for containerized deployment)

## Setting up PostgreSQL Database

Before creating your `config.yaml`, you need to set up a PostgreSQL database. This will provide the values you'll use in your configuration file.

### Option 1: Using Local PostgreSQL

1. **Install PostgreSQL** (if not already installed):
   ```bash
   # Ubuntu/Debian
   sudo apt-get update
   sudo apt-get install postgresql postgresql-contrib
   
   # macOS (using Homebrew)
   brew install postgresql
   
   # CentOS/RHEL/Fedora
   sudo dnf install postgresql postgresql-server
   # Or for older versions:
   sudo yum install postgresql postgresql-server
   ```

2. **Install PostGIS extension**:
   
   PostGIS is a separate package that extends PostgreSQL with geographic capabilities. Install it after PostgreSQL:
   
   ```bash
   # Ubuntu/Debian
   sudo apt-get install postgis postgresql-<version>-postgis-<version>
   # Example for PostgreSQL 15:
   sudo apt-get install postgis postgresql-15-postgis-3
   
   # macOS (using Homebrew)
   brew install postgis
   
   # CentOS/RHEL/Fedora
   sudo dnf install postgis
   # Or for older versions:
   sudo yum install postgis
   ```
   
   **Note**: Replace `<version>` with your PostgreSQL version. To check your PostgreSQL version:
   ```bash
   psql --version
   # Or
   sudo -u postgres psql -c "SELECT version();"
   ```
   
   For Ubuntu/Debian, you may need to find the exact package name:
   ```bash
   apt-cache search postgis | grep postgresql
   ```

3. **Start PostgreSQL service**:
   ```bash
   # Ubuntu/Debian (systemd)
   sudo systemctl start postgresql
   sudo systemctl enable postgresql
   
   # macOS (using Homebrew)
   brew services start postgresql
   
   # CentOS/RHEL/Fedora
   sudo systemctl start postgresql
   sudo systemctl enable postgresql
   ```

4. **Create the database and user** (if using local installation):
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
   
   -- Verify PostGIS installation
   SELECT PostGIS_Version();
   
   -- Grant privileges
   GRANT ALL PRIVILEGES ON DATABASE b11k_db TO b11k;
   
   -- Exit
   \q
   ```
   
   **Troubleshooting PostGIS installation**:
   
   If you get an error like `ERROR: could not open extension control file`, PostGIS may not be installed correctly:
   ```bash
   # Ubuntu/Debian - check if PostGIS is installed
   dpkg -l | grep postgis
   
   # If not found, install the correct version for your PostgreSQL
   sudo apt-get install postgresql-<version>-postgis-<version>
   
   # Example: For PostgreSQL 15 with PostGIS 3
   sudo apt-get install postgresql-15-postgis-3
   ```
   
   After installing PostGIS, you may need to restart PostgreSQL:
   ```bash
   sudo systemctl restart postgresql
   ```

5. **Note the connection details** for your `config.yaml`:
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

### Running as a Systemd Service (Production)

To run b11k as a background service that starts automatically on boot, you can use systemd.

#### Prerequisites

1. Build the application and place it in a system directory:
   ```bash
   # Create application directory
   sudo mkdir -p /opt/b11k
   
   # Copy the binary
   sudo cp bin/b11k /opt/b11k/
   
   # Copy web templates and static files
   sudo cp -r web /opt/b11k/
   
   # Copy config.yaml
   sudo cp config.yaml /opt/b11k/
   
   # Set ownership (create b11k user if it doesn't exist)
   sudo useradd -r -s /bin/false b11k 2>/dev/null || true
   sudo chown -R b11k:b11k /opt/b11k
   ```

2. **Create systemd service file**:
   
   Copy the service template and customize it:
   ```bash
   sudo cp b11k.service.template /etc/systemd/system/b11k.service
   sudo nano /etc/systemd/system/b11k.service
   ```
   
   Edit the service file and adjust the following if needed:
   - `User` and `Group`: The user/group to run the service as (default: `b11k`)
   - `WorkingDirectory`: Where the application files are located (default: `/opt/b11k`)
   - `ExecStart`: Path to the binary (default: `/opt/b11k/b11k`)
   - `MemoryMax`: Hard memory limit (default: `1024M`)
   - `MemoryHigh`: Soft memory limit (default: `512M`)
   - `CPUQuota`: Maximum CPU usage as percentage (default: `100%` = 1 CPU core)
   
   **Important**: Make sure the `WorkingDirectory` in the service file matches where your `config.yaml` is located, as the application reads `config.yaml` from the current working directory.
   
   **Resource Limits**: The service template includes default resource limits:
   - **Memory**: 1024MB hard limit, 512MB soft limit
   - **CPU**: 100% (1 CPU core)
   
   Adjust these values based on your server's capacity and expected load. For example:
   - High-traffic server: `MemoryMax=2G`, `CPUQuota=200%`
   - Low-resource server: `MemoryMax=512M`, `CPUQuota=50%`

3. **Reload systemd and enable the service**:
   ```bash
   # Reload systemd to recognize the new service
   sudo systemctl daemon-reload
   
   # Enable the service to start on boot
   sudo systemctl enable b11k
   
   # Start the service
   sudo systemctl start b11k
   ```

4. **Check service status**:
   ```bash
   # Check if the service is running
   sudo systemctl status b11k
   
   # View logs
   sudo journalctl -u b11k -f
   
   # View recent logs
   sudo journalctl -u b11k -n 50
   ```

5. **Service management commands**:
   ```bash
   # Start the service
   sudo systemctl start b11k
   
   # Stop the service
   sudo systemctl stop b11k
   
   # Restart the service
   sudo systemctl restart b11k
   
   # Check service status
   sudo systemctl status b11k
   
   # Disable auto-start on boot
   sudo systemctl disable b11k
   
   # Enable auto-start on boot
   sudo systemctl enable b11k
   ```

6. **Access the web interface** at `http://localhost:8080` (or your configured port)

**Troubleshooting**:

- If the service fails to start, check the logs:
  ```bash
  sudo journalctl -u b11k -n 100
  ```
  
- Verify the binary path and permissions:
  ```bash
  ls -la /opt/b11k/b11k
  sudo chmod +x /opt/b11k/b11k
  ```
  
- Ensure `config.yaml` exists in the working directory:
  ```bash
  ls -la /opt/b11k/config.yaml
  ```
  
- Check that the database is accessible from the service user:
  ```bash
  sudo -u b11k /opt/b11k/b11k -test-db
  ```

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

