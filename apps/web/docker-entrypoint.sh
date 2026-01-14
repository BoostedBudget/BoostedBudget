#!/bin/sh
set -e

# Runtime config injection for frontend
# This allows environment variables to be injected at container start time
# rather than build time, enabling the same image to be used across environments

# Determine HTTPS redirect port (default to 443 for production)
HTTPS_PORT="${HTTPS_PORT:-443}"

# Create runtime config file that will be loaded by the frontend
cat > /usr/share/nginx/html/config.js <<EOF
window.ENV = {
  API_URL: '${API_URL:-}',
  CLERK_PUBLISHABLE_KEY: '${CLERK_PUBLISHABLE_KEY:-${VITE_CLERK_PUBLISHABLE_KEY:-}}',
  ENVIRONMENT: '${ENVIRONMENT:-production}'
};
EOF

# Update nginx redirect to use correct HTTPS port
# Replace placeholder in nginx.conf with actual port
# Staging: 3443, Production: 443 (default)
sed -i "s/HTTPS_PORT_PLACEHOLDER/$HTTPS_PORT/g" /etc/nginx/nginx.conf

echo "Runtime configuration injected (HTTPS_PORT=$HTTPS_PORT)"

# Execute the CMD
exec "$@"
