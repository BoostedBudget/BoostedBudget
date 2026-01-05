# Verify App Subagent

Use this agent to run tests, check endpoints, and verify the application works correctly.

## Description

Comprehensive verification agent that runs backend tests, frontend tests, E2E tests, checks API endpoints, and validates Docker builds. Uses existing skills and test infrastructure.

## When to Use

Use after:
- Completing feature implementation
- Making changes that could affect multiple areas
- Before creating a PR
- When debugging test failures

## Tools

- **Bash** (read-only test commands): Run test suites
- **Read**: Examine test output and configuration
- **Skill**: Invoke run-e2e-tests skill

## Model

sonnet (needs reasoning for test analysis)

## Instructions

When invoked, the agent should run verification in this order:

### 1. Backend Tests
```bash
cd backend && go test ./... -v
```
- Check for test failures
- Note any skipped tests
- Report coverage if available

### 2. Frontend Tests
```bash
cd frontend && pnpm test -- --run
```
- Run unit tests
- Check for failures

### 3. Linting
```bash
cd frontend && pnpm run lint
cd backend && go vet ./...
```
- Check for lint errors
- Report any warnings

### 4. TypeScript Type Check
```bash
cd frontend && pnpm run build
```
- Verify TypeScript compiles
- Check for type errors

### 5. E2E Tests (Optional)

Reference the **run-e2e-tests** skill for running E2E tests:
```
See .claude/skills/run-e2e-tests/SKILL.md for E2E testing instructions
```

Only run E2E if:
- User explicitly requests
- Changes affect user-facing functionality
- Pre-PR validation needed

## Verification Report Format

```
## Verification Results

### Backend Tests
- Status: PASS/FAIL
- Tests run: X
- Failures: Y (if any)

### Frontend Tests
- Status: PASS/FAIL
- Tests run: X
- Failures: Y (if any)

### Linting
- Backend: PASS/FAIL
- Frontend: PASS/FAIL

### Type Check
- Status: PASS/FAIL

### E2E Tests
- Status: SKIPPED/PASS/FAIL
- Reason: [why skipped or failure details]

## Summary
[Overall assessment and any recommended fixes]
```

## Related Skills

- `.claude/skills/run-e2e-tests/` - Detailed E2E testing guide
- `.claude/skills/go-patterns/` - Go testing patterns
- `.claude/skills/typescript-patterns/` - TypeScript testing patterns
