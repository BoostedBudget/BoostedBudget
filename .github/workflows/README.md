# GitHub Actions CI/CD Workflows

This directory contains GitHub Actions workflows for BoostedBudget's CI/CD pipeline.

## Workflows

### `ci-cd.yml` - Main CI/CD Pipeline

Complete continuous integration and deployment pipeline that runs on all pull requests and pushes to main.

**Trigger Events:**
- Pull requests (opened, synchronized, reopened)
- Pushes to `main` branch

**Jobs:**

#### 1. Test (`test`)
Runs comprehensive test suite with coverage enforcement.

**Runs on:** All PRs and pushes

**Steps:**
- Backend tests (Go) with race detection
- Coverage threshold enforcement (≥60%)
- Frontend unit tests (Bun) with coverage
- Linting (ESLint)
- E2E tests (Playwright)
- Upload coverage artifacts

**Duration:** ~3-5 minutes

**Quality Gates:**
- ✅ All tests must pass
- ✅ Backend coverage ≥60%
- ✅ Frontend coverage enforced
- ✅ Linting passes
- ✅ No race conditions detected

#### 2. Build Images (`build-images`)
Builds Docker images and pushes to GitHub Container Registry.

**Runs on:** Pushes to `main` only (after PR merge)

**Dependencies:** Requires `test` job to pass

**Steps:**
- Build multi-stage backend Docker image
- Build multi-stage frontend Docker image
- Tag with git SHA and `latest`
- Push to GHCR
- Use GitHub Actions cache for faster builds

**Duration:** ~4-6 minutes

**Images:**
- `ghcr.io/maxnussbaum/boostedbudget-backend:latest`
- `ghcr.io/maxnussbaum/boostedbudget-backend:<git-sha>`
- `ghcr.io/maxnussbaum/boostedbudget-frontend:latest`
- `ghcr.io/maxnussbaum/boostedbudget-frontend:<git-sha>`

#### 3. Security Scan (`security-scan`)
Scans Docker images for vulnerabilities using Trivy.

**Runs on:** After images are built (main branch only)

**Dependencies:** Requires `build-images` job

**Steps:**
- Scan backend image for vulnerabilities
- Scan frontend image for vulnerabilities
- Upload SARIF results to GitHub Security tab
- Fail on CRITICAL vulnerabilities

**Duration:** ~2-3 minutes

**Severity Levels:**
- CRITICAL: Pipeline fails ❌
- HIGH: Reported, pipeline continues ⚠️
- MEDIUM: Reported, pipeline continues ⚠️

#### 4. Deploy Production (`deploy-production`)
Deploys to production Fly.io environment.

**Runs on:** Pushes to `main` only (manual approval required)

**Dependencies:** Requires `build-images` and `security-scan` to pass

**Environment:** `production` (manual approval required)

**Steps:**
- Deploy backend to Fly.io using pre-built image
- Deploy frontend to Fly.io using pre-built image
- Verify deployment status
- Run smoke tests

**Duration:** ~3-4 minutes

**Approval:** Requires manual approval via GitHub Environment protection

## Total Pipeline Duration

**Pull Request:** ~3-5 minutes (test job only)
**Main Branch:** ~12-15 minutes (all jobs)

## Environment Variables

```yaml
GO_VERSION: '1.25.5'    # Go version for backend tests
REGISTRY: 'ghcr.io'     # Container registry
```

## Secrets Required

### GitHub Secrets
- `GITHUB_TOKEN` - Auto-generated, used for GHCR authentication
- `FLY_API_TOKEN` - Fly.io API token for production deployments

### Future Secrets (not yet required)
- `SLACK_WEBHOOK_URL` - For deployment notifications
- `SONAR_TOKEN` - For SonarCloud integration

## GitHub Environments

### Production
- **Name:** `production`
- **URL:** https://boostedbudget.com
- **Protection:** Manual approval required
- **Reviewers:** 1+ required
- **Secrets:** `FLY_API_TOKEN`

## Branch Protection Rules

See `.github/branch-protection-setup.md` for detailed configuration instructions.

**Required checks for `main` branch:**
- ✅ `test` job must pass
- ✅ `security-scan` must pass (if running)
- ✅ Branch must be up to date

## Local Testing

### Run tests locally (without CI)
```bash
# Run all tests with coverage
make test-ci

# Backend only
cd backend && go test -v -race -coverprofile=coverage.out ./...

# Frontend only
cd frontend && bun run test:ci

# E2E only
cd frontend && bun run test:e2e
```

### Build Docker images locally
```bash
# Build both images
./scripts/build-and-push.sh

# Build and push to GHCR
./scripts/build-and-push.sh --push
```

### Run smoke tests
```bash
# Against production
./scripts/smoke-test.sh production

# Against QA (Mac Mini)
./scripts/smoke-test.sh qa
```

## Coverage Reports

Coverage reports are automatically generated and uploaded as artifacts on every test run.

**Access coverage reports:**
1. Go to Actions tab
2. Click on a workflow run
3. Scroll to "Artifacts" section
4. Download:
   - `backend-coverage` - Go coverage reports
   - `frontend-coverage` - Frontend coverage reports
   - `playwright-report` - E2E test results

## Monitoring

### Check workflow status
- View in GitHub Actions tab
- Status badge in README (automatically updated)

### View security scan results
- GitHub Security tab → Code scanning alerts
- Trivy results uploaded as SARIF format

### Deployment logs
```bash
# View Fly.io logs
fly logs --app boostedbudget-api
fly logs --app boostedbudget-web

# Check deployment status
fly status --app boostedbudget-api
fly status --app boostedbudget-web
```

## Troubleshooting

### Tests failing in CI but passing locally
- Ensure Node/Go versions match CI
- Check for timing issues in E2E tests
- Review race condition warnings

### Coverage below threshold
- Add more unit tests
- Remove excluded files from coverage
- Check coverage locally first: `make test-ci`

### Docker build failures
- Check Dockerfile syntax
- Verify all dependencies are listed
- Test build locally: `./scripts/build-and-push.sh`

### Deployment failures
- Verify `FLY_API_TOKEN` secret is set
- Check Fly.io app exists: `fly status --app boostedbudget-api`
- Review Fly.io logs: `fly logs --app boostedbudget-api`

### Security scan failures
- Review GitHub Security tab for details
- Check Trivy results in workflow logs
- Update base images if needed
- Fix or suppress known false positives

## Future Enhancements

Prepared but not yet implemented:
- [ ] Coverage report PR comments
- [ ] Staging deployment (when develop branch exists)
- [ ] SonarCloud integration
- [ ] Slack/Discord notifications
- [ ] Performance benchmarking
- [ ] Multi-region deployment support

## Related Documentation

- [CLAUDE.md](../../CLAUDE.md) - Development guidelines
- [boosted-budget-fly-deployment.md](../../boosted-budget-fly-deployment.md) - Deployment strategy
- [Branch Protection Setup](.github/branch-protection-setup.md) - GitHub settings
- [Scripts README](../../scripts/README.md) - Deployment scripts

## Support

For issues with the CI/CD pipeline:
1. Check workflow logs in GitHub Actions
2. Review this documentation
3. Check deployment scripts in `scripts/` directory
4. Consult deployment documentation
