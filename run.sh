#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# Strava Bike Tracker Setup and Run Script

echo "🚴 Strava Bike Activity Tracker Setup"
echo "====================================="

# Check if config.yaml exists
if [ ! -f "config.yaml" ]; then
    echo "❌ Error: config.yaml not found!"
    echo ""
    echo "Please create config.yaml in the root directory."
    echo "You can copy from the template:"
    echo "  cp config.yaml.template config.yaml"
    echo ""
    echo "Then edit config.yaml with your actual values."
    echo "See INSTALL.md for detailed configuration instructions."
    exit 1
fi

echo "✅ config.yaml found"
echo ""

# Try to run the application
echo "🚀 Starting Strava Bike Tracker..."
echo ""

exec go run ./cmd "$@"
