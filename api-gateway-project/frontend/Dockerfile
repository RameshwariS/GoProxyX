# Local-development image for the static storefront (Render serves the
# frontend as a Static Site instead and does not use this file).
FROM nginx:alpine
COPY index.html style.css app.js /usr/share/nginx/html/
# The official nginx image runs every executable *.sh in /docker-entrypoint.d/
# before starting nginx; this one writes config.js from $API_BASE_URL.
COPY 40-write-config.sh /docker-entrypoint.d/40-write-config.sh
RUN chmod +x /docker-entrypoint.d/40-write-config.sh
