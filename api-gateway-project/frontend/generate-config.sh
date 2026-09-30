#!/bin/sh
# Writes config.js from config.template.js, substituting the gateway's URL.
# Render runs this as the static site's build command. The URL comes from,
# in order of priority:
#   1. API_BASE_URL   a full URL you set yourself
#   2. GATEWAY_HOST   the gateway's Render slug (the Blueprint fills this in);
#                     the public URL is https://<slug>.onrender.com
#   3. http://localhost:3000   for local development
# Locally:  API_BASE_URL=http://localhost:3000 sh generate-config.sh
set -e
cd "$(dirname "$0")"
if [ -z "$API_BASE_URL" ] && [ -n "$GATEWAY_HOST" ]; then
  API_BASE_URL="https://${GATEWAY_HOST}.onrender.com"
fi
API_BASE_URL="${API_BASE_URL:-http://localhost:3000}"
# strip any trailing slash so the app can safely do API_BASE_URL + "/items"
API_BASE_URL="${API_BASE_URL%/}"
sed "s|__API_BASE_URL__|${API_BASE_URL}|g" config.template.js > config.js
echo "generate-config: API_BASE_URL=${API_BASE_URL}"
