#!/bin/sh
set -e

# Runtime config injection for frontend
# This allows environment variables to be injected at container start time
# rather than build time, enabling the same image to be used across environments

# Create runtime config file that will be loaded by the frontend
cat > /usr/share/nginx/html/config.js <<EOF
window.ENV = {
  API_URL: '${API_URL:-http://localhost:8080}',
  AUTH0_DOMAIN: '${AUTH0_DOMAIN:-}',
  AUTH0_CLIENT_ID: '${AUTH0_CLIENT_ID:-}',
  AUTH0_AUDIENCE: '${AUTH0_AUDIENCE:-}',
  ENVIRONMENT: '${ENVIRONMENT:-production}'
};
EOF

echo "Runtime configuration injected"

# Execute the CMD
exec "$@"
