#!/usr/bin/env bash

# build-and-push.sh - Build and optionally push Docker images to GHCR
# Usage: ./scripts/build-and-push.sh [--push] [version]
#
# Examples:
#   ./scripts/build-and-push.sh               # Build only (no push)
#   ./scripts/build-and-push.sh --push         # Build and push with git SHA tag
#   ./scripts/build-and-push.sh --push v1.2.3  # Build and push with custom version

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REGISTRY="ghcr.io"
GITHUB_USER="${GITHUB_REPOSITORY_OWNER:-maxnussbaum}"
BACKEND_IMAGE="${REGISTRY}/${GITHUB_USER}/boostedbudget-backend"
FRONTEND_IMAGE="${REGISTRY}/${GITHUB_USER}/boostedbudget-frontend"

# Parse arguments
PUSH=false
VERSION=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --push)
      PUSH=true
      shift
      ;;
    *)
      VERSION="$1"
      shift
      ;;
  esac
done

# Determine version tag
if [ -z "$VERSION" ]; then
  # Use git SHA if no version specified
  VERSION=$(git rev-parse --short HEAD)
  echo -e "${BLUE}ℹ No version specified, using git SHA: ${VERSION}${NC}"
fi

# Function to print section headers
print_section() {
  echo ""
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BLUE}  $1${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# Function to check if Docker is running
check_docker() {
  if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}✗ Error: Docker is not running${NC}"
    exit 1
  fi
}

# Function to check if logged into GHCR
check_ghcr_auth() {
  if [ "$PUSH" = true ]; then
    if ! docker pull ${REGISTRY}/hello-world > /dev/null 2>&1; then
      echo -e "${YELLOW}⚠ Warning: Not authenticated with GHCR${NC}"
      echo -e "${YELLOW}  Run: echo \$GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin${NC}"
      read -p "Do you want to continue without pushing? (y/N) " -n 1 -r
      echo
      if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
      fi
      PUSH=false
    fi
  fi
}

# Function to build an image
build_image() {
  local context=$1
  local image=$2
  local version=$3

  print_section "Building $image:$version"

  cd "$context"

  # Build with multiple tags
  docker build \
    --tag "${image}:${version}" \
    --tag "${image}:latest" \
    --build-arg BUILD_DATE="$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
    --build-arg VERSION="${version}" \
    --build-arg VCS_REF="$(git rev-parse HEAD)" \
    .

  local exit_code=$?
  cd - > /dev/null

  if [ $exit_code -eq 0 ]; then
    echo -e "${GREEN}✓ Successfully built ${image}:${version}${NC}"
  else
    echo -e "${RED}✗ Failed to build ${image}:${version}${NC}"
    exit $exit_code
  fi
}

# Function to push an image
push_image() {
  local image=$1
  local version=$2

  print_section "Pushing $image:$version"

  docker push "${image}:${version}"
  docker push "${image}:latest"

  if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Successfully pushed ${image}:${version}${NC}"
    echo -e "${GREEN}✓ Successfully pushed ${image}:latest${NC}"
  else
    echo -e "${RED}✗ Failed to push ${image}${NC}"
    exit 1
  fi
}

# Function to display image info
display_info() {
  local image=$1
  local version=$2

  echo ""
  echo -e "${BLUE}Image: ${image}:${version}${NC}"
  docker images "${image}:${version}" --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}"
}

# Main execution
main() {
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║           BoostedBudget Docker Build Script                   ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
  echo ""
  echo -e "${BLUE}Version:${NC} ${VERSION}"
  echo -e "${BLUE}Push to registry:${NC} ${PUSH}"
  echo ""

  # Pre-flight checks
  check_docker
  check_ghcr_auth

  # Build backend
  build_image "./backend" "${BACKEND_IMAGE}" "${VERSION}"
  display_info "${BACKEND_IMAGE}" "${VERSION}"

  # Build frontend
  build_image "./frontend" "${FRONTEND_IMAGE}" "${VERSION}"
  display_info "${FRONTEND_IMAGE}" "${VERSION}"

  # Push if requested
  if [ "$PUSH" = true ]; then
    push_image "${BACKEND_IMAGE}" "${VERSION}"
    push_image "${FRONTEND_IMAGE}" "${VERSION}"

    print_section "Summary"
    echo -e "${GREEN}✓ All images built and pushed successfully!${NC}"
    echo ""
    echo -e "${BLUE}Backend image:${NC}  ${BACKEND_IMAGE}:${VERSION}"
    echo -e "${BLUE}Frontend image:${NC} ${FRONTEND_IMAGE}:${VERSION}"
    echo ""
    echo -e "${BLUE}Deploy with:${NC}"
    echo -e "  ./scripts/deploy-to-fly.sh production ${VERSION}"
  else
    print_section "Summary"
    echo -e "${GREEN}✓ All images built successfully!${NC}"
    echo ""
    echo -e "${YELLOW}Images were not pushed (use --push to push to registry)${NC}"
    echo ""
    echo -e "${BLUE}To push:${NC}"
    echo -e "  ./scripts/build-and-push.sh --push ${VERSION}"
  fi
}

main "$@"
