// Copyright (c) 2025 github.com/orangefrg
// Licensed under the Apache License, Version 2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"b11k/internal/pggeo"
	"b11k/internal/strava"
	"b11k/internal/sync"
	"b11k/internal/web"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"
)

type Config struct {
	StravaClientID     string `yaml:"strava_client_id"`
	StravaClientSecret string `yaml:"strava_client_secret"`
	StravaRedirectURI  string `yaml:"strava_redirect_uri"`
	MapboxToken        string `yaml:"mapbox_token"`
	PGIP               string `yaml:"pg_ip"`
	PGPort             string `yaml:"pg_port"`
	PGUser             string `yaml:"pg_user"`
	PGPassword         string `yaml:"pg_secret"`
	PGDatabase         string `yaml:"pg_db"`
	WebHost            string `yaml:"web_host"`
	WebPort            string `yaml:"web_port"`
	WebProtocol        string `yaml:"web_protocol"` // "http" or "https" - use "https" when behind Cloudflare Tunnel or reverse proxy
}

func main() {
	setupDB := flag.Bool("setup-db", false, "Set up database tables and exit")
	testDB := flag.Bool("test-db", false, "Test database connection and exit")
	truncateDB := flag.Bool("truncate-db", false, "Truncate all database tables and exit")
	recreateDB := flag.Bool("recreate-db", false, "Drop and recreate all database tables and exit")
	validateSchema := flag.Bool("validate-schema", false, "Validate database schema and exit")
	forceRebuild := flag.Bool("force-rebuild", false, "Force rebuild tables with schema mismatches (WARNING: will delete data)")
	// serve flag deprecated; server runs by default
	_ = flag.Bool("serve", false, "Run web server UI (default)")
	flag.Parse()

	config := Config{}
	yamlFile, err := os.ReadFile("config.yaml")
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}
	yaml.Unmarshal(yamlFile, &config)

	// Construct redirect URI from host and port if not explicitly provided
	if config.StravaRedirectURI == "" {
		webHost := config.WebHost
		if webHost == "" {
			webHost = "localhost"
		}
		webPort := config.WebPort
		if webPort == "" {
			webPort = "8080"
		}
		// Determine protocol - default to http, but use web_protocol if set
		protocol := "http"
		if config.WebProtocol == "https" {
			protocol = "https"
		}
		
		// For standard ports (80 for HTTP, 443 for HTTPS), omit port in URL
		// For non-standard ports, include port in URL
		var redirectURI string
		if (protocol == "http" && webPort == "80") || (protocol == "https" && webPort == "443") {
			// Standard port - omit from URL
			redirectURI = fmt.Sprintf("%s://%s/strava/callback", protocol, webHost)
		} else if protocol == "https" {
			// HTTPS with non-standard port - but if behind proxy, usually omit port
			// For Cloudflare Tunnel and most reverse proxies, HTTPS URLs don't include port
			redirectURI = fmt.Sprintf("%s://%s/strava/callback", protocol, webHost)
		} else {
			// HTTP with non-standard port - include port
			redirectURI = fmt.Sprintf("%s://%s:%s/strava/callback", protocol, webHost, webPort)
		}
		
		config.StravaRedirectURI = redirectURI
		log.Printf("📝 Constructed Strava redirect URI: %s", config.StravaRedirectURI)
		if protocol == "http" {
			log.Printf("💡 If behind Cloudflare Tunnel or reverse proxy with HTTPS, set web_protocol: https in config.yaml")
		}
	}

	// Connect to database
	ctx := context.Background()
	conn, err := pggeo.Connect(ctx, config.PGUser, config.PGPassword, config.PGIP, config.PGPort, config.PGDatabase)
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}
	defer conn.Close(ctx)

	// Handle command line flags
	if *setupDB {
		setupDatabase(ctx, conn)
		return
	}

	if *testDB {
		testDatabase(ctx, conn)
		return
	}

	if *truncateDB {
		truncateDatabase(ctx, conn)
		return
	}

	if *recreateDB {
		recreateDatabase(ctx, conn)
		return
	}

	if *validateSchema {
		validateDatabaseSchema(ctx, conn, *forceRebuild)
		return
	}

	// Validate schema before starting server
	log.Printf("🔍 Validating database schema...")
	if err := pggeo.ValidateAndMigrateSchema(ctx, conn, *forceRebuild); err != nil {
		log.Fatalf("Error validating/migrating database schema: %v", err)
	}
	log.Printf("✅ Schema validation completed")

	// Default behavior: serve web UI (if -serve is provided or not)
	web.RunServer(ctx, web.Config{
		StravaClientID:     config.StravaClientID,
		StravaClientSecret: config.StravaClientSecret,
		StravaRedirectURI:  config.StravaRedirectURI,
		MapboxToken:        config.MapboxToken,
		PGIP:               config.PGIP,
		PGPort:             config.PGPort,
		PGUser:             config.PGUser,
		PGPassword:         config.PGPassword,
		PGDatabase:         config.PGDatabase,
		WebHost:            config.WebHost,
		WebPort:            config.WebPort,
		WebProtocol:        config.WebProtocol,
	})
}

func setupDatabase(ctx context.Context, conn *pgx.Conn) {
	log.Printf("🔧 Setting up database tables...")
	if err := pggeo.CreateTables(ctx, conn); err != nil {
		log.Fatalf("Error creating database tables: %v", err)
	}
	log.Printf("✅ Database setup completed successfully!")
	log.Printf("📊 Created tables:")
	log.Printf("   - activity_summaries")
	log.Printf("   - activity_geometries")
	log.Printf("   - point_samples")
	log.Printf("   - favorite_segments")
	log.Printf("🔧 Created helper functions for spatial operations")
}

func testDatabase(ctx context.Context, conn *pgx.Conn) {
	log.Printf("🧪 Testing database connection...")

	// Test basic connection
	var version string
	err := conn.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		log.Fatalf("Error querying database: %v", err)
	}
	log.Printf("✅ Database version: %s", version)

	// Test PostGIS availability
	var postgisVersion string
	err = conn.QueryRow(ctx, "SELECT PostGIS_Version()").Scan(&postgisVersion)
	if err != nil {
		log.Printf("⚠️ PostGIS not available: %v", err)
		log.Printf("ℹ️ You can still use the application, but spatial functions will be limited")
	} else {
		log.Printf("✅ PostGIS version: %s", postgisVersion)
	}

	// Test table existence
	var count int
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM activity_summaries").Scan(&count)
	if err != nil {
		log.Printf("⚠️ Tables don't exist yet. Run with -setup-db flag to create them.")
	} else {
		log.Printf("✅ Tables exist, current activity count: %d", count)
	}

	log.Printf("🎉 Database test completed successfully!")
}

func truncateDatabase(ctx context.Context, conn *pgx.Conn) {
	log.Printf("🗑️ Truncating database tables...")
	if err := pggeo.TruncateTables(ctx, conn); err != nil {
		log.Fatalf("Error truncating database tables: %v", err)
	}
	log.Printf("✅ Database truncated successfully!")
}

func recreateDatabase(ctx context.Context, conn *pgx.Conn) {
	log.Printf("🔄 Dropping and recreating database tables...")
	if err := pggeo.DropAndRecreateTables(ctx, conn); err != nil {
		log.Fatalf("Error recreating database tables: %v", err)
	}
	log.Printf("✅ Database recreated successfully!")
	log.Printf("📊 Recreated tables:")
	log.Printf("   - activity_summaries")
	log.Printf("   - activity_geometries")
	log.Printf("   - point_samples")
	log.Printf("   - favorite_segments")
	log.Printf("   - segment_activity_matches (cache table)")
	log.Printf("ℹ️ All tables have been dropped and recreated from scratch")
}

func validateDatabaseSchema(ctx context.Context, conn *pgx.Conn, forceRebuild bool) {
	log.Printf("🔍 Validating database schema...")
	if forceRebuild {
		log.Printf("⚠️ Force rebuild enabled - tables with mismatches will be dropped and recreated")
	}
	if err := pggeo.ValidateAndMigrateSchema(ctx, conn, forceRebuild); err != nil {
		log.Fatalf("Error validating/migrating database schema: %v", err)
	}
	log.Printf("✅ Schema validation completed successfully!")
	log.Printf("📊 All tables validated and migrated as needed")
}

func runSync(ctx context.Context, config Config) {
	// Authenticate with Strava
	authCfg := strava.NewStravaAuthConfig(config.StravaClientID, config.StravaClientSecret, config.StravaRedirectURI)
	token, err := strava.ConsoleLogin(*authCfg)
	if err != nil {
		log.Fatalf("Error logging in: %v", err)
	}

	// Create database tables if they don't exist
	log.Printf("🔧 Setting up database tables...")
	conn, err := pggeo.Connect(ctx, config.PGUser, config.PGPassword, config.PGIP, config.PGPort, config.PGDatabase)
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}
	defer conn.Close(ctx)

	if err := pggeo.CreateTables(ctx, conn); err != nil {
		log.Fatalf("Error creating database tables: %v", err)
	}
	log.Printf("✅ Database tables ready")

	// Create sync configuration
	syncConfig := sync.SyncConfig{
		StravaAccessToken: token,
		DatabaseConfig: sync.DatabaseConfig{
			Host:     config.PGIP,
			Port:     config.PGPort,
			User:     config.PGUser,
			Password: config.PGPassword,
			Database: config.PGDatabase,
		},
		Timeframe: sync.TimeframeConfig{
			StartTime: time.Now().AddDate(0, 0, -30), // Last 30 days
			EndTime:   time.Time{},                   // No end time (current)
		},
	}

	// Perform the sync (no progress callback for CLI)
	result, err := sync.SyncActivitiesFromStravaWithRetry(ctx, syncConfig, 3, nil)
	if err != nil {
		log.Fatalf("Error syncing activities: %v", err)
	}

	// Print results
	fmt.Printf("\n🎉 Sync completed successfully!\n")
	fmt.Printf("📊 Results:\n")
	fmt.Printf("   - Total activities found: %d\n", result.TotalActivitiesFound)
	fmt.Printf("   - Existing activities: %d\n", result.ExistingActivities)
	fmt.Printf("   - New activities: %d\n", result.NewActivities)
	fmt.Printf("   - Successfully processed: %d\n", result.SuccessfullyProcessed)
	fmt.Printf("   - Failed activities: %d\n", len(result.FailedActivities))
	fmt.Printf("   - Processing time: %v\n", result.ProcessingTime)

	if len(result.FailedActivities) > 0 {
		fmt.Printf("❌ Failed activity IDs: %v\n", result.FailedActivities)
	}
}
