# Branch Protection Setup Guide

This guide walks through setting up branch protection rules for the BoostedBudget repository to enforce code quality and deployment safety.

## Overview

Branch protection ensures that:
- ✅ All tests pass before merging
- ✅ Code coverage meets thresholds
- ✅ Security scans complete successfully
- ✅ Manual approval required for production deployments
- ✅ Linear commit history is maintained

## Main Branch Protection

### Step 1: Navigate to Branch Protection Settings

1. Go to repository: https://github.com/maxnussbaum/BoostedBudget
2. Click **Settings** tab
3. Click **Branches** in left sidebar
4. Click **Add branch protection rule**

### Step 2: Configure Branch Name Pattern

**Branch name pattern:** `main`

### Step 3: Configure Protection Rules

Enable the following settings:

#### ✅ Require a pull request before merging
- **Required approvals:** 1 (recommended)
- ✅ Dismiss stale pull request approvals when new commits are pushed
- ✅ Require review from Code Owners (optional, if using CODEOWNERS file)

#### ✅ Require status checks to pass before merging
- ✅ Require branches to be up to date before merging

**Required status checks** (add these):
- `test` - Main test job (Backend + Frontend + E2E)
- `build-images` (optional - only runs on main, but good to have)
- `security-scan` (optional - only runs on main)

> **Note:** Status checks will only appear in the list after the first workflow run. You may need to create a PR first, then come back to add these checks.

#### ✅ Require conversation resolution before merging
Ensures all review comments are addressed.

#### ✅ Require signed commits (optional, recommended)
Verifies commit authenticity with GPG signatures.

#### ✅ Require linear history
Prevents merge commits, enforces clean git history.

#### ✅ Require deployments to succeed before merging (optional)
Can be enabled once staging environment exists.

#### ✅ Lock branch (optional, for emergency freeze)
Prevents all changes except by admins.

#### ⚠️ Do not allow bypassing the above settings
Enforce rules for administrators too (recommended).

#### ✅ Restrict who can push to matching branches (optional)
- Limit to specific users or teams
- Useful for larger teams

#### ✅ Allow force pushes (disabled)
Prevents rewriting history.

#### ✅ Allow deletions (disabled)
Prevents accidental branch deletion.

### Step 4: Save Protection Rule

Click **Create** or **Save changes**.

## Environment Protection Rules

### Production Environment Setup

1. Go to **Settings** → **Environments**
2. Click **New environment**
3. Name: `production`

#### Configure Environment Protection Rules

**Required reviewers:**
- Add yourself and/or team members (minimum 1)
- Reviewers must manually approve before deployment

**Deployment branches:**
- ✅ Selected branches only
- Add: `main`

**Environment secrets:**
- Add secret: `FLY_API_TOKEN`
  - Value: Your Fly.io API token for production
  - Get it from: https://fly.io/dashboard/personal/tokens

**Wait timer (optional):**
- Set to 0 minutes (manual approval only)
- Or set delay (e.g., 5 minutes) for automatic cool-down

## Verification

### Test Branch Protection

1. Create a test branch:
   ```bash
   git checkout -b test-protection
   ```

2. Make a small change and push:
   ```bash
   echo "# Test" >> test.txt
   git add test.txt
   git commit -m "Test: Verify branch protection"
   git push origin test-protection
   ```

3. Create a pull request

4. Verify:
   - ✅ PR shows "Required checks" section
   - ✅ Cannot merge until checks pass
   - ✅ "Merge" button is disabled until requirements met

5. After checks pass:
   - ✅ Approval required (if configured)
   - ✅ Can merge after approval

6. Clean up:
   ```bash
   gh pr close <pr-number>
   git checkout main
   git branch -D test-protection
   ```

### Test Environment Protection

1. Merge a PR to main
2. Watch the workflow run
3. When `deploy-production` job starts:
   - ✅ Should show "Waiting for review"
   - ✅ Notification sent to required reviewers
   - ✅ Cannot proceed without approval

4. Approve deployment:
   - Click "Review deployments" button
   - Select "production" environment
   - Click "Approve and deploy"

5. Verify deployment completes

## Troubleshooting

### Status checks not appearing

**Problem:** Required status checks don't show up in the dropdown.

**Solution:**
1. Create a PR and let the workflow run once
2. After workflow completes, checks will appear
3. Go back to branch protection settings
4. Add the newly visible checks

### Can't merge even though checks passed

**Problem:** Merge button still disabled after all checks pass.

**Solution:**
- Check if branch is up to date with base branch
- Pull latest main and rebase if needed:
  ```bash
  git checkout main
  git pull
  git checkout feature-branch
  git rebase main
  git push --force-with-lease
  ```

### Environment protection not working

**Problem:** Deployment runs without approval.

**Solution:**
1. Verify environment name matches exactly: `production`
2. Check workflow file uses correct environment:
   ```yaml
   environment:
     name: production
   ```
3. Ensure required reviewers are set in environment settings

### Force push blocked

**Problem:** Need to force push for legitimate reason (e.g., removing sensitive data).

**Solution:**
1. Temporarily disable branch protection
2. Perform necessary force push
3. Re-enable branch protection immediately
4. Document the reason in commit message

## Best Practices

### For Individual Developers

- ✅ Always create feature branches from up-to-date main
- ✅ Run `make test-ci` locally before pushing
- ✅ Keep PRs small and focused
- ✅ Respond to review comments promptly
- ✅ Rebase instead of merge to keep history clean

### For Teams

- ✅ Require code reviews from team members
- ✅ Use CODEOWNERS file for automatic reviewer assignment
- ✅ Set up Slack/email notifications for PR reviews
- ✅ Define clear review criteria
- ✅ Review security scan results before approving deployments

### For Deployments

- ✅ Always review changes before approving production deployments
- ✅ Check smoke test results in workflow logs
- ✅ Monitor Fly.io logs after deployment
- ✅ Have rollback plan ready (previous image tag)
- ✅ Schedule deployments during low-traffic periods

## Emergency Procedures

### Hotfix Process

For critical production bugs:

1. Create hotfix branch from main:
   ```bash
   git checkout main
   git pull
   git checkout -b hotfix/critical-bug
   ```

2. Fix the bug with minimal changes

3. Test thoroughly locally:
   ```bash
   make test-ci
   ```

4. Push and create PR:
   ```bash
   git push origin hotfix/critical-bug
   gh pr create --title "Hotfix: Critical bug description"
   ```

5. Request expedited review

6. After CI passes and approval:
   - Merge immediately
   - Monitor deployment closely
   - Verify fix in production

### Rollback Procedure

If deployment causes issues:

1. Get previous working image SHA:
   ```bash
   fly releases --app boostedbudget-api
   ```

2. Rollback using previous image:
   ```bash
   fly deploy \
     --app boostedbudget-api \
     --image ghcr.io/maxnussbaum/boostedbudget-backend:<previous-sha>
   ```

3. Do the same for frontend if needed

4. Create issue to investigate and fix properly

## Maintenance

### Regular Reviews

**Monthly:**
- Review branch protection rules
- Update required status checks if workflow changes
- Audit environment protection reviewers

**Quarterly:**
- Review and update this documentation
- Audit deployment logs for patterns
- Update emergency procedures if needed

### When to Update

Update branch protection when:
- Adding new workflow jobs that should be required
- Adding new team members who should review deployments
- Changing deployment strategy or environments
- Security requirements change

## Additional Resources

- [GitHub Branch Protection Docs](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/defining-the-mergeability-of-pull-requests/about-protected-branches)
- [GitHub Environments Docs](https://docs.github.com/en/actions/deployment/targeting-different-environments/using-environments-for-deployment)
- [Workflow Documentation](.github/workflows/README.md)
- [CLAUDE.md](../CLAUDE.md) - Development guidelines

## Support

For issues with branch protection:
1. Check GitHub documentation
2. Review workflow logs
3. Verify environment configuration
4. Contact repository maintainers
