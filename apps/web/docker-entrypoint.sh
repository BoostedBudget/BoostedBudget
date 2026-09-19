#!/bin/sh
set -e

# Runtime config injection for frontend
# This allows environment variables to be injected at container start time
# rather than build time, enabling the same image to be used across environments

# Determine HTTPS redirect port (default to 443 for production)
HTTPS_PORT="${HTTPS_PORT:-443}"

# Public SDK keys are opaque ASCII identifiers, not arbitrary JavaScript strings.
REVENUECAT_PUBLIC_API_KEY="${REVENUECAT_PUBLIC_API_KEY:-${VITE_REVENUECAT_PUBLIC_API_KEY:-}}"
case "$REVENUECAT_PUBLIC_API_KEY" in
  *[!a-zA-Z0-9_-]*) echo "Invalid RevenueCat public SDK key format" >&2; exit 1 ;;
esac

# Create runtime config file that will be loaded by the frontend
cat > /usr/share/nginx/html/config.js <<EOF
window.ENV = {
  REVENUECAT_PUBLIC_API_KEY: '${REVENUECAT_PUBLIC_API_KEY}',
  API_URL: '${API_URL:-}',
  CLERK_PUBLISHABLE_KEY: '${CLERK_PUBLISHABLE_KEY:-${VITE_CLERK_PUBLISHABLE_KEY:-}}',
  ENVIRONMENT: '${ENVIRONMENT:-production}',
  SENTRY_DSN: '${SENTRY_DSN:-}',
  POSTHOG_KEY: '${POSTHOG_KEY:-}',
  POSTHOG_HOST: '${POSTHOG_HOST:-https://us.i.posthog.com}',
  MCP_URL: '${MCP_URL:-}',
};
EOF

# Update nginx redirect to use correct HTTPS port
# Replace placeholder in nginx.conf with actual port
# Staging: 3443, Production: 443 (default)
sed -i "s/HTTPS_PORT_PLACEHOLDER/$HTTPS_PORT/g" /etc/nginx/nginx.conf

# Replace CSP report-uri placeholder with Sentry CSP endpoint URL.
# Format: https://sentry.io/api/<PROJECT_ID>/security/?sentry_key=<PUBLIC_KEY>
# If SENTRY_CSP_REPORT_URI is not set, the directive becomes report-uri (empty), which is a no-op.
# Uses | as delimiter because Sentry URLs contain /.
SENTRY_CSP_REPORT_URI="${SENTRY_CSP_REPORT_URI:-}"
sed -i "s|CSP_REPORT_URI_PLACEHOLDER|$SENTRY_CSP_REPORT_URI|g" /etc/nginx/nginx.conf

echo "Runtime configuration injected (HTTPS_PORT=$HTTPS_PORT, SENTRY_CSP_REPORT_URI=${SENTRY_CSP_REPORT_URI:-<not set>})"

# Execute the CMD
exec "$@"
