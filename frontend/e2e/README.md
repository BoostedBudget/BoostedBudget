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

### 3. Auth0 Authentication (`auth.spec.ts`)
- ✅ Unauthenticated state shows Log In button
- ✅ Protected routes redirect to login when not authenticated
- ✅ Login button redirects to Auth0 Universal Login
- ✅ Invalid credentials show error message
- ✅ Signup option available on Auth0 page
- ✅ Auth0 redirect configuration is correct
- ⏭️ **Skipped (requires Auth0 test credentials)**:
  - Successful login with valid credentials
  - Account creation flow
  - Authenticated user can access protected routes
  - User profile information display
  - Logout flow
  - Session persistence across reloads/navigation

## Prerequisites

### Required
- Node.js 18+ and pnpm
- Backend server running (`./bin/server` in `../backend/`)
- Frontend development server (`pnpm run dev`)

### Optional (for full Auth0 tests)
- Auth0 test tenant with test user
- Test credentials in `.env.test` file

## Setup

### 1. Install Dependencies
```bash
cd frontend
pnpm install
```

### 2. Install Playwright Browsers
```bash
npx playwright install chromium
```

### 3. Configure Test Environment (Optional)
For Auth0 tests that require real credentials, create `.env.test`:

```bash
cp e2e/.env.test.example e2e/.env.test
```

Edit `e2e/.env.test` with your Auth0 test tenant credentials:
```env
TEST_AUTH0_EMAIL=test-user@example.com
TEST_AUTH0_PASSWORD=TestPassword123!
VITE_AUTH0_DOMAIN=your-test-tenant.auth0.com
VITE_AUTH0_CLIENT_ID=your-test-client-id
VITE_AUTH0_AUDIENCE=https://api.boostedbudget.com
```

## Running Tests

### Run All E2E Tests
```bash
pnpm run test:e2e
```

### Run Tests with UI Mode (Interactive)
```bash
pnpm run test:e2e:ui
```

### Run Tests in Headed Mode (See Browser)
```bash
pnpm run test:e2e:headed
```

### Run Tests in Debug Mode
```bash
pnpm run test:e2e:debug
```

### Run Specific Test File
```bash
npx playwright test health.spec.ts
```

### View Test Report
```bash
pnpm run test:e2e:report
```

## Test Structure

```
e2e/
├── README.md                    # This file
├── .env.test.example            # Example environment configuration
├── helpers/
│   └── auth.ts                  # Auth0 helper functions
├── health.spec.ts               # Health endpoint tests
├── cookie-consent.spec.ts       # GDPR cookie consent tests
└── auth.spec.ts                 # Auth0 authentication flow tests
```

## Helper Functions

The `helpers/auth.ts` module provides reusable functions for Auth0 flows:

- `loginWithAuth0(page, credentials)` - Perform login via Auth0 Universal Login
- `signupWithAuth0(page, credentials)` - Perform signup via Auth0
- `logout(page)` - Perform logout
- `isAuthenticated(page)` - Check if user is authenticated
- `acceptCookieConsent(page)` - Accept GDPR cookie consent banner

## Skipped Tests

Some tests are marked with `test.skip()` because they require:

1. **Actual Auth0 test credentials** - Creating real users in Auth0 test tenant
2. **Auth0 user cleanup** - Automated cleanup of test users after test runs
3. **Session management** - Handling authenticated state across tests

To enable these tests:
1. Set up Auth0 test tenant
2. Create test user or implement automated user creation
3. Configure `.env.test` with test credentials
4. Remove `test.skip()` from the desired tests

## CI/CD Integration

These tests are designed to run in GitHub Actions CI/CD pipeline:

### Test Job (from #9 - CI/CD Pipeline issue)
```yaml
- name: Run E2E Tests
  run: |
    cd frontend
    pnpm run test:e2e
```

### Recommended CI Configuration
- Use headless mode (default in CI)
- Set `CI=true` environment variable
- Store Auth0 test credentials in GitHub Secrets
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
pnpm run test:e2e:debug
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

### 4. Auth0 Testing
- Auth0 Universal Login UI may change
- Use flexible selectors that work across Auth0 versions
- Test redirect flow rather than Auth0 UI details
- Consider mocking Auth0 for unit tests

## Known Limitations

1. **Auth0 Dependency**: Full authentication tests require real Auth0 credentials
2. **Browser Download**: First run downloads ~280MB of browser binaries
3. **Server Requirement**: Tests expect backend server at `localhost:8080`
4. **Sequential Execution**: Auth0 tests should run sequentially to avoid conflicts

## Troubleshooting

### Tests Fail: "Target closed"
- Ensure backend server is running: `cd ../backend && ./bin/server`
- Ensure frontend dev server is running: `pnpm run dev`

### Tests Fail: "Timeout waiting for Auth0"
- Check Auth0 configuration in `.env`
- Verify Auth0 tenant is accessible
- Check network connectivity

### Tests Fail: "Element not found"
- Component might not be implemented yet (Phase 0)
- UI structure might have changed
- Cookie banner might be blocking element

### Slow Test Execution
- Use headed mode only for debugging: `pnpm run test:e2e:headed`
- Run specific test files instead of entire suite
- Increase workers in `playwright.config.ts` (parallel execution)

## Future Improvements

1. **Auth0 Test Automation**
   - Automated test user creation/cleanup
   - Mocked Auth0 for faster tests
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
- [Auth0 Testing Guide](https://auth0.com/docs/test)
- [GDPR Cookie Consent Best Practices](https://gdpr.eu/cookies/)
- Issue #12: Phase 0 Integration Testing
- `boosted-budget-complete-plan.md`: Phase 0 requirements
