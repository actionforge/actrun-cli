#!/usr/bin/env bash
echo "🔨 ── Stage 2: Build ──"
mkdir -p build/out
cp build/assets/* build/out/
echo "COMPILED=true" > build/out/manifest.txt
echo "VERSION=1.0.0" >> build/out/manifest.txt
echo "BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> build/out/manifest.txt
echo "✅ Build complete: $(ls build/out | wc -l | tr -d ' ') files"
sleep 2
