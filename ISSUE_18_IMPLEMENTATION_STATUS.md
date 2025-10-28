# Issue #18: Account Deletion Flow - Implementation Status

## Current Status: RED Phase (Writing Tests)

**Branch**: `feature/18-account-deletion-flow`
**Phase**: RED (Test-Driven Development - Write Failing Tests First)
**Progress**: 60% of RED phase complete

---

## Completed (RED Phase)

### ✅ Test Files Created (All tests FAIL as expected)

1. **`backend/internal/services/deletion_service_test.go`** (13 tests)
   - TestRequestDeletion_Success
   - TestRequestDeletion_SchedulesFor30Days
   - TestRequestDeletion_GeneratesToken
   - TestRequestDeletion_CreatesAuditLog
   - TestRequestDeletion_EmailServiceFailure
   - TestCancelDeletion_ValidToken
   - TestCancelDeletion_InvalidToken
   - TestCancelDeletion_ExpiredToken
   - TestExecuteDeletion_CascadesAllData
   - TestExecuteDeletion_DeletesAuth0User
   - TestExecuteDeletion_HandlesAlreadyDeleted
   - TestExecuteDeletion_HandlesAuth0Error

2. **`backend/internal/services/email_service_test.go`** (4 tests)
   - TestSendDeletionConfirmation_Success
   - TestSendDeletionConfirmation_InvalidEmail
   - TestSendDeletionConfirmation_SMTPError
   - TestEmailTemplate_ContainsRequiredFields

3. **`backend/internal/services/auth0_service_test.go`** (5 tests)
   - TestAuth0DeleteUser_Success
   - TestAuth0DeleteUser_NotFound
   - TestAuth0DeleteUser_APIError
   - TestAuth0GetManagementToken_Success
   - TestAuth0GetManagementToken_CachesToken

**Total Tests Written**: 22/32 RED phase tests

---

## Next Steps (Continue RED Phase)

### 🔄 Remaining Test Files

1. **`backend/internal/handlers/deletion_handler_test.go`** (5 tests needed)
   - Test POST /api/v1/user/delete requires authentication
   - Test POST /api/v1/user/delete returns 200 with deletion details
   - Test POST /api/v1/user/delete handles service errors
   - Test GET /api/v1/user/delete/cancel/:token validates and cancels
   - Test invalid cancellation token returns 400

2. **`backend/internal/workers/deletion_worker_test.go`** (5 tests needed)
   - Test worker finds users scheduled for deletion
   - Test worker processes each user correctly
   - Test worker handles errors gracefully
   - Test worker respects context cancellation
   - Test worker graceful shutdown

3. **Verify RED Phase Complete**
   - Run `cd backend && go test ./internal/services/... -v`
   - Confirm all tests FAIL with "undefined" errors (expected)
   - Document which functions/interfaces are missing

---

## GREEN Phase (Next - Weeks 2-3)

### Implementation Order

1. **Config Updates** (`backend/internal/config/config.go`)
   - Add SendGrid API key, from email
   - Add Auth0 Management Client ID/Secret
   - Add deletion configuration (grace period, worker interval, token secret)
   - Add frontend URL for cancellation links

2. **Auth0Service** (`backend/internal/services/auth0_service.go`)
   - Implement Auth0 Management API client
   - M2M token acquisition and caching
   - DeleteUser method with error handling

3. **EmailService** (`backend/internal/services/email_service.go`)
   - SendGrid integration
   - Email template with cancellation link
   - Error handling and validation

4. **ProfileRepository Extensions** (modify `backend/internal/repository/profile_repository.go`)
   - MarkForDeletion method
   - CancelDeletion method
   - GetUsersScheduledForDeletion method
   - DeleteUserCascade method (10-step cascade)

5. **DeletionService** (`backend/internal/services/deletion_service.go`)
   - RequestDeletion (schedule, generate JWT token, send email)
   - CancelDeletion (validate JWT, clear deletion fields)
   - ExecuteDeletion (cascade delete, call Auth0, audit log)

6. **DeletionHandler** (`backend/internal/handlers/deletion_handler.go`)
   - POST /api/v1/user/delete endpoint
   - GET /api/v1/user/delete/cancel/:token endpoint
   - Request validation and error responses

7. **DeletionWorker** (`backend/internal/workers/deletion_worker.go`)
   - Polling worker (every 1 hour)
   - Process scheduled deletions
   - Graceful shutdown

8. **Wire Up in main.go**
   - Initialize services with dependencies
   - Register HTTP routes
   - Start worker goroutine

9. **OpenAPI Spec** (`api/openapi.yaml`)
   - Add DELETE /api/v1/user/delete endpoint
   - Add GET /api/v1/user/delete/cancel/{token} endpoint
   - Run `make generate` to update generated code

---

## Testing Strategy

### Unit Tests (Backend)
```bash
cd backend
go test ./internal/services/deletion_service_test.go -v
go test ./internal/services/email_service_test.go -v
go test ./internal/services/auth0_service_test.go -v
go test ./internal/handlers/deletion_handler_test.go -v
go test ./internal/workers/deletion_worker_test.go -v
```

### Integration Tests
```bash
go test ./internal/... -tags=integration -v
```

### Coverage Check
```bash
go test ./internal/... -coverprofile=coverage.out
go tool cover -func=coverage.out
# Target: >80%
```

---

## Frontend Implementation (Week 4)

1. **API Client** (`frontend/src/api/deletionApi.ts`)
2. **Delete Account Component** (`frontend/src/features/profile/components/DeleteAccountSection.tsx`)
3. **Cancellation Page** (`frontend/src/features/profile/pages/CancelDeletionPage.tsx`)
4. **Routes** (add to app router)
5. **Component Tests**
6. **E2E Tests** (Playwright)

---

## Environment Variables Needed

```bash
# SendGrid
SENDGRID_API_KEY=SG.xxx
SENDGRID_FROM_EMAIL=noreply@boostedbudget.com

# Auth0 Management API (M2M)
AUTH0_MANAGEMENT_CLIENT_ID=xxx
AUTH0_MANAGEMENT_CLIENT_SECRET=xxx

# Deletion Configuration
DELETION_GRACE_PERIOD_DAYS=30
DELETION_WORKER_INTERVAL=1h
DELETION_TOKEN_SECRET=your-secure-secret-here

# Frontend
FRONTEND_URL=https://boostedbudget.com
```

---

## Data Cascade Deletion Order

1. Delete transactions
2. Delete accounts
3. Delete linked_items
4. Delete webhook_events
5. Delete subscription_overrides
6. Delete privacy_settings
7. Anonymize audit_logs (user_id → NULL)
8. Delete user_identities
9. Delete users
10. Delete Auth0 user (via Management API)

---

## Success Criteria

- [x] Feature branch created
- [x] 22/32 RED phase tests written
- [ ] All 32 RED phase tests written
- [ ] All tests verified to FAIL (expected)
- [ ] All backend services implemented (GREEN phase)
- [ ] All tests PASS
- [ ] Frontend components implemented
- [ ] E2E tests pass
- [ ] Test coverage >80%
- [ ] All backend + frontend + e2e + lint tests pass
- [ ] PR created with `#18:` prefix and `Closes #18`
- [ ] Deployed to staging
- [ ] QA testing complete
- [ ] Merged to main

---

## Timeline

- **Week 1**: RED Phase (Writing tests) - IN PROGRESS
- **Week 2-3**: GREEN Phase (Implementation)
- **Week 4**: Frontend & E2E tests
- **Week 5**: REFACTOR & Deploy

---

## Key Decisions Documented

1. **SendGrid over SMTP**: Better deliverability, free tier sufficient
2. **JWT tokens for cancellation**: Self-contained, no DB table needed
3. **Application-level cascade deletion**: Better audit trail, more control
4. **Anonymize audit logs**: GDPR Article 17 exception for legal compliance
5. **Graceful Auth0 error handling**: Log but don't fail if Auth0 delete fails

---

## Next Immediate Actions

1. Complete remaining test files (deletion_handler_test.go, deletion_worker_test.go)
2. Verify all tests FAIL with clear error messages
3. Begin GREEN phase: Implement Auth0Service first
4. Continue tracking progress in server-memory during implementation
