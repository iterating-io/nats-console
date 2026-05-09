#!/bin/sh
set -e

# Start API in background
nats-console-api &
API_PID=$!

# Start Nginx in foreground
nginx -g "daemon off;"

# If Nginx stops, terminate API
kill $API_PID 2>/dev/null || true
