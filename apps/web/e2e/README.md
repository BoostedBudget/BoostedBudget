# E2E Tests - BoostedBudget

End-to-end testing suite for BoostedBudget using Playwright. These tests verify Phase 0 deliverables including authentication, health checks, GDPR compliance, and user flows.

## Test Coverage

### 1. Health Endpoint (`health.spec.ts`)
- ✅ Backend health endpoint returns correct status
- ✅ Application homepage loads successfully
- ✅ Backend API is accessible

### 2. Cookie Consent Banner (`cookie-consent.spec.ts`)
- ✅ GDPR cookie consent banner displays on first visit
- ✅ Accept All / Necessary Only / Customize buttons present
- ✅ Banner hides after user selection
- ✅ Detailed preferences with toggles for analytics and advertising
- ✅ Necessary cookies are always enabled (disabled checkbox)
- ✅ Custom preferences can be saved
- ✅ Banner doesn't reappear on subsequent visits after acceptance
- ✅ Privacy and Cookie policy links present
- ✅ Accessibility (ARIA labels, role attributes)

### 3. Clerk Authentication (`auth.spec.ts`)
- ✅ Unauthenticated state shows Log In button
- ✅ Protected routes redirect to login when not authenticated
- ✅ Login button opens Clerk modal
- ✅ Invalid credentials show error message
- ✅ Signup option available in Clerk modal
- ✅ Clerk redirect configuration is correct
- ⏭️ **Skipped (requires Clerk test credentials)**:
  - Successful login with valid credentials
  - Account creation flow
  - Authenticated user can access protected routes
  - User profile information display
  - Logout flow
  - Session persistence across reloads/navigation

## Prerequisites

### Required
- Bun
- Backend server running (`./bin/server` in `../backend/`)
- Frontend development server (`bun run dev`)

### Optional (for full Clerk tests)
- Clerk test tenant with test user
- Test credentials in `.env.test` file

## Setup

### 1. Install Dependencies
```bash
cd apps/web
bun install
```

### 2. Install Playwright Browsers
```bash
npx playwright install chromium
```

### 3. Configure Test Environment (Optional)
For Clerk tests that require real credentials, create `.env.test`:

```bash
cp e2e/.env.test.example e2e/.env.test
```

Edit `e2e/.env.test` with your Clerk test tenant credentials:
```env
TEST_CLERK_EMAIL=test-user@example.com
TEST_CLERK_PASSWORD=TestPassword123!
VITE_CLERK_PUBLISHABLE_KEY=pk_test_your_clerk_publishable_key
```

## Running Tests

### Run All E2E Tests
```bash
bun run test:e2e
```

### Run Tests with UI Mode (Interactive)
```bash
bun run test:e2e:ui
```

### Run Tests in Headed Mode (See Browser)
```bash
bun run test:e2e:headed
```

### Run Tests in Debug Mode
```bash
bun run test:e2e:debug
```

### Run Specific Test File
```bash
npx playwright test health.spec.ts
```

### View Test Report
```bash
bun run test:e2e:report
```

## Test Structure

```
e2e/
├── README.md                    # This file
├── .env.test.example            # Example environment configuration
├── helpers/
│   └── auth.ts                  # Clerk helper functions
├── health.spec.ts               # Health endpoint tests
├── cookie-consent.spec.ts       # GDPR cookie consent tests
└── auth.spec.ts                 # Clerk authentication flow tests
```

## Helper Functions

The `helpers/auth.ts` module provides reusable functions for Clerk flows:

- `signInWithClerk(page, credentials)` - Perform login via Clerk modal
- `signOut(page)` - Perform logout
- `isAuthenticated(page)` - Check if user is authenticated
- `acceptCookieConsent(page)` - Accept GDPR cookie consent banner

## Skipped Tests

Some tests are marked with `test.skip()` because they require:

1. **Actual Clerk test credentials** - Creating real users in Clerk test tenant
2. **Clerk user cleanup** - Automated cleanup of test users after test runs
3. **Session management** - Handling authenticated state across tests

To enable these tests:
1. Set up Clerk test tenant
2. Create test user or implement automated user creation
3. Configure `.env.test` with test credentials
4. Remove `test.skip()` from the desired tests

## CI/CD Integration

These tests are designed to run in GitHub Actions CI/CD pipeline:

### Test Job (from #9 - CI/CD Pipeline issue)
```yaml
- name: Run E2E Tests
  run: |
    cd apps/web
    bun run test:e2e
```

### Recommended CI Configuration
- Use headless mode (default in CI)
- Set `CI=true` environment variable
- Store Clerk test credentials in GitHub Secrets
- Upload test artifacts (screenshots, videos) on failure
- Generate and upload test reports

## Debugging Failed Tests

### 1. View Screenshots
Failed tests automatically capture screenshots:
```
playwright-report/
  test-results/
    <test-name>/
      screenshot.png
```

### 2. View Videos
Failed tests record video (if configured):
```
playwright-report/
  test-results/
    <test-name>/
      video.webm
```

### 3. View Trace
Run with trace enabled:
```bash
npx playwright test --trace on
```

Then view trace:
```bash
npx playwright show-trace trace.zip
```

### 4. Run in Debug Mode
```bash
bun run test:e2e:debug
```

## Best Practices

### 1. Test Isolation
- Each test starts with a clean state (cookies cleared)
- Tests should not depend on each other
- Use `test.beforeEach()` for common setup

### 2. Waiting Strategies
- Use `waitForLoadState('networkidle')` for page loads
- Use `waitForURL()` for navigation
- Use `waitForTimeout()` sparingly (prefer explicit waits)

### 3. Selectors
- Prefer text-based selectors: `button:has-text("Log In")`
- Use aria-labels for accessibility
- Use data-testid attributes for complex components
- Avoid brittle CSS selectors

### 4. Clerk Testing
- Clerk modal UI may change
- Use flexible selectors that work across Clerk versions
- Test redirect flow rather than Clerk UI details
- Consider mocking Clerk for unit tests

## Known Limitations

1. **Clerk Dependency**: Full authentication tests require real Clerk credentials
2. **Browser Download**: First run downloads ~280MB of browser binaries
3. **Server Requirement**: Tests expect backend server at `localhost:8080`
4. **Sequential Execution**: Clerk tests should run sequentially to avoid conflicts

## Troubleshooting

### Tests Fail: "Target closed"
- Ensure backend server is running: `cd ../backend && ./bin/server`
- Ensure frontend dev server is running: `bun run dev`

### Tests Fail: "Timeout waiting for Clerk"
- Check Clerk configuration in `.env`
- Verify Clerk publishable key is valid
- Check network connectivity

### Tests Fail: "Element not found"
- Component might not be implemented yet (Phase 0)
- UI structure might have changed
- Cookie banner might be blocking element

### Slow Test Execution
- Use headed mode only for debugging: `bun run test:e2e:headed`
- Run specific test files instead of entire suite
- Increase workers in `playwright.config.ts` (parallel execution)

## Future Improvements

1. **Clerk Test Automation**
   - Automated test user creation/cleanup
   - Mocked Clerk for faster tests
   - Separate test tenant for CI/CD

2. **Additional Test Coverage**
   - User profile management
   - Account settings
   - Premium subscription flows
   - Error handling and edge cases

3. **Performance Testing**
   - Page load times
   - API response times
   - Lighthouse audits

4. **Accessibility Testing**
   - Automated a11y checks
   - Keyboard navigation
   - Screen reader compatibility

## References

- [Playwright Documentation](https://playwright.dev/)
- [Clerk Documentation](https://clerk.com/docs)
- [GDPR Cookie Consent Best Practices](https://gdpr.eu/cookies/)
- Issue #12: Phase 0 Integration Testing
- `boosted-budget-complete-plan.md`: Phase 0 requirements
