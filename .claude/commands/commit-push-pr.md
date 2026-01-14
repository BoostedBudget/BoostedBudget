# Commit, Push, and Create PR

Stage, commit, push, and create a PR in one workflow following project conventions.

## Usage

```
/commit-push-pr [issue-number] [commit-message]
```

## Workflow

### 1. Pre-Commit Checks

Run verification before committing:
```bash
# Backend checks
cd backend && go vet ./... && go test ./... -v

# Frontend checks
cd apps/web && pnpm run lint && pnpm test -- --run
```

### 2. Stage Changes

```bash
git add -A
git status
```

Review staged changes before committing.

### 3. Commit with Issue Prefix

**CRITICAL: All commits MUST start with issue ID**

Format: `#N: Commit message`

```bash
git commit -m "#627: Add user authentication middleware

- Implement Clerk JWT validation
- Add user context resolution
- Include auto-registration flow

Co-Authored-By: Claude <noreply@anthropic.com>"
```

### 4. Push to Remote

```bash
git push -u origin feature/N-description
```

### 5. Create PR

**PR titles MUST start with issue ID**: `#N: PR title`

```bash
gh pr create \
  --title "#627: Add user authentication middleware" \
  --body "## Summary
- Implement Clerk JWT validation middleware
- Add user context resolution from JWT claims
- Auto-register new users on first login

## Test Plan
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing with Clerk dashboard

Closes #627" \
  --base staging
```

## Branch Naming Convention

Format: `feature|bugfix|refactor|docs/<issue#>-<short-description>`

Examples:
- `feature/627-user-auth`
- `bugfix/628-fix-transaction-sync`
- `refactor/629-cleanup-handlers`

## PR Targeting

- **Feature PRs** → target `staging`
- **Hotfixes** → target `main` (with approval)
- **staging→main** → for production release

## Auto-Sync Behavior

After `staging→main` merge and successful deployment:
1. GitHub Action merges `main→staging` automatically
2. Prevents branch divergence
3. If auto-sync fails, manual sync required:
   ```bash
   git checkout staging && git pull && git merge origin/main --no-edit && git push
   ```

## Related Skills

- `.claude/skills/git-workflow/` - Complete git conventions reference
