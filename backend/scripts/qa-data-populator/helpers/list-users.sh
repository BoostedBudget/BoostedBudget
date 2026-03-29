#!/bin/bash

# list-users.sh - List all users in the database with their details
# Helps find users to populate with data

set -euo pipefail

# Script directory
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PARENT_DIR="$(dirname "$DIR")"

# Load dependencies
source "$PARENT_DIR/lib/logging.sh"
source "$PARENT_DIR/lib/config.sh"

# Usage message
usage() {
  cat << EOF
Usage: $(basename "$0") [options]

List all users in the staging database with their details.

Options:
  --limit N        Limit results to N users (default: 20)
  --email PATTERN  Filter by email pattern (e.g., '%@example.com')
  --all            Show all users (no limit)

Examples:
  $(basename "$0")                           # List 20 most recent users
  $(basename "$0") --limit 50                # List 50 most recent users
  $(basename "$0") --email 'demo%'           # List users with email starting with 'demo'
  $(basename "$0") --all                     # List all users

EOF
  exit 1
}

# Parse arguments
LIMIT=20
EMAIL_FILTER=""
SHOW_ALL=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --limit)
      LIMIT="$2"
      shift 2
      ;;
    --email)
      EMAIL_FILTER="$2"
      shift 2
      ;;
    --all)
      SHOW_ALL=true
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      log_error "Unknown argument: $1"
      usage
      ;;
  esac
done

# Initialize
init_config

# Validate database
if ! validate_db_connection; then
  exit 1
fi

# Build query
WHERE_CLAUSE=""
if [ -n "$EMAIL_FILTER" ]; then
  WHERE_CLAUSE="WHERE u.email LIKE '$EMAIL_FILTER'"
fi

LIMIT_CLAUSE=""
if [ "$SHOW_ALL" = "false" ]; then
  LIMIT_CLAUSE="LIMIT $LIMIT"
fi

# Query users
log_info "Querying users from database..."
echo ""

SQL="
SELECT
  u.email,
  u.subscription_tier,
  ui.clerk_user_id,
  u.created_at::date as joined_date,
  COUNT(DISTINCT li.id) as linked_items,
  COUNT(DISTINCT a.id) as accounts,
  COUNT(DISTINCT t.id) as transactions
FROM users u
LEFT JOIN user_identities ui ON ui.user_id = u.id AND ui.is_primary = true
LEFT JOIN linked_items li ON li.user_id = u.id
LEFT JOIN accounts a ON a.user_id = u.id
LEFT JOIN transactions t ON t.user_id = u.id
$WHERE_CLAUSE
GROUP BY u.email, u.subscription_tier, ui.clerk_user_id, u.created_at
ORDER BY u.created_at DESC
$LIMIT_CLAUSE;
"

# Execute query
psql "$DATABASE_URL" -c "$SQL"

echo ""
log_success "Query complete"
echo ""
log_info "Tip: Use these emails in your YAML scenarios (setup.user.email)"
log_info "     For existing users, set mode: existing to preserve Clerk login"
echo ""

exit 0
