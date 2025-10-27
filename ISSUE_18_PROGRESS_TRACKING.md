# Issue #18 Account Deletion Flow - Detailed Progress Tracking

**Last Updated**: Starting GREEN Phase
**Branch**: `feature/18-account-deletion-flow`
**Current Phase**: GREEN (Implementing to pass tests)

---

## ✅ RED Phase Complete (100%)

### Test Files Created
- [x] `deletion_service_test.go` - 13 tests
- [x] `email_service_test.go` - 4 tests
- [x] `auth0_service_test.go` - 5 tests
- [x] `deletion_handler_test.go` - 5 tests
- [x] `deletion_worker_test.go` - 5 tests
- [x] Verified all tests FAIL (expected)
- [x] Committed RED phase

**Total Tests**: 32 failing tests (as expected in TDD)

---

## 🟢 GREEN Phase In Progress

### Implementation Checklist

#### 1. Configuration (config.go) ✅ COMPLETE
**Status**: Implemented
**File**: `backend/internal/config/config.go`

**Changes Made**:
```go
// Added to Config struct:
- SendGridAPIKey, SendGridFromEmail, SendGridFromName
- DeletionGracePeriodDays (int, default 30)
- DeletionWorkerInterval (string, default "1h")
- DeletionTokenSecret (string)
- Auth0ManagementClientID, Auth0ManagementClientSecret
- FrontendURL (string, default "http://localhost:5173")

// Added helper:
- getEnvAsInt() for parsing integer environment variables
```

**Environment Variables Required**:
```bash
# Email
SENDGRID_API_KEY=xxx
SENDGRID_FROM_EMAIL=noreply@boostedbudget.com
SENDGRID_FROM_NAME=BoostedBudget

# Deletion
DELETION_GRACE_PERIOD_DAYS=30
DELETION_WORKER_INTERVAL=1h
DELETION_TOKEN_SECRET=your-secret-here

# Auth0 Management
AUTH0_MANAGEMENT_CLIENT_ID=xxx
AUTH0_MANAGEMENT_CLIENT_SECRET=xxx

# Frontend
FRONTEND_URL=https://boostedbudget.com
```

**Tests Affected**: All tests now have access to config values

---

#### 2. Auth0Service (auth0_service.go) ⏳ NEXT
**Status**: Not started
**File**: `backend/internal/services/auth0_service.go`

**Interface to Implement**:
```go
type Auth0Service interface {
    DeleteUser(ctx context.Context, auth0UserID string) error
    GetManagementToken(ctx context.Context) (string, error)
}
```

**Implementation Plan**:
- Create auth0Service struct with domain, clientID, clientSecret, httpClient
- Implement M2M token fetching (client credentials flow)
- Cache token with expiration (24 hours)
- Implement DeleteUser method calling Auth0 Management API DELETE /api/v2/users/{id}
- Handle 404 as success (user already deleted)
- Log errors but don't fail deletion if Auth0 fails

**Auth0 Management API Endpoints**:
- POST `https://{domain}/oauth/token` - Get M2M token
- DELETE `https://{domain}/api/v2/users/{id}` - Delete user

**Tests to Pass**: 5 tests in auth0_service_test.go

---

#### 3. EmailService (email_service.go) ⏳ PENDING
**Status**: Not started
**File**: `backend/internal/services/email_service.go`

**Interface to Implement**:
```go
type EmailService interface {
    SendDeletionConfirmation(ctx context.Context, email, cancellationToken string, scheduledDate time.Time) error
}
```

**Implementation Plan**:
- Use SendGrid Go SDK
- Create email template with:
  - Scheduled deletion date
  - Cancellation link: {FrontendURL}/user/delete/cancel/{token}
  - Clear instructions
  - Support contact info
- Validate email format before sending
- Handle SendGrid errors gracefully
- Return error only for validation failures, log SendGrid errors

**SendGrid Integration**:
- Package: `github.com/sendgrid/sendgrid-go`
- API: `POST https://api.sendgrid.com/v3/mail/send`

**Email Template**:
```
Subject: Account Deletion Request - BoostedBudget

Hi,

We've received your request to delete your BoostedBudget account.

Your account is scheduled for permanent deletion on: {scheduledDate}

If you did NOT request this deletion, or if you change your mind, you can cancel by clicking:

{cancellationLink}

What happens next:
- Your account remains accessible until {scheduledDate}
- All data will be permanently deleted on {scheduledDate}
- This action cannot be undone after {scheduledDate}

Questions? Contact support@boostedbudget.com

Best regards,
The BoostedBudget Team
```

**Tests to Pass**: 4 tests in email_service_test.go

---

#### 4. ProfileRepository Extensions ⏳ PENDING
**Status**: Not started
**File**: `backend/internal/repository/profile_repository.go`

**Methods to Add**:
```go
MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error
CancelDeletion(ctx context.Context, userID string) error
GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error)
DeleteUserCascade(ctx context.Context, userID string) error
```

**Implementation Plan**:

**MarkForDeletion**:
```sql
UPDATE users
SET deletion_requested_at = NOW(),
    deletion_scheduled_at = $1,
    updated_at = NOW()
WHERE id = $2
```

**CancelDeletion**:
```sql
UPDATE users
SET deletion_requested_at = NULL,
    deletion_scheduled_at = NULL,
    updated_at = NOW()
WHERE id = $1
```

**GetUsersScheduledForDeletion**:
```sql
SELECT * FROM users
WHERE deletion_scheduled_at IS NOT NULL
  AND deletion_scheduled_at <= NOW()
  AND is_deleted = FALSE
ORDER BY deletion_scheduled_at ASC
```

**DeleteUserCascade** (10-step process):
```go
1. BEGIN TRANSACTION
2. DELETE FROM transactions WHERE user_id IN (SELECT id FROM accounts WHERE user_id = $1)
3. DELETE FROM accounts WHERE user_id = $1
4. DELETE FROM linked_items WHERE user_id = $1
5. DELETE FROM webhook_events WHERE item_id IN (SELECT id FROM linked_items WHERE user_id = $1)
6. DELETE FROM subscription_overrides WHERE user_id = $1
7. DELETE FROM privacy_settings WHERE user_id = $1
8. UPDATE audit_logs SET user_id = NULL WHERE user_id = $1 (anonymize, don't delete)
9. DELETE FROM user_identities WHERE user_id = $1
10. UPDATE users SET is_deleted = TRUE, updated_at = NOW() WHERE id = $1
11. COMMIT TRANSACTION
```

**Tests to Pass**: Integration with deletion_service_test.go

---

#### 5. DeletionService (deletion_service.go) ⏳ PENDING
**Status**: Not started
**File**: `backend/internal/services/deletion_service.go`

**Interface to Implement**:
```go
type DeletionService interface {
    RequestDeletion(ctx context.Context, userID, email string) (token string, scheduledAt time.Time, error)
    CancelDeletion(ctx context.Context, token string) error
    ExecuteDeletion(ctx context.Context, userID string) error
}
```

**Implementation Plan**:

**RequestDeletion**:
1. Calculate scheduledAt = NOW() + gracePeriodDays
2. Generate JWT token with:
   - user_id claim
   - purpose: "deletion_cancellation"
   - exp: scheduledAt + 5 days (buffer)
3. Call profileRepo.MarkForDeletion(userID, scheduledAt)
4. Call emailService.SendDeletionConfirmation(email, token, scheduledAt)
5. Call auditLog.Log("user.deletion_requested", "user", userID)
6. Return token, scheduledAt, nil

**CancelDeletion**:
1. Parse and validate JWT token
2. Extract userID from token claims
3. Verify token not expired
4. Check Redis if token already used (replay protection)
5. Call profileRepo.CancelDeletion(userID)
6. Mark token as used in Redis (TTL = grace period)
7. Call auditLog.Log("user.deletion_cancelled", "user", userID)
8. Return nil

**ExecuteDeletion**:
1. Get user from profileRepo.GetUserByID(userID)
2. If user.IsDeleted, return nil (idempotent)
3. Call profileRepo.DeleteUserCascade(userID)
4. Call auth0Service.DeleteUser(user.Auth0UserID) - log errors, don't fail
5. Call auditLog.Log("user.deleted", "user", userID)
6. Return nil

**JWT Token Generation**:
```go
token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
    "user_id": userID,
    "purpose": "deletion_cancellation",
    "exp": scheduledAt.Add(5*24*time.Hour).Unix(),
    "iat": time.Now().Unix(),
})
tokenString, _ := token.SignedString([]byte(deletionTokenSecret))
```

**Tests to Pass**: 13 tests in deletion_service_test.go

---

#### 6. DeletionHandler (deletion_handler.go) ⏳ PENDING
**Status**: Not started
**File**: `backend/internal/handlers/deletion_handler.go`

**Handlers to Implement**:
```go
type DeletionHandler struct {
    deletionService DeletionService
}

func (h *DeletionHandler) RequestDeletion(c echo.Context) error
func (h *DeletionHandler) CancelDeletion(c echo.Context) error
```

**RequestDeletion (POST /api/v1/user/delete)**:
1. Extract userID from context (requires Auth0 middleware)
2. Extract email from context or claims
3. Call deletionService.RequestDeletion(userID, email)
4. Return 200 with JSON: {message, scheduled_at, cancellation_token}
5. Handle errors: 401 if not authenticated, 500 for service errors

**CancelDeletion (GET /api/v1/user/delete/cancel/:token)**:
1. Extract token from URL params
2. Call deletionService.CancelDeletion(token)
3. Return 200 with JSON: {message: "Account deletion cancelled"}
4. Handle errors: 400 for invalid token, 500 for service errors

**Tests to Pass**: 5 tests in deletion_handler_test.go

---

#### 7. DeletionWorker (deletion_worker.go) ⏳ PENDING
**Status**: Not started
**File**: `backend/internal/workers/deletion_worker.go`

**Worker to Implement**:
```go
type DeletionWorker struct {
    deletionService DeletionService
    profileRepo     ProfileRepository
    interval        time.Duration
}

func (w *DeletionWorker) Start(ctx context.Context)
func (w *DeletionWorker) ProcessScheduledDeletions(ctx context.Context) error
```

**Start Method**:
```go
func (w *DeletionWorker) Start(ctx context.Context) {
    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()

    log.Println("Deletion worker started")

    for {
        select {
        case <-ticker.C:
            if err := w.ProcessScheduledDeletions(ctx); err != nil {
                log.Printf("Error processing deletions: %v", err)
            }
        case <-ctx.Done():
            log.Println("Deletion worker shutting down")
            return
        }
    }
}
```

**ProcessScheduledDeletions Method**:
```go
func (w *DeletionWorker) ProcessScheduledDeletions(ctx context.Context) error {
    users, err := w.profileRepo.GetUsersScheduledForDeletion(ctx)
    if err != nil {
        return fmt.Errorf("failed to fetch users: %w", err)
    }

    log.Printf("Processing %d scheduled deletions", len(users))

    for _, user := range users {
        if err := w.deletionService.ExecuteDeletion(ctx, user.ID); err != nil {
            log.Printf("Failed to delete user %s: %v", user.ID, err)
            // Continue to next user (don't fail entire batch)
        } else {
            log.Printf("Successfully deleted user %s", user.ID)
        }
    }

    return nil
}
```

**Tests to Pass**: 5 tests in deletion_worker_test.go

---

#### 8. Wire Up in main.go ⏳ PENDING
**Status**: Not started
**File**: `backend/cmd/server/main.go`

**Integration Steps**:
```go
// After existing services...

// Create deletion-related services
auth0Service := services.NewAuth0Service(
    cfg.Auth0Domain,
    cfg.Auth0ManagementClientID,
    cfg.Auth0ManagementClientSecret,
)

emailService := services.NewEmailService(
    cfg.SendGridAPIKey,
    cfg.SendGridFromEmail,
    cfg.FrontendURL,
)

deletionService := services.NewDeletionService(
    profileRepo,
    emailService,
    auth0Service,
    auditLogger,
    cfg.DeletionGracePeriodDays,
)

// Create deletion handler
deletionHandler := handlers.NewDeletionHandler(deletionService)

// Register routes (in authenticated group)
authGroup.POST("/user/delete", deletionHandler.RequestDeletion)
e.GET("/api/v1/user/delete/cancel/:token", deletionHandler.CancelDeletion) // Public endpoint

// Start deletion worker
interval, _ := time.ParseDuration(cfg.DeletionWorkerInterval)
deletionWorker := workers.NewDeletionWorker(deletionService, profileRepo, interval)
go func() {
    log.Println("Starting deletion worker...")
    deletionWorker.Start(ctx)
}()
```

---

#### 9. OpenAPI Spec Update ⏳ PENDING
**Status**: Not started
**File**: `api/openapi.yaml`

**Endpoints to Add**:
```yaml
/api/v1/user/delete:
  post:
    summary: Request account deletion
    security:
      - Auth0: []
    responses:
      200:
        content:
          application/json:
            schema:
              properties:
                message: string
                scheduled_at: string (date-time)
                cancellation_token: string

/api/v1/user/delete/cancel/{token}:
  get:
    summary: Cancel account deletion
    parameters:
      - name: token
        in: path
        required: true
    responses:
      200:
        content:
          application/json:
            schema:
              properties:
                message: string
```

**After update**: Run `make generate` to regenerate API clients

---

## 📊 Test Progress

### Backend Tests
- [ ] auth0_service_test.go (0/5 passing)
- [ ] email_service_test.go (0/4 passing)
- [ ] deletion_service_test.go (0/13 passing)
- [ ] deletion_handler_test.go (0/5 passing)
- [ ] deletion_worker_test.go (0/5 passing)

**Total**: 0/32 tests passing (expected during GREEN phase development)

---

## 🗂️ Files Created/Modified

### Created (RED Phase):
- ✅ `backend/internal/services/deletion_service_test.go`
- ✅ `backend/internal/services/email_service_test.go`
- ✅ `backend/internal/services/auth0_service_test.go`
- ✅ `backend/internal/handlers/deletion_handler_test.go`
- ✅ `backend/internal/workers/deletion_worker_test.go`
- ✅ `ISSUE_18_IMPLEMENTATION_STATUS.md`
- ✅ `ISSUE_18_PROGRESS_TRACKING.md` (this file)

### Modified (GREEN Phase):
- ✅ `backend/internal/config/config.go` - Added deletion config fields

### To Create (GREEN Phase):
- ⏳ `backend/internal/services/auth0_service.go`
- ⏳ `backend/internal/services/email_service.go`
- ⏳ `backend/internal/services/deletion_service.go`
- ⏳ `backend/internal/handlers/deletion_handler.go`
- ⏳ `backend/internal/workers/deletion_worker.go`

### To Modify (GREEN Phase):
- ⏳ `backend/internal/repository/profile_repository.go` - Add deletion methods
- ⏳ `backend/cmd/server/main.go` - Wire up services
- ⏳ `api/openapi.yaml` - Add deletion endpoints

---

## 💡 Key Decisions & Learnings

### Decision 1: JWT Tokens for Cancellation
**Why**: Self-contained, no database table needed, automatic expiration
**Trade-off**: Can't revoke individually, but mitigated with Redis replay protection
**Implementation**: HS256 JWT with user_id, purpose, exp claims

### Decision 2: Graceful Auth0 Failure
**Why**: User data is already deleted from our system, Auth0 can be cleaned up separately
**Implementation**: Log error but don't fail deletion, create orphan cleanup job later

### Decision 3: Application-Level Cascade Deletion
**Why**: Better audit trail, more control, explicit ordering
**Trade-off**: More code vs simpler DB CASCADE, but better visibility

### Decision 4: Anonymize Audit Logs
**Why**: GDPR Article 17 exception for legal compliance allows retention
**Implementation**: SET user_id = NULL instead of DELETE, keep logs for 7 years

### Decision 5: Hourly Worker Polling
**Why**: Simple for MVP, sufficient for expected deletion volume
**Trade-off**: Less efficient than queue-based, but easier to implement and monitor
**Future**: Consider RabbitMQ queue if volume increases

---

## 🚀 Next Immediate Steps

1. ✅ **Config Update** - COMPLETE
2. ⏳ **Implement Auth0Service** - NEXT
   - Create auth0_service.go
   - Implement M2M token fetching
   - Implement DeleteUser method
   - Run tests: `go test ./internal/services/auth0_service_test.go -v`
3. ⏳ **Implement EmailService** - After Auth0Service
4. ⏳ **Extend ProfileRepository** - After EmailService
5. ⏳ **Implement DeletionService** - After Repository
6. ⏳ **Implement Handler** - After DeletionService
7. ⏳ **Implement Worker** - After Handler
8. ⏳ **Wire up main.go** - After all services
9. ⏳ **Verify all tests pass** - Final GREEN phase check

---

## 🎯 Success Criteria

- [x] RED Phase: All 32 tests written and failing
- [ ] GREEN Phase: All 32 tests passing
- [ ] REFACTOR Phase: Code cleaned up, >80% coverage
- [ ] Integration tests passing
- [ ] E2E tests passing
- [ ] PR created and merged
- [ ] Deployed to staging
- [ ] QA testing complete

---

**Last Commit**: `#18: RED Phase - Write all failing tests for account deletion flow`
**Next Commit**: `#18: GREEN Phase - Implement Auth0Service with Management API`
