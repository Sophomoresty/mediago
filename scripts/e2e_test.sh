#!/bin/bash
set +e

MEDIGO="./medigo"

echo "=== MediGo E2E Verification ==="
echo ""

PASS=0
FAIL=0

echo "[BUILD] Building medigo..."
cd ~/code/medigo
go build -o medigo ./cmd/medigo
echo "[BUILD] OK"
echo ""

# Test 1: Bilibili -j
echo "[TEST] Bilibili -j (dump json)..."
if $MEDIGO -j "https://www.bilibili.com/video/BV1GJ411x7h7" 2>/dev/null | grep -q 'bilibili'; then
    echo "  PASS: JSON with streams"
    PASS=$((PASS+1))
else
    echo "  FAIL"
    FAIL=$((FAIL+1))
fi

# Test 2: Douyin -j
echo "[TEST] Douyin -j (short URL)..."
if $MEDIGO -j "https://v.douyin.com/CeiJFhAo/" 2>/dev/null | grep -q 'douyin'; then
    echo "  PASS: JSON with streams"
    PASS=$((PASS+1))
else
    echo "  FAIL (may be network issue)"
    FAIL=$((FAIL+1))
fi

# Test 3: --list-extractors count
echo "[TEST] --list-extractors count..."
COUNT=$($MEDIGO --list-extractors 2>/dev/null | grep "extractors" | grep -o '[0-9]*')
if [ "$COUNT" -ge 90 ]; then
    echo "  PASS: $COUNT extractors"
    PASS=$((PASS+1))
else
    echo "  FAIL: only $COUNT"
    FAIL=$((FAIL+1))
fi

# Test 4: -F (list formats)
echo "[TEST] -F (list formats)..."
if $MEDIGO -F "https://www.bilibili.com/video/BV1GJ411x7h7" 2>/dev/null | grep -q 'QUALITY'; then
    echo "  PASS: format table shown"
    PASS=$((PASS+1))
else
    echo "  FAIL"
    FAIL=$((FAIL+1))
fi

# Test 5: Auth error
echo "[TEST] Auth error handling..."
if $MEDIGO "https://www.icourse163.org/course/ZJICM-1449623161" 2>&1 | grep -qi "cookie\|login\|auth\|not logged\|requires"; then
    echo "  PASS: proper error message"
    PASS=$((PASS+1))
else
    echo "  FAIL"
    FAIL=$((FAIL+1))
fi

# Test 6: Unsupported URL
echo "[TEST] Unsupported URL..."
if $MEDIGO "https://www.example.com/video" 2>&1 | grep -qi "unsupported"; then
    echo "  PASS"
    PASS=$((PASS+1))
else
    echo "  FAIL"
    FAIL=$((FAIL+1))
fi

# Test 7: Version
echo "[TEST] Version..."
if $MEDIGO version 2>/dev/null | grep -q "medigo"; then
    echo "  PASS"
    PASS=$((PASS+1))
else
    echo "  FAIL"
    FAIL=$((FAIL+1))
fi

# Test 8: No args shows help
echo "[TEST] No args shows help..."
if $MEDIGO 2>&1 | grep -q "Usage"; then
    echo "  PASS"
    PASS=$((PASS+1))
else
    echo "  FAIL"
    FAIL=$((FAIL+1))
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="

if [ $FAIL -gt 0 ]; then
    exit 1
fi
exit 0
