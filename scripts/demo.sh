#!/usr/bin/env bash

set -e

# Floe Interactive Demo Script
# This script simulates a production environment without requiring any external API keys
# or dependencies other than the `floe` binary itself.

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧊 Floe Interactive Demo${NC}"
echo "This script will demonstrate Floe's routing, circuit breaking, and dashboard capabilities."
echo "No external API keys are needed. We use the internal 'mock' provider."
echo "------------------------------------------------------"

# Check if the binary exists
if ! command -v floe &> /dev/null; then
    echo -e "${RED}Error: 'floe' binary not found in PATH or current directory.${NC}"
    echo "Please build it first: 'make build' or 'go build ./cmd/floe'"
    exit 1
fi

echo -e "\n${YELLOW}Step 1: Starting the Floe Gateway...${NC}"
# Use the built-in demo configuration
floe demo &
FLOE_PID=$!

# Ensure we cleanup background process on exit
trap "kill $FLOE_PID 2>/dev/null" EXIT

sleep 2 # wait for server to bind

echo -e "\n${YELLOW}Step 2: Sending normal requests (Simulating OpenAI)...${NC}"
for i in {1..3}; do
  curl -s -X POST http://localhost:4400/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{
      "model": "gpt-4",
      "messages": [{"role": "user", "content": "Hello, mock provider!"}]
    }' | grep -o '"content":"[^"]*"' | head -n 1
  sleep 0.5
done

echo -e "\n${YELLOW}Step 3: Simulating Provider Failure & Circuit Breaker Failover...${NC}"
echo "We will now instruct the 'mock-fast' provider to start returning 500 errors."
echo "Watch the gateway instantly failover to the 'mock-slow' backup provider."

# Send a special header to trigger the mock's failure mode (demo only feature)
for i in {1..4}; do
  curl -s -X POST http://localhost:4400/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "X-Mock-Fail-Next: true" \
    -d '{
      "model": "gpt-4",
      "messages": [{"role": "user", "content": "Trigger failover!"}]
    }' | grep -o '"content":"[^"]*"' | head -n 1
  sleep 1
done

echo -e "\n${GREEN}Notice how the requests still succeeded?${NC}"
echo "The circuit breaker tripped on 'mock-fast' and automatically routed to 'mock-slow'."

echo -e "\n${YELLOW}Step 4: View the Dashboard${NC}"
echo "Floe has recorded all these requests, token usage, and failovers into local SQLite."
echo "If the Dashboard was built, it would now be available at http://localhost:4401"
echo ""
echo -e "${BLUE}Press Ctrl+C to stop the demo.${NC}"

wait $FLOE_PID
