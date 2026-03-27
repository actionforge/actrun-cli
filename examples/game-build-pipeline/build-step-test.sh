#!/usr/bin/env bash
echo "🧪 ── Stage 3: Test ──"
PASS=0
FAIL=0
for f in model.json texture.dat level.json manifest.txt; do
  if [ -f "build/out/$f" ]; then
    echo "  ✅ $f present"
    PASS=$((PASS+1))
  else
    echo "  ❌ $f missing"
    FAIL=$((FAIL+1))
  fi
done
echo "🏁 Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
sleep 2
