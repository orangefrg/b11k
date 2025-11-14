#!/bin/bash

# Build script for Strava Bike Tracker
# Compiles the Go application for Linux and places it in bin/

echo "🔨 Building Strava Bike Tracker for Linux"
echo "=========================================="

# Create bin directory if it doesn't exist
mkdir -p bin

# Set build variables
BINARY_NAME="strava-tracker"
BUILD_DIR="bin"
TARGET_OS="linux"
TARGET_ARCH="amd64"

echo "📁 Building from cmd/ directory..."
echo "🎯 Target: ${TARGET_OS}/${TARGET_ARCH}"
echo "📦 Output: ${BUILD_DIR}/${BINARY_NAME}"
echo ""

# Build the application
echo "⚙️  Compiling..."
GOOS=${TARGET_OS} GOARCH=${TARGET_ARCH} go build -o ${BUILD_DIR}/${BINARY_NAME} ./cmd

# Check if build was successful
if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    echo ""
    echo "📋 Build details:"
    echo "   Binary: ${BUILD_DIR}/${BINARY_NAME}"
    echo "   Size: $(du -h ${BUILD_DIR}/${BINARY_NAME} | cut -f1)"
    echo "   Architecture: ${TARGET_OS}/${TARGET_ARCH}"
    echo ""
    echo "🚀 To run the application:"
    echo "   ./${BUILD_DIR}/${BINARY_NAME}"
    echo ""
    echo "💡 Configuration is loaded from:"
    echo "   ${BUILD_DIR}/config.yaml"
else
    echo "❌ Build failed!"
    exit 1
fi
