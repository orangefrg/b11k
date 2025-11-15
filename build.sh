#!/bin/bash

# Build script for Strava Bike Tracker
# Compiles the Go application for Linux and places it in bin/

echo "🔨 Building Strava Bike Tracker for Linux"
echo "=========================================="

# Create bin directory if it doesn't exist
mkdir -p bin

# Set build variables
BINARY_NAME="b11k"
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

# Copy config.yaml to bin directory if it exists
if [ -f "config.yaml" ]; then
    echo "📋 Copying config.yaml to ${BUILD_DIR}/..."
    cp config.yaml ${BUILD_DIR}/config.yaml
    echo "✅ config.yaml copied to ${BUILD_DIR}/"
else
    echo "⚠️  Warning: config.yaml not found in root directory"
    echo "   You'll need to create ${BUILD_DIR}/config.yaml before running the application"
fi

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
