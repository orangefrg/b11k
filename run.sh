#!/bin/bash

# Strava Bike Tracker Setup and Run Script

echo "🚴 Strava Bike Activity Tracker Setup"
echo "====================================="

# Check if environment variables are set
if [ -z "$STRAVA_CLIENT_ID" ] || [ -z "$STRAVA_CLIENT_SECRET" ]; then
    echo "❌ Error: Environment variables not set!"
    echo ""
    echo "Please set the following environment variables:"
    echo "export STRAVA_CLIENT_ID=your_client_id_here"
    echo "export STRAVA_CLIENT_SECRET=your_client_secret_here"
    echo "export STRAVA_REDIRECT_URI=http://localhost:8080/callback"
    echo ""
    echo "Optional (for map visualization):"
    echo "export MAPBOX_ACCESS_TOKEN=your_mapbox_token_here"
    echo ""
    echo "Get your credentials from:"
    echo "- Strava: https://www.strava.com/settings/api"
    echo "- Mapbox: https://account.mapbox.com/access-tokens/"
    exit 1
fi

echo "✅ Environment variables are set"
echo ""

# Try to run the application
echo "🚀 Starting Strava Bike Tracker..."
echo ""

go run main.go
