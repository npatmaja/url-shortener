#!/bin/bash

# URL Shortener API Test Script
# This script starts the server and tests all API endpoints

set -e

BASE_URL="http://localhost:8080"
SERVER_PID=""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null
        echo "Server stopped (PID: $SERVER_PID)"
    fi
}

trap cleanup EXIT

echo "========================================"
echo "URL Shortener API Test Script"
echo "========================================"

# Build the server
echo -e "\n${YELLOW}Building server...${NC}"
go build -o bin/server ./cmd/server
echo -e "${GREEN}Build complete!${NC}"

# Start the server in background
echo -e "\n${YELLOW}Starting server...${NC}"
./bin/server &
SERVER_PID=$!
echo "Server started with PID: $SERVER_PID"

# Wait for server to be ready
echo "Waiting for server to be ready..."
for i in {1..10}; do
    if curl -s "$BASE_URL/health" > /dev/null 2>&1; then
        echo -e "${GREEN}Server is ready!${NC}"
        break
    fi
    if [ $i -eq 10 ]; then
        echo -e "${RED}Server failed to start${NC}"
        exit 1
    fi
    sleep 1
done

echo ""
echo "========================================"
echo "Testing API Endpoints"
echo "========================================"

# Test 1: Health Check
echo -e "\n${YELLOW}1. Testing GET /health${NC}"
echo "Request: GET $BASE_URL/health"
HEALTH_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" "$BASE_URL/health")
HTTP_CODE=$(echo "$HEALTH_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
BODY=$(echo "$HEALTH_RESPONSE" | grep -v "HTTP_CODE")
echo "Response ($HTTP_CODE):"
echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
if [ "$HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}✓ Health check passed${NC}"
else
    echo -e "${RED}✗ Health check failed${NC}"
fi

# Test 2: Create Short URL
echo -e "\n${YELLOW}2. Testing POST /shorten${NC}"
echo "Request: POST $BASE_URL/shorten"
echo "Body: {\"long_url\": \"https://www.example.com/very/long/path/to/resource\"}"
SHORTEN_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -X POST "$BASE_URL/shorten" \
    -H "Content-Type: application/json" \
    -d '{"long_url": "https://www.example.com/very/long/path/to/resource"}')
HTTP_CODE=$(echo "$SHORTEN_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
BODY=$(echo "$SHORTEN_RESPONSE" | grep -v "HTTP_CODE")
echo "Response ($HTTP_CODE):"
echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
if [ "$HTTP_CODE" = "201" ]; then
    echo -e "${GREEN}✓ URL shortening passed${NC}"
    SHORT_CODE=$(echo "$BODY" | jq -r '.short_code')
    echo "Extracted short_code: $SHORT_CODE"
else
    echo -e "${RED}✗ URL shortening failed${NC}"
    SHORT_CODE=""
fi

# Test 3: Create Short URL with TTL
echo -e "\n${YELLOW}3. Testing POST /shorten with TTL${NC}"
echo "Request: POST $BASE_URL/shorten"
echo "Body: {\"long_url\": \"https://github.com\", \"ttl_seconds\": 3600}"
SHORTEN_TTL_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -X POST "$BASE_URL/shorten" \
    -H "Content-Type: application/json" \
    -d '{"long_url": "https://github.com", "ttl_seconds": 3600}')
HTTP_CODE=$(echo "$SHORTEN_TTL_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
BODY=$(echo "$SHORTEN_TTL_RESPONSE" | grep -v "HTTP_CODE")
echo "Response ($HTTP_CODE):"
echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
if [ "$HTTP_CODE" = "201" ]; then
    echo -e "${GREEN}✓ URL shortening with TTL passed${NC}"
else
    echo -e "${RED}✗ URL shortening with TTL failed${NC}"
fi

# Test 4: Get Stats
if [ -n "$SHORT_CODE" ]; then
    echo -e "\n${YELLOW}4. Testing GET /stats/{code}${NC}"
    echo "Request: GET $BASE_URL/stats/$SHORT_CODE"
    STATS_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" "$BASE_URL/stats/$SHORT_CODE")
    HTTP_CODE=$(echo "$STATS_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
    BODY=$(echo "$STATS_RESPONSE" | grep -v "HTTP_CODE")
    echo "Response ($HTTP_CODE):"
    echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
    if [ "$HTTP_CODE" = "200" ]; then
        echo -e "${GREEN}✓ Stats retrieval passed${NC}"
    else
        echo -e "${RED}✗ Stats retrieval failed${NC}"
    fi
else
    echo -e "\n${YELLOW}4. Skipping stats test - no short code available${NC}"
fi

# Test 5: Redirect
if [ -n "$SHORT_CODE" ]; then
    echo -e "\n${YELLOW}5. Testing GET /s/{code} (redirect)${NC}"
    echo "Request: GET $BASE_URL/s/$SHORT_CODE"
    REDIRECT_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -o /dev/null "$BASE_URL/s/$SHORT_CODE")
    HTTP_CODE=$(echo "$REDIRECT_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
    echo "Response: HTTP $HTTP_CODE (302 = redirect)"
    if [ "$HTTP_CODE" = "302" ]; then
        LOCATION=$(curl -s -I "$BASE_URL/s/$SHORT_CODE" | grep -i "Location" | tr -d '\r')
        echo "$LOCATION"
        echo -e "${GREEN}✓ Redirect passed${NC}"
    else
        echo -e "${RED}✗ Redirect failed (expected 302)${NC}"
    fi
else
    echo -e "\n${YELLOW}5. Skipping redirect test - no short code available${NC}"
fi

# Test 6: Get Stats again (should show click_count increased)
if [ -n "$SHORT_CODE" ]; then
    echo -e "\n${YELLOW}6. Testing GET /stats/{code} (after redirect)${NC}"
    echo "Request: GET $BASE_URL/stats/$SHORT_CODE"
    STATS_RESPONSE2=$(curl -s -w "\nHTTP_CODE:%{http_code}" "$BASE_URL/stats/$SHORT_CODE")
    HTTP_CODE=$(echo "$STATS_RESPONSE2" | grep "HTTP_CODE" | cut -d: -f2)
    BODY=$(echo "$STATS_RESPONSE2" | grep -v "HTTP_CODE")
    echo "Response ($HTTP_CODE):"
    echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
    CLICK_COUNT=$(echo "$BODY" | jq -r '.click_count')
    echo "Click count: $CLICK_COUNT (should be >= 1 after redirect)"
    if [ "$HTTP_CODE" = "200" ] && [ "$CLICK_COUNT" -ge 1 ]; then
        echo -e "${GREEN}✓ Click tracking passed${NC}"
    else
        echo -e "${RED}✗ Click tracking failed${NC}"
    fi
fi

# Test 7: Invalid URL
echo -e "\n${YELLOW}7. Testing POST /shorten with invalid URL${NC}"
echo "Request: POST $BASE_URL/shorten"
echo "Body: {\"long_url\": \"not-a-valid-url\"}"
INVALID_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" -X POST "$BASE_URL/shorten" \
    -H "Content-Type: application/json" \
    -d '{"long_url": "not-a-valid-url"}')
HTTP_CODE=$(echo "$INVALID_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
BODY=$(echo "$INVALID_RESPONSE" | grep -v "HTTP_CODE")
echo "Response ($HTTP_CODE):"
echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
if [ "$HTTP_CODE" = "400" ]; then
    echo -e "${GREEN}✓ Invalid URL validation passed${NC}"
else
    echo -e "${RED}✗ Invalid URL validation failed (expected 400)${NC}"
fi

# Test 8: Not found short code
echo -e "\n${YELLOW}8. Testing GET /s/{code} with non-existent code${NC}"
echo "Request: GET $BASE_URL/s/notfound"
NOTFOUND_RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" "$BASE_URL/s/notfound")
HTTP_CODE=$(echo "$NOTFOUND_RESPONSE" | grep "HTTP_CODE" | cut -d: -f2)
BODY=$(echo "$NOTFOUND_RESPONSE" | grep -v "HTTP_CODE")
echo "Response ($HTTP_CODE):"
echo "$BODY" | jq . 2>/dev/null || echo "$BODY"
if [ "$HTTP_CODE" = "404" ]; then
    echo -e "${GREEN}✓ Not found handling passed${NC}"
else
    echo -e "${RED}✗ Not found handling failed (expected 404)${NC}"
fi

echo ""
echo "========================================"
echo -e "${GREEN}All API tests completed!${NC}"
echo "========================================"
