#!/bin/bash

# logging.sh - Structured logging with color-coded output
# Used by all staging data populator scripts

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'  # No Color

# Log levels
log_info() {
  echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
  echo -e "${GREEN}✅ $1${NC}"
}

log_error() {
  echo -e "${RED}❌ $1${NC}" >&2
}

log_warning() {
  echo -e "${YELLOW}⚠️  $1${NC}"
}

log_debug() {
  if [ "${DEBUG:-false}" = "true" ]; then
    echo -e "${CYAN}🔍 $1${NC}"
  fi
}

log_section() {
  echo ""
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BLUE}  $1${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo ""
}

# Error handler - call on script failures
handle_error() {
  local exit_code=$1
  local line_number=$2
  local bash_lineno=$3
  local command="$4"

  log_error "Script failed at line $line_number"
  log_error "Exit code: $exit_code"
  if [ -n "$SCENARIO_NAME" ]; then
    log_error "Scenario: $SCENARIO_NAME"
  fi
  log_debug "Command: $command"

  exit "$exit_code"
}

# Progress indicator
log_progress() {
  local current=$1
  local total=$2
  local item=$3
  echo -e "${CYAN}[$current/$total]${NC} $item"
}

# Summary line with timing
log_summary() {
  local status=$1
  local duration=$2

  if [ "$status" = "success" ]; then
    echo ""
    log_success "Scenario complete! (${duration}s)"
  else
    echo ""
    log_error "Scenario failed! (${duration}s)"
  fi
}
