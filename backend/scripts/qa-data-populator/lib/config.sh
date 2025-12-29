#!/bin/bash

# config.sh - Load configuration from backend/.env
# Reuses backend configuration (no credential duplication)

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
BACKEND_ENV_FILE="$BACKEND_DIR/.env"

# Load backend .env file
load_backend_config() {
  if [ ! -f "$BACKEND_ENV_FILE" ]; then
    log_error "Backend .env file not found: $BACKEND_ENV_FILE"
    log_info "Expected location: backend/.env"
    log_info "Please create backend/.env with DATABASE_URL and other config"
    exit 1
  fi

  log_debug "Loading config from: $BACKEND_ENV_FILE"

  # Export variables from .env file
  set -a  # Auto-export all variables
  source "$BACKEND_ENV_FILE"
  set +a  # Disable auto-export

  # Validate required variables
  if [ -z "$DATABASE_URL" ]; then
    log_error "DATABASE_URL not found in backend/.env"
    log_info "Example: DATABASE_URL=postgresql://user:pass@localhost:5432/dbname"
    exit 1
  fi

  log_debug "DATABASE_URL loaded successfully"
}

# Get tier-specific historical depth (in days)
get_historical_days() {
  local tier=$1

  case "$tier" in
    "free")
      echo "30"
      ;;
    "premium_lite")
      echo "365"
      ;;
    "premium"|"premium_plus")
      echo "730"
      ;;
    *)
      log_warning "Unknown tier: $tier, defaulting to 30 days"
      echo "30"
      ;;
  esac
}

# Check if required tools are installed
check_dependencies() {
  local missing=()

  # Check for yq (YAML parser)
  if ! command -v yq &> /dev/null; then
    missing+=("yq")
  fi

  # Check for psql (PostgreSQL client)
  if ! command -v psql &> /dev/null; then
    missing+=("psql")
  fi

  # Check for jq (JSON parser)
  if ! command -v jq &> /dev/null; then
    missing+=("jq")
  fi

  if [ ${#missing[@]} -gt 0 ]; then
    log_error "Missing required dependencies: ${missing[*]}"
    echo ""
    echo "Install instructions:"
    for tool in "${missing[@]}"; do
      case "$tool" in
        "yq")
          echo "  yq:   brew install yq    (or see https://github.com/mikefarah/yq)"
          ;;
        "psql")
          echo "  psql: brew install postgresql  (PostgreSQL client)"
          ;;
        "jq")
          echo "  jq:   brew install jq"
          ;;
      esac
    done
    exit 1
  fi

  log_debug "All dependencies installed: yq, psql, jq"
}

# Initialize configuration
init_config() {
  check_dependencies
  load_backend_config
}
