---
name: git-workflow
description: "Git conventions for BoostedBudget: branch naming, commit format, PR workflow, and merge strategy."
version: "1.0.0"
---

# Git Workflow Guide

## Branch Naming Convention

Format: `<type>/<issue#>-<short-description>`

### Types

| Type | Use For |
|------|---------|
| `feature` | New features |
| `bugfix` | Bug fixes |
| `refactor` | Code refactoring |
| `docs` | Documentation changes |
| `devops` | CI/CD, infrastructure |

### Examples

```
feature/627-user-authentication
bugfix/628-fix-transaction-sync
refactor/629-cleanup-handlers
docs/630-update-readme
devops/631-add-github-action
```

## Commit Message Format

**CRITICAL: All commits MUST start with issue ID**

Format: `#N: Commit message`

### Structure

```
#627: Short summary (50 chars max)

- Detailed bullet point 1
- Detailed bullet point 2

Co-Authored-By: Claude <noreply@anthropic.com>
```

### Examples

```bash
# Good
git commit -m "#627: Add Clerk JWT validation middleware"
git commit -m "#628: Fix null pointer in transaction sync"

# Bad (missing issue ID)
git commit -m "Add authentication"
git commit -m "Fix bug"
```

### Multi-line Commit

```bash
git commit -m "#627: Implement user authentication

- Add Clerk JWT validation middleware
- Resolve user ID from claims
- Auto-register new users on first login

Co-Authored-By: Claude <noreply@anthropic.com>"
```

## Branching Strategy

```
main (production)
  │
  └── staging (integration)
        │
        ├── feature/627-auth
        ├── bugfix/628-sync
        └── feature/629-budgets
```

### Branch Flow

1. **Create from staging**: `git checkout staging && git pull && git checkout -b feature/N-desc`
2. **Develop**: Make commits with `#N:` prefix
3. **PR to staging**: All feature PRs target `staging`
4. **Merge to staging**: After review approval
5. **PR staging→main**: For production release
6. **Auto-sync**: After main deployment, main merges back to staging

### Branch Protection

- `main`: Requires PR, status checks, approval
- `staging`: Requires PR, status checks

## Pull Request Workflow

### PR Title Format

**CRITICAL: All PR titles MUST start with issue ID**

Format: `#N: PR title`

### PR Description Template

```markdown
## Summary
- Brief description of changes
- Key features/fixes

## Test Plan
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed

## Screenshots (if UI changes)

Closes #N
```

### Creating a PR

```bash
# Push branch
git push -u origin feature/627-auth

# Create PR
gh pr create \
  --title "#627: Add user authentication" \
  --body "## Summary
- Implement Clerk JWT middleware
- Add user context resolution

## Test Plan
- [x] Unit tests pass
- [x] Integration tests pass

Closes #627" \
  --base staging
```

### PR Targeting

| PR Type | Target Branch |
|---------|---------------|
| Feature | `staging` |
| Bugfix | `staging` |
| Hotfix | `main` (with approval) |
| Release | `main` (from staging) |

## Auto-Sync Behavior

After `staging→main` merge and deployment:

1. GitHub Action runs "Sync Staging with Main"
2. Merges `main→staging` automatically
3. Prevents branch divergence

### Manual Sync (if auto-sync fails)

```bash
git checkout staging
git pull origin staging
git merge origin/main --no-edit
git push origin staging
```

## Common Git Commands

### Start New Work

```bash
# Ensure staging is up to date
git checkout staging
git pull origin staging

# Create feature branch
git checkout -b feature/N-description
```

### Save Progress

```bash
# Stage all changes
git add -A

# Commit with issue prefix
git commit -m "#N: Description"

# Push
git push -u origin feature/N-description
```

### Sync with Staging

```bash
# While on feature branch
git fetch origin
git rebase origin/staging

# Or merge
git merge origin/staging
```

### Clean Up After Merge

```bash
# Delete local branch
git branch -d feature/N-description

# Delete remote branch (if not auto-deleted)
git push origin --delete feature/N-description
```

## Quick Reference

| Action | Command |
|--------|---------|
| New branch | `git checkout -b feature/N-desc` |
| Commit | `git commit -m "#N: message"` |
| Push | `git push -u origin feature/N-desc` |
| Create PR | `gh pr create --base staging` |
| Sync | `git fetch && git rebase origin/staging` |
