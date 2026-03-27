#!/usr/bin/env bash
echo "📦 ── Stage 1: Fetch Assets ──"
mkdir -p build/assets
echo '{"name":"hero","vertices":1024}' > build/assets/model.json
echo 'PLACEHOLDER_TEXTURE_DATA' > build/assets/texture.dat
echo '{"level":"dungeon","tiles":256}' > build/assets/level.json
echo "✅ Fetched 3 asset files"
sleep 2
