#!/bin/sh
# Runs at container start (see Dockerfile). API_BASE_URL is the address your
# BROWSER uses to reach the gateway -- not a Docker-internal hostname.
echo "window.API_BASE_URL = \"${API_BASE_URL:-http://localhost:3000}\";" > /usr/share/nginx/html/config.js
echo "40-write-config: API_BASE_URL=${API_BASE_URL:-http://localhost:3000}"
