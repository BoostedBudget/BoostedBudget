#!/bin/bash
# Install git hooks for the BoostedBudget project
# This script sets up pre-push hooks to run tests before pushing

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "🔧 Installing git hooks for BoostedBudget..."
echo ""

# Get the project root directory (one level up from scripts/)
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOKS_DIR="$PROJECT_ROOT/.git/hooks"
HOOK_SOURCE="$PROJECT_ROOT/scripts/pre-push-hook"
HOOK_TARGET="$HOOKS_DIR/pre-push"

# Check if we're in a git repository
if [ ! -d "$PROJECT_ROOT/.git" ]; then
    echo "❌ Error: Not in a git repository"
    echo "   Please run this script from the BoostedBudget project root"
    exit 1
fi

# Create hooks directory if it doesn't exist
if [ ! -d "$HOOKS_DIR" ]; then
    echo "Creating hooks directory..."
    mkdir -p "$HOOKS_DIR"
fi

# Make the source hook executable
chmod +x "$HOOK_SOURCE"

# Remove existing hook if it exists (to handle both symlinks and regular files)
if [ -e "$HOOK_TARGET" ] || [ -L "$HOOK_TARGET" ]; then
    echo "Removing existing pre-push hook..."
    rm "$HOOK_TARGET"
fi

# Create symlink
ln -s "$HOOK_SOURCE" "$HOOK_TARGET"

echo ""
echo -e "${GREEN}✅ Git hooks installed successfully!${NC}"
echo ""
echo "Installed hooks:"
echo "  • pre-push: Runs 'make test-all' before every push"
echo ""
echo -e "${YELLOW}Note:${NC} You can bypass the pre-push hook with:"
echo "  git push --no-verify"
echo ""
echo "Happy coding! 🚀"
