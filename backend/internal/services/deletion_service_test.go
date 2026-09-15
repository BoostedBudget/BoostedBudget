package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockProfileRepositoryForDeletion for testing deletion service
type MockProfileRepositoryForDeletion struct {
	mock.Mock
	// LastClaimKind records the repository.DeletionClaimKind the last ClaimForDeletion call passed
	// (Issue #1440, T0). Asserted by TestExecuteDeletion_ThreadsClaimKindToRepository.
	LastClaimKind repository.DeletionClaimKind

	// Issue #1494 — CountUncleanedLinkedItems's fields. Field-driven rather than testify-driven on
	// purpose: this method is reached ONLY on ExecuteDeletion's Plaid-less branch, which several
	// pre-existing tests in this file exercise (they construct the service with a nil
	// plaidCleanupSvc), and an unmatched testify expectation panics. The zero value is 0/nil — "no
	// outstanding Plaid work" — which is exactly the behaviour those tests were written against.
	UncleanedLinkedItemCount int
	// UncleanedLinkedItemCountErr, when set, makes the count fail — the fail-closed case.
	UncleanedLinkedItemCountErr error
	// UncleanedLinkedItemCountCalls counts the calls, so a test can pin that the Plaid-less branch
	// actually consulted the count rather than reaching its conclusion some other way.
	UncleanedLinkedItemCountCalls int

	// DeleteUserCascadeAffectedOwnerIDs is DeleteUserCascade's third return value (GDPR round-6
	// finding P1) — field-driven for the same reason as UncleanedLinkedItemCount above: every
	// pre-existing `.On("DeleteUserCascade", ...).Return(bool, error)` two-value expectation in this
	// file predates the third return value, and a testify `.Return()` call with the wrong arity
	// panics rather than defaulting. The zero value is nil — "no cross-user owner was affected" —
	// exactly the behaviour every pre-existing test was written against. A test that cares about the
	// new fix sets this before calling ExecuteDeletion.
	DeleteUserCascadeAffectedOwnerIDs []string

	// AuthorisedFn answers DeletionStillAuthorised — ExecuteDeletion's use-time re-verification of
	// the authorisation it was claimed under. Field-driven for the same reason as
	// CountUncleanedLinkedItems above: every pre-existing test in this file reaches it, and a nil
	// AuthorisedFn means "still authorised", which is exactly the behaviour they were written
	// against. Tests that care flip it mid-flow.
	AuthorisedFn func(ctx context.Context) (bool, error)
	// AuthorisedKinds / AuthorisedOccurrences record what each recheck was ASKED, so a test can pin
	// that the question matches the authorisation this run was claimed under rather than a union.
	AuthorisedKinds       []repository.DeletionClaimKind
	AuthorisedOccurrences []*time.Time

	// Round 15, finding D2 — HouseholdOwnerBlocksDeletion's fields. Field-driven for the same reason
	// as CountUncleanedLinkedItems above: ExecuteDeletion now asks this on EVERY run, before any
	// external cleanup, so every pre-existing test in this package reaches it and an unmatched testify
	// expectation would panic. The zero value is false/nil — "no household blocks this deletion" —
	// which is exactly the behaviour those tests were written against.
	HouseholdOwnerBlocks bool
	// HouseholdOwnerBlocksErr, when set, makes the precondition undeterminable — the fail-closed case.
	HouseholdOwnerBlocksErr error
	// HouseholdOwnerBlocksCalls counts the calls, so a test can pin that the decision was made from
	// the precondition rather than reached some other way.
	HouseholdOwnerBlocksCalls int
}

// HouseholdOwnerBlocksDeletion — round 15, finding D2. See the struct's field comments for why this
// is field-driven rather than a testify expectation.
func (m *MockProfileRepositoryForDeletion) HouseholdOwnerBlocksDeletion(ctx context.Context, userID string) (bool, error) {
	m.HouseholdOwnerBlocksCalls++
	if m.HouseholdOwnerBlocksErr != nil {
		return false, m.HouseholdOwnerBlocksErr
	}
	return m.HouseholdOwnerBlocks, nil
}

// DeletionStillAuthorised — the use-time recheck. See the struct's field comments.
func (m *MockProfileRepositoryForDeletion) DeletionStillAuthorised(ctx context.Context, userID string, kind repository.DeletionClaimKind, expectedDeletionRequestedAt *time.Time) (bool, error) {
	m.AuthorisedKinds = append(m.AuthorisedKinds, kind)
	m.AuthorisedOccurrences = append(m.AuthorisedOccurrences, expectedDeletionRequestedAt)
	if m.AuthorisedFn == nil {
		return true, nil
	}
	return m.AuthorisedFn(ctx)
}

func (m *MockProfileRepositoryForDeletion) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	args := m.Called(ctx, clerkUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	args := m.Called(ctx, subscriptionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	args := m.Called(ctx, tx, user)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	args := m.Called(ctx, clerkUserID)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	args := m.Called(ctx, userID, scheduledAt)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) CancelDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// RecordDeletionResidue (Issue #1504) records that a deletion performed an irreversible external
// effect. See repository.ProfileRepository for the contract.
//
// ⚠ ROUND 15: THE PERMISSIVE DEFAULT IS DELIBERATE. Since each irreversible external Paddle call now
// records itself AS IT IS ISSUED (deletionService.recordIssuedExternalEffect) rather than being
// inferred from an in-memory flag at the end of the run, this method is reached by EVERY test that
// gets as far as the Paddle leg — some fifty of them, almost none of which are about the residue
// record. Registering a permissive default here rather than in fifty test bodies keeps those tests
// about their own subject.
//
// It does not weaken the residue tests. testify still records the call, so their AssertCalled /
// AssertNotCalled assertions see exactly what they saw before; and a test that registers its own
// expectation (e.g. one that makes the write fail) registered it during setup, so testify finds that
// one first and this default is never installed at all.
func (m *MockProfileRepositoryForDeletion) RecordDeletionResidue(ctx context.Context, userID string, effect repository.DeletionResidueEffect) error {
	if !m.hasExpectationFor("RecordDeletionResidue") {
		m.On("RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	}
	args := m.Called(ctx, userID, effect)
	return args.Error(0)
}

// hasExpectationFor reports whether the test has already programmed this method. Reads
// ExpectedCalls without testify's own mutex, which is safe here and only here: these mocks are
// programmed during single-goroutine test setup and this check runs on the same goroutine as the
// call it guards.
func (m *MockProfileRepositoryForDeletion) hasExpectationFor(method string) bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == method {
			return true
		}
	}
	return false
}

// #1531 G5: m.Called below is still programmed with the pre-existing two-value
// `.On(...).Return(bool, error)` shape — the third return (affected owner IDs) comes back through
// the DeleteUserCascadeAffectedOwnerIDs side-channel field instead, per that field's own doc
// comment. Benign today, but it is an arity foot-gun for a future edit: a caller who "helpfully"
// updates a test to `.Return(true, []string{...}, nil)` will compile fine and panic at test run
// time — not from an out-of-range index (a three-value `.Return(...)` produces a valid 3-element
// Arguments), but because args.Error(1) below would then receive the []string at index 1 and
// panic on the failed type assertion to error, not a bool/error pair shifted by one. Do not
// restructure this mock to close that gap — every pre-existing expectation in this file predates
// the third return value; this comment is the guard instead.
func (m *MockProfileRepositoryForDeletion) DeleteUserCascade(ctx context.Context, userID string, expectedDeletionRequestedAt *time.Time) (bool, []string, error) {
	args := m.Called(ctx, userID, expectedDeletionRequestedAt)
	return args.Bool(0), m.DeleteUserCascadeAffectedOwnerIDs, args.Error(1)
}

// CountUncleanedLinkedItems — Issue #1494. See the struct's field comments for why this is
// field-driven rather than a testify expectation.
func (m *MockProfileRepositoryForDeletion) CountUncleanedLinkedItems(ctx context.Context, userID string) (int, error) {
	m.UncleanedLinkedItemCountCalls++
	if m.UncleanedLinkedItemCountErr != nil {
		return 0, m.UncleanedLinkedItemCountErr
	}
	return m.UncleanedLinkedItemCount, nil
}

// Issue #679, #680: Trial expiration methods
func (m *MockProfileRepositoryForDeletion) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	args := m.Called(ctx, withinDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	args := m.Called(ctx, userID, status, trialStartedAt, trialEndsAt, hadTrial)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	args := m.Called(ctx, userID, paddleCustomerID)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt, subscriptionID)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// Retention tracking stubs (#930)
func (m *MockProfileRepositoryForDeletion) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	args := m.Called(ctx, userID)
	return args.String(0), args.Error(1)
}
func (m *MockProfileRepositoryForDeletion) SetRetentionDates(ctx context.Context, userID string, guard repository.RetentionClockGuard, retentionMonths int) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) ClearRetentionDates(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *MockProfileRepositoryForDeletion) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}

// Dunning methods (#898)
func (m *MockProfileRepositoryForDeletion) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	return []domain.User{}, nil
}

// GetUsersNeedingRetentionClockRepair, RepairRetentionClock (Issue #1413 H5 residual) are not
// exercised by these deletion-service tests; stubbed to satisfy the widened ProfileRepository
// interface.
func (m *MockProfileRepositoryForDeletion) GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error) {
	return []*domain.User{}, nil
}

func (m *MockProfileRepositoryForDeletion) RepairRetentionClock(ctx context.Context, userID string, startedAt time.Time, retentionMonths int) (bool, error) {
	return false, nil
}

// GetUsersNeedingPlaidDisconnectRepair (Issue #1413 round-7 grace-window repair path) is not
// exercised by these deletion-service tests; stubbed to satisfy the widened ProfileRepository
// interface.
func (m *MockProfileRepositoryForDeletion) GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error) {
	return []*domain.User{}, nil
}

func (m *MockProfileRepositoryForDeletion) ClaimPlaidDisconnectIfPastDue(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

func (m *MockProfileRepositoryForDeletion) ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

// CountRetentionDeletionsWithheld is Issue #1569 T1's withholding-count read. Never exercised by
// this suite: it exists so MockProfileRepositoryForDeletion keeps satisfying
// repository.ProfileRepository.
func (m *MockProfileRepositoryForDeletion) CountRetentionDeletionsWithheld(ctx context.Context) (repository.RetentionWithholdingCounts, error) {
	return repository.RetentionWithholdingCounts{}, nil
}

// ClaimForDeletion (Issue #1413 Critical finding C2, Codex review round 2) is stubbed via
// m.Called so individual tests can assert the claimed/not-claimed branch explicitly — unlike
// the other narrow-interface CAS stubs above, ExecuteDeletion's own tests need to control this
// return value directly.
func (m *MockProfileRepositoryForDeletion) ClaimForDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) (bool, error) {
	m.LastClaimKind = kind
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, nil
}

func (m *MockProfileRepositoryForDeletion) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	args := m.Called(ctx, tx, userID, plainEmail)
	return args.Error(0)
}

// MockEmailService for testing
type MockEmailService struct {
	mock.Mock
}

func (m *MockEmailService) SendDeletionConfirmation(ctx context.Context, email, cancellationToken string, scheduledDate time.Time) error {
	args := m.Called(ctx, email, cancellationToken, scheduledDate)
	return args.Error(0)
}

func (m *MockEmailService) SendNotificationEmail(ctx context.Context, toEmail string, notification *domain.Notification) error {
	args := m.Called(ctx, toEmail, notification)
	return args.Error(0)
}

// SendHouseholdInvite (#1367 — Phase 3) is not exercised by these deletion tests;
// stubbed to satisfy the expanded EmailService interface.
func (m *MockEmailService) SendHouseholdInvite(ctx context.Context, toEmail, inviterName, householdName, inviteURL string, expiresAt time.Time) error {
	args := m.Called(ctx, toEmail, inviterName, householdName, inviteURL, expiresAt)
	return args.Error(0)
}

// SendHouseholdMemberRemoved (#1371 — Phase 7, Task 3) is not exercised by these
// deletion tests; stubbed to satisfy the expanded EmailService interface.
func (m *MockEmailService) SendHouseholdMemberRemoved(ctx context.Context, toEmail, householdName string, purgeDate time.Time, disbanded bool) error {
	args := m.Called(ctx, toEmail, householdName, purgeDate, disbanded)
	return args.Error(0)
}

// SendHouseholdOverCapacityNotice/Warning (#1371 — Phase 7, Task 6) are not exercised
// by these deletion tests; stubbed to satisfy the expanded EmailService interface.
func (m *MockEmailService) SendHouseholdOverCapacityNotice(ctx context.Context, toEmail, householdName string, activeCount, seatLimit int, deadline time.Time) error {
	args := m.Called(ctx, toEmail, householdName, activeCount, seatLimit, deadline)
	return args.Error(0)
}

func (m *MockEmailService) SendHouseholdOverCapacityWarning(ctx context.Context, toEmail, householdName string, activeCount, seatLimit int, deadline time.Time) error {
	args := m.Called(ctx, toEmail, householdName, activeCount, seatLimit, deadline)
	return args.Error(0)
}

// MockClerkService for testing
type MockClerkService struct {
	mock.Mock
}

func (m *MockClerkService) DeleteUser(ctx context.Context, clerkUserID string) error {
	args := m.Called(ctx, clerkUserID)
	return args.Error(0)
}

func (m *MockClerkService) UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error {
	args := m.Called(ctx, clerkUserID, metadata)
	return args.Error(0)
}

func (m *MockClerkService) UpdateUserEmail(ctx context.Context, clerkUserID, newEmail string) error {
	args := m.Called(ctx, clerkUserID, newEmail)
	return args.Error(0)
}

// MockAuditLogger for testing
type MockAuditLogger struct {
	mock.Mock
}

func (m *MockAuditLogger) Log(ctx context.Context, action, resourceType, resourceID string) error {
	args := m.Called(ctx, action, resourceType, resourceID)
	return args.Error(0)
}

// MockAuditLogRepository implements repository.AuditLogRepository (Log + LogInTx) for testing
// callers that need the transactional write, e.g. PurgeAccount/PurgeInstitution (Issue #1569,
// Task 6).
type MockAuditLogRepository struct {
	mock.Mock
}

func (m *MockAuditLogRepository) Log(ctx context.Context, action, resourceType, resourceID string) error {
	args := m.Called(ctx, action, resourceType, resourceID)
	return args.Error(0)
}

func (m *MockAuditLogRepository) LogInTx(ctx context.Context, tx pgx.Tx, action, resourceType, resourceID string) error {
	args := m.Called(ctx, tx, action, resourceType, resourceID)
	return args.Error(0)
}

// MockWorkClaimsRepository mocks repository.WorkClaimsRepository (Issue #1413 Critical finding
// C2) — the durable, fencing-token-guarded claim mechanism ExecuteDeletion uses for exclusivity
// on ActionAccountDeletion, wired in via SetWorkClaimsRepo.
type MockWorkClaimsRepository struct {
	mock.Mock
}

func (m *MockWorkClaimsRepository) Claim(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (string, bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, lease)
	return args.String(0), args.Bool(1), args.Error(2)
}

func (m *MockWorkClaimsRepository) Complete(ctx context.Context, resourceType, resourceID, action, token string) (bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, token)
	return args.Bool(0), args.Error(1)
}

func (m *MockWorkClaimsRepository) Release(ctx context.Context, resourceType, resourceID, action, token, reason string) (bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, token, reason)
	return args.Bool(0), args.Error(1)
}

// testDeletionClaimToken is the fixed fencing token newAllowingClaimsRepo's Claim stub returns.
// Tests that only care about ExecuteDeletion's OTHER behavior (Paddle/Plaid/cascade ordering,
// etc.) can assert Complete was called with this exact value without having to plumb a real
// per-test token through.
const testDeletionClaimToken = "test-claim-token"

// newAllowingClaimsRepo returns a MockWorkClaimsRepository stubbed to always grant the
// ActionAccountDeletion claim and always complete it successfully — for tests that exercise
// ExecuteDeletion's OTHER behavior and aren't themselves testing the claim mechanism. userID is
// wildcarded (mock.Anything) so one instance works across this file's whole matrix of fixture
// users. Both expectations are .Maybe() because some callers (e.g. the already-deleted /
// belt-and-braces-skip tests) never reach the claim at all.
func newAllowingClaimsRepo() *MockWorkClaimsRepository {
	m := new(MockWorkClaimsRepository)
	m.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil).Maybe()
	m.On("Complete", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, testDeletionClaimToken).
		Return(true, nil).Maybe()
	m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()

	// Issue #1413 H1: cleanupPaddleResources nests a side-effect claim on
	// ActionPaddleCancelSubscription inside the flow claim above. Tests exercising
	// ExecuteDeletion's OTHER behavior (not the side-effect claim mechanism itself) need this
	// permissive too, or any test reaching an active-subscription cancellation would panic on an
	// unexpected Claim call. claimSideEffectAndRun always Releases (never Completes) — see its
	// doc comment in plaid_item_cleanup_service.go for why.
	m.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPaddleCancelSideEffectToken, true, nil).Maybe()
	m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionPaddleCancelSubscription, testPaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()
	return m
}

// testPaddleCancelSideEffectToken is the fixed fencing token newAllowingClaimsRepo's permissive
// ActionPaddleCancelSubscription stub returns.
const testPaddleCancelSideEffectToken = "test-paddle-cancel-side-effect-token"

// TestRequestDeletion_Success tests successful deletion request
func TestRequestDeletion_Success(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	token, scheduledTime, err := service.RequestDeletion(ctx, userID, email)

	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.True(t, scheduledTime.After(time.Now().Add(29*24*time.Hour)))
	assert.True(t, scheduledTime.Before(time.Now().Add(31*24*time.Hour)))
	mockRepo.AssertExpectations(t)
	mockEmail.AssertExpectations(t)
	mockAudit.AssertExpectations(t)
}

// TestRequestDeletion_SchedulesFor30Days verifies 30-day scheduling
func TestRequestDeletion_SchedulesFor30Days(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	gracePeriodDays := 30
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, gracePeriodDays, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	_, scheduledTime, err := service.RequestDeletion(ctx, userID, email)

	assert.NoError(t, err)
	expectedTime := time.Now().Add(time.Duration(gracePeriodDays) * 24 * time.Hour)
	diff := scheduledTime.Sub(expectedTime)
	assert.True(t, diff < time.Minute && diff > -time.Minute, "Scheduled time should be approximately 30 days from now")
}

// TestRequestDeletion_GeneratesToken verifies token generation
func TestRequestDeletion_GeneratesToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	token, _, err := service.RequestDeletion(ctx, userID, email)

	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.True(t, len(token) > 20, "Token should be a reasonable length")
}

// TestRequestDeletion_CreatesAuditLog verifies audit logging
func TestRequestDeletion_CreatesAuditLog(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	_, _, err := service.RequestDeletion(ctx, userID, email)

	assert.NoError(t, err)
	mockAudit.AssertExpectations(t)
	mockAudit.AssertCalled(t, "Log", ctx, "user.deletion_requested", "user", userID)
}

// TestRequestDeletion_EmailServiceFailure tests email failure handling
func TestRequestDeletion_EmailServiceFailure(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(errors.New("email service error"))
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	_, _, err := service.RequestDeletion(ctx, userID, email)

	// Should still succeed even if email fails (deletion is scheduled)
	assert.NoError(t, err)
}

// TestCancelDeletion_ValidToken tests successful cancellation
func TestCancelDeletion_ValidToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	// First request deletion to get a token
	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	token, _, _ := service.RequestDeletion(ctx, userID, email)

	// Now cancel with the token
	mockRepo.On("CancelDeletion", ctx, userID).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_cancelled", "user", userID).Return(nil)

	err := service.CancelDeletion(ctx, token)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "CancelDeletion", ctx, userID)
	mockAudit.AssertCalled(t, "Log", ctx, "user.deletion_cancelled", "user", userID)
}

// TestCancelDeletion_InvalidToken tests invalid token handling
func TestCancelDeletion_InvalidToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	invalidToken := "invalid-token-string"

	err := service.CancelDeletion(ctx, invalidToken)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token")
	mockRepo.AssertNotCalled(t, "CancelDeletion", mock.Anything, mock.Anything)
}

// TestCancelDeletion_ExpiredToken tests expired token handling
func TestCancelDeletion_ExpiredToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	tokenSecret := "test-secret-key"
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, tokenSecret)

	// Create a real expired JWT token (expired 1 hour ago)
	expiredTime := time.Now().Add(-1 * time.Hour)
	claims := &tokenClaims{
		UserID: "user-123",
		Email:  "test@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiredTime),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			Issuer:    "boostedbudget-deletion-service",
			Subject:   "user-123",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	expiredToken, _ := token.SignedString([]byte(tokenSecret))

	err := service.CancelDeletion(ctx, expiredToken)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

// TestExecuteDeletion_CascadesAllData tests complete data deletion
func TestExecuteDeletion_CascadesAllData(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID, mock.Anything)
	mockClerk.AssertCalled(t, "DeleteUser", ctx, user.ClerkUserID)
	// Issue #1507: the `user.deleted` entry is written INSIDE DeleteUserCascade's transaction
	// (repository.LogInTx), not by this service afterwards. A call here would be a SECOND row per
	// deletion. The durable single-row property is asserted where it can actually be observed, in
	// TestDeleteUserCascade_Integration_AuditEntryIsWrittenExactlyOncePerDeletion.
	mockAudit.AssertNotCalled(t, "Log", ctx, "user.deleted", "user", user.ID)
}

// TestExecuteDeletion_BumpsCrossUserAffectedOwnersCache is GDPR round-6 finding P1's regression
// test at the service layer: DeleteUserCascade's dangling cross-user custom-category-override
// clear rewrites an OTHER, surviving user's transaction row — purgeCacheForDeletedUser only ever
// bumps the DELETED user's own scopes (see its own doc comment), so without a separate bump keyed
// on the SURVIVING owner, that owner's warm analytics cache keeps serving the erased user's
// now-stale custom category name until the cache namespace's TTL elapses.
//
// This drives ExecuteDeletion with a REAL cache.Generations backed by a fake Redis (never a mock
// of the bump call itself — matching account_service_cache_invalidation_test.go's own reasoning:
// a regression that silently no-ops the bump must fail this test, not just skip an assertion
// against a spy) and asserts the OTHER user's ScopeUser generation token actually changed.
//
// #1531 G1: also wires a HouseholdReader that resolves the affected owner to a household, and
// asserts that household's ScopeHousehold generation is bumped too — bumpCacheForAffectedOwners'
// household branch (deletion_service.go's household-bump loop) previously had zero test coverage;
// only the ScopeUser path above ever ran.
func TestExecuteDeletion_BumpsCrossUserAffectedOwnersCache(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader := &mockHouseholdReaderForDeletion{}

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(hhReader)

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}
	otherUserID := "other-user-456"
	otherHousehold := &domain.Household{ID: "hh-affected-owner", OwnerUserID: otherUserID}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.DeleteUserCascadeAffectedOwnerIDs = []string{otherUserID}
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	hhReader.On("GetHouseholdForUser", mock.Anything, otherUserID).Return(otherHousehold, false, true, nil)
	// The deleted user's OWN household lookup (purgeCacheForDeletedUser's pre-cascade capture,
	// unrelated to G1's affected-owner bump) — user-123 has no household of its own in this test.
	hhReader.On("GetHouseholdForUser", mock.Anything, user.ID).Return(nil, false, false, nil)

	fake := redisclient.NewFake("cache")
	gens := cache.NewGenerations(fake)
	service.SetCache(nil, gens, nil)

	preGen, err := gens.Get(ctx, cache.ScopeUser, otherUserID)
	require.NoError(t, err)
	preHHGen, err := gens.Get(ctx, cache.ScopeHousehold, otherHousehold.ID)
	require.NoError(t, err)

	err = service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	require.NoError(t, err)

	postGen, err := gens.Get(ctx, cache.ScopeUser, otherUserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"P1: deleting user-123 rewrote otherUserID's transaction row (the cross-user dangling "+
			"override clear), so otherUserID's OWN cache generation must be bumped — otherwise their "+
			"warm analytics cache keeps serving user-123's just-erased custom category name")

	postHHGen, err := gens.Get(ctx, cache.ScopeHousehold, otherHousehold.ID)
	require.NoError(t, err)
	assert.NotEqual(t, preHHGen, postHHGen,
		"G1: otherUserID belongs to a household, so bumpCacheForAffectedOwners must also bump "+
			"that household's ScopeHousehold generation — a household-scoped analytics read "+
			"(cache/consumer.go's Scope selection prefers household over user) would otherwise keep "+
			"serving user-123's just-erased custom category name under a DIFFERENT key than the "+
			"one just bumped above")
}

// TestExecuteDeletion_DeletesClerkUser verifies Clerk deletion
func TestExecuteDeletion_DeletesClerkUser(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err)
	mockClerk.AssertExpectations(t)
	mockClerk.AssertCalled(t, "DeleteUser", ctx, user.ClerkUserID)
}

// TestExecuteDeletion_HandlesAlreadyDeleted tests idempotency
func TestExecuteDeletion_HandlesAlreadyDeleted(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   true, // Already deleted
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err, "Should not error on already deleted user")
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_HandlesClerkError tests graceful Clerk error handling
func TestExecuteDeletion_HandlesClerkError(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(errors.New("Clerk API error"))

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	// Should still succeed even if Clerk fails (our data is deleted)
	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID, mock.Anything)
	// Issue #1507: written inside the cascade's transaction, not here — see
	// TestExecuteDeletion_CascadesAllData's note.
	mockAudit.AssertNotCalled(t, "Log", ctx, "user.deleted", "user", user.ID)
}

// MockPlaidItemCleanupService is a mock for PlaidItemCleanupService
type MockPlaidItemCleanupService struct {
	mock.Mock

	// LastRemoveAllRecheck is the eligibility callback the last RemoveAllUserItems call was handed.
	// Captured in a FIELD rather than passed to m.Called so that every pre-existing two-argument
	// `.On("RemoveAllUserItems", ctx, userID)` expectation in this package keeps matching — a func
	// value is also not comparable, which makes it a poor testify argument regardless.
	// TestExecuteDeletion_HandsPlaidCleanupALiveEligibilityRecheck reads it; nil here is the defect
	// this parameter exists to prevent.
	LastRemoveAllRecheck func(ctx context.Context) (bool, error)
	// RemoveAllRecheckWasNil distinguishes "captured a nil callback" from "was never called".
	RemoveAllRecheckWasNil bool
}

func (m *MockPlaidItemCleanupService) RemoveAllUserItems(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	m.LastRemoveAllRecheck = recheck
	m.RemoveAllRecheckWasNil = recheck == nil
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// RemoveItem's use-time eligibility recheck (Codex pass-5 Medium 2) is excluded from the expectation
// arguments: DeletionService never calls RemoveItem at all, and the recheck's behaviour is asserted
// where it lives (plaid_item_cleanup_removeitem_recheck_test.go and the worker's own tests).
func (m *MockPlaidItemCleanupService) RemoveItem(ctx context.Context, linkedItemID string, _ func(ctx context.Context) (bool, error)) error {
	args := m.Called(ctx, linkedItemID)
	return args.Error(0)
}

// DisconnectAllUserItemsIfEligible is never called through this mock — DeletionService only calls
// RemoveAllUserItems on its plaidCleanupSvc field. Bare no-op stubbed only to satisfy the
// PlaidItemCleanupService interface. (Its nil-recheck twin, DisconnectAllUserItems, was deleted
// outright by Issue #1500 and no longer needs stubbing here.)
func (m *MockPlaidItemCleanupService) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	return nil
}

// SetWorkClaimsRepo (Issue #1413 H1) is never called through this mock — DeletionService only
// calls RemoveAllUserItems on its plaidCleanupSvc field; the claims repo
// is wired directly onto the concrete PlaidItemCleanupService in main.go, not via DeletionService.
// Bare no-op (no m.Called) to avoid an unregistered-expectation panic; stubbed only to satisfy
// the widened PlaidItemCleanupService interface.
func (m *MockPlaidItemCleanupService) SetWorkClaimsRepo(repo repository.WorkClaimsRepository) {}
func (m *MockPlaidItemCleanupService) SetAccountRepo(repo AccountRepoForCleanup)              {}

// TestExecuteDeletion_CallsPlaidCleanupBeforeDBDelete verifies that
// ExecuteDeletion calls PlaidCleanupService.RemoveAllUserItems before DeleteUserCascade
// Issue #668: Plaid Item Management for Cost Optimization
func TestExecuteDeletion_CallsPlaidCleanupBeforeDBDelete(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	// Set up call order tracking
	callOrder := []string{}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "RemoveAllUserItems")
	})
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "DeleteUserCascade")
	})
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err)
	mockPlaidCleanup.AssertCalled(t, "RemoveAllUserItems", ctx, user.ID)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID, mock.Anything)

	// Verify Plaid cleanup happens BEFORE database deletion
	assert.Equal(t, []string{"RemoveAllUserItems", "DeleteUserCascade"}, callOrder,
		"PlaidCleanup should be called before DeleteUserCascade")
}

// TestExecuteDeletion_PlaidCleanupFails_AbortsDeletion is a corrected test (Issue #1413 Critical
// finding C2, round 3 remediation).
//
// This test previously asserted the OPPOSITE — named "...SucceedsEvenIfPlaidCleanupFails" — that
// ExecuteDeletion continues to cascade-delete even when Plaid cleanup fails, framed as "graceful
// degradation." That was the C2 bug, not a documented rule: DeleteUserCascade clears the user's
// Plaid references, so proceeding after a failed cleanup destroys the only local record of the
// Plaid item that still needs removing (and is still billing) — nothing would ever retry it.
// Aborting and releasing the claim is the deliberate fix: the next run retries the whole
// sequence while the references are still present to retry against.
func TestExecuteDeletion_PlaidCleanupFails_AbortsDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	plaidErr := errors.New("plaid API unavailable")
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(plaidErr)
	// DeleteUserCascade, Clerk deletion, and the audit log are deliberately left unstubbed —
	// an unstubbed testify mock call panics, a stronger proof than AssertNotCalled alone that
	// the abort actually happens before any of them.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err, "a failed Plaid cleanup must abort the deletion, not be swallowed")
	assert.ErrorIs(t, err, plaidErr)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestSetPlaidCleanupService verifies that SetPlaidCleanupService wires up the service
// Issue #668: Setter for late binding of Plaid cleanup service
func TestSetPlaidCleanupService(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	// Create service with nil plaidCleanupSvc
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	// Set the cleanup service via setter
	service.SetPlaidCleanupService(mockPlaidCleanup)

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err)
	// Verify the cleanup service was called (proving the setter worked)
	mockPlaidCleanup.AssertCalled(t, "RemoveAllUserItems", ctx, user.ID)
}

// MockPaddleDeletionCleaner is a mock for PaddleDeletionCleaner.
// Only 2 methods needed for DeletionService — narrowed interface. Issue #1006.
type MockPaddleDeletionCleaner struct {
	mock.Mock
}

func (m *MockPaddleDeletionCleaner) CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error {
	return m.Called(ctx, subscriptionID).Error(0)
}

func (m *MockPaddleDeletionCleaner) ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error {
	return m.Called(ctx, customerID, anonymizedEmail).Error(0)
}

// buildUserWithPaddle creates a test user with Paddle IDs set.
// Returns user, subscriptionID, customerID for convenience.
func buildUserWithPaddle(subscriptionStatus string) (*domain.User, string, string) {
	paddleSubID := "sub_01abc123"
	paddleCustID := "ctm_01abc123"
	return &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   subscriptionStatus,
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
	}, paddleSubID, paddleCustID
}

const expectedAnonymizedEmail = "deleted_user-123@deleted.local"

// TestExecuteDeletion_ActiveSubscription_CancelsImmediately verifies that an active
// subscription is cancelled immediately and customer is archived. Issue #1006.
func TestExecuteDeletion_ActiveSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("active")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, paddleSubID)
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_TrialSubscription_CancelsImmediately verifies trial subscriptions
// are cancelled immediately. Issue #1006.
func TestExecuteDeletion_TrialSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("trial")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, paddleSubID)
}

// TestExecuteDeletion_TrialingSubscription_CancelsImmediately verifies Paddle-trialing
// subscriptions are cancelled immediately. Issue #1006.
func TestExecuteDeletion_TrialingSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("trialing")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, paddleSubID)
}

// TestExecuteDeletion_PastDueSubscription_CancelsImmediately verifies past_due subscriptions
// are cancelled immediately (billing is still active). Issue #1006.
func TestExecuteDeletion_PastDueSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("past_due")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, paddleSubID)
}

// TestExecuteDeletion_CancelledSubscription_DoesNotCancelAgain verifies that a cancelled
// subscription does NOT trigger another cancel call, but customer is still archived. Issue #1006.
func TestExecuteDeletion_CancelledSubscription_DoesNotCancelAgain(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, _, paddleCustID := buildUserWithPaddle("cancelled")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	// CancelSubscriptionImmediately must NOT be called
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_InactiveSubscription_DoesNotCancel verifies that an inactive
// subscription (no subscription ID) does not trigger a cancel call. Issue #1006.
func TestExecuteDeletion_InactiveSubscription_DoesNotCancel(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	// inactive: no subscription ID (only customer ID)
	paddleCustID := "ctm_01abc123"
	user := &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   "inactive",
		PaddleSubscriptionID: nil,
		PaddleCustomerID:     &paddleCustID,
	}
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_CompedUser_RawCancelled_DoesNotCancelRealSubscription is the
// #1413 PinnedRaw pin test for cleanupPaddleResources (deletion_service.go:625-626).
//
// That gate deliberately reads the RAW user.SubscriptionStatus, not ComputeEffectiveStatus():
// cancelling a Paddle subscription must be driven by what Paddle itself reports the
// subscription's real state to be, not by a comp override that only relabels access. If this
// switched to effective status, a comped user's forced "active" would make the gate true even
// though the underlying Paddle subscription is genuinely cancelled — CancelSubscriptionImmediately
// would fire against a subscription that (from Paddle's point of view) is already gone.
//
// The two pre-#1413 tests above (CancelledSubscription_DoesNotCancelAgain,
// InactiveSubscription_DoesNotCancel) do NOT pin this: they set no ManualTierOverride, so raw and
// effective status are identical for them and both survive a switch to effective silently. The
// compedUser helper (used by the Part C guard tests) doesn't pin it either: it leaves
// PaddleSubscriptionID and SubscriptionStatus unset, so execution never reaches this branch at
// all. This test closes that gap: a LIVE override (so effective would read "active") combined
// with a raw "cancelled" status AND a real PaddleSubscriptionID (so the branch is actually
// entered), plus DeletionRequestedAt set so the user's own GDPR request clears the Part C guard
// at ExecuteDeletion's top and execution reaches cleanupPaddleResources at all.
func TestExecuteDeletion_CompedUser_RawCancelled_DoesNotCancelRealSubscription(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	tier := "premium"
	paddleSubID := "sub_01abc123"
	paddleCustID := "ctm_01abc123"
	requestedAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   "cancelled", // raw: the real Paddle subscription is already cancelled
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
		ManualTierOverride:   &tier, // live comp -> effective status would read "active"
		ManualTierEndsAt:     nil,
		DeletionRequestedAt:  &requestedAt, // user's own GDPR request clears the Part C guard
	}
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	// CancelSubscriptionImmediately must NOT be called: raw status "cancelled" is not an active
	// subscription and not past_due, regardless of the comp override's effective "active" status.
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindUserRequested)
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_NoPaddleCustomerID_SkipsArchive verifies that when no customer ID
// is set, ArchiveAndAnonymizeCustomer is not called. Issue #1006.
func TestExecuteDeletion_NoPaddleCustomerID_SkipsArchive(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	// No customer ID set — user never completed checkout
	user := &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   "trial",
		PaddleSubscriptionID: nil,
		PaddleCustomerID:     nil,
	}
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_PaddleCancelFails_AbortsDeletion is a corrected test (Issue #1413 Critical
// finding C2, round 3 remediation).
//
// This test previously asserted the OPPOSITE — named "...DeletionContinues" — that a Paddle
// cancel failure is "non-critical" and the archive step and cascade still run. That was the C2
// bug: DeleteUserCascade clears paddle_subscription_id/paddle_customer_id, so proceeding after a
// failed cancel destroys the only local record of the subscription that still needs cancelling —
// nothing would ever retry it, and the user keeps getting billed. Aborting on the FIRST cleanup
// failure (before ever attempting the archive step) and releasing the claim is the fix.
func TestExecuteDeletion_PaddleCancelFails_AbortsDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, _ := buildUserWithPaddle("active")

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	// Issue #1413 H1: cleanupPaddleResources nests ActionPaddleCancelSubscription inside the flow
	// claim above, immediately around the Paddle call this test fails.
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPaddleCancelSideEffectToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionPaddleCancelSubscription, testPaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	paddleErr := errors.New("Paddle API timeout")
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(paddleErr)
	// ArchiveAndAnonymizeCustomer, DeleteUserCascade, and Clerk deletion are deliberately left
	// unstubbed — an unstubbed testify mock call panics, proving the abort happens before any
	// of them, not just before the cascade.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err, "a failed Paddle cancel must abort the deletion, not be swallowed")
	assert.ErrorIs(t, err, paddleErr)
	mockPaddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_PaddleSideEffectClaimBusy_Aborts_ReleasesFlowClaim is the H1 gating test at
// the CALLER boundary: when the OPERATION-keyed ActionPaddleCancelSubscription claim is held by
// another flow (e.g. a concurrent past_due grace-cancel), cleanupPaddleResources must return
// ErrSideEffectClaimBusy WITHOUT ever calling the Paddle collaborator, and ExecuteDeletion must
// still release its OWN ActionAccountDeletion flow claim and propagate the error — exactly like
// any other cleanup failure. This is the "side-effect claim lost while flow claim held →
// collaborator not called, flow claim released" case, proven at this caller rather than only
// inside plaidItemCleanupService.
func TestExecuteDeletion_PaddleSideEffectClaimBusy_Aborts_ReleasesFlowClaim(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, _, _ := buildUserWithPaddle("active")

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	// Another flow already holds the side-effect claim — Claim reports claimed=false.
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return("", false, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	// CancelSubscriptionImmediately, ArchiveAndAnonymizeCustomer, DeleteUserCascade, and Clerk
	// deletion are deliberately left unstubbed — an unstubbed testify mock call panics, proving
	// the Paddle collaborator is never reached at all.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err, "a busy side-effect claim must abort this deletion attempt so it retries")
	assert.ErrorIs(t, err, ErrSideEffectClaimBusy)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
	// The side-effect claim was never won, so there is nothing to release for it.
	mockClaims.AssertNotCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionPaddleCancelSubscription, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_PaddleArchiveFails_AbortsDeletion is the archive-step sibling of the
// cancel-failure test above — same C2 correction, same rationale.
func TestExecuteDeletion_PaddleArchiveFails_AbortsDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("active")

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	// Issue #1413 H1: cleanupPaddleResources nests ActionPaddleCancelSubscription inside the flow
	// claim above, immediately around the (here, successful) Paddle cancel call.
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionPaddleCancelSubscription, paddleCancelSideEffectLease).
		Return(testPaddleCancelSideEffectToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionPaddleCancelSubscription, testPaddleCancelSideEffectToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	archiveErr := errors.New("Paddle API timeout")
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(archiveErr)
	// DeleteUserCascade and Clerk deletion are deliberately left unstubbed.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err, "a failed Paddle archive must abort the deletion, not be swallowed")
	assert.ErrorIs(t, err, archiveErr)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestSetPaddleCleanupService_WiresCorrectly verifies that SetPaddleCleanupService wires
// the mock so it's invoked during ExecuteDeletion. Issue #1006.
func TestSetPaddleCleanupService_WiresCorrectly(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	// Create with nil paddleCleanupSvc, wire via setter
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("active")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	// Both mock methods called proves setter worked
	mockPaddle.AssertExpectations(t)
}

// TestExecuteDeletion_PlaidRunsBeforePaddleAndDB verifies the execution order:
// Plaid cleanup → Paddle cleanup → DB cascade. Issue #1006 established the ordering; the ordering
// itself was REVERSED afterwards, and this test's name and expectation were reversed with it.
//
// ⚠ IT USED TO ASSERT THE OPPOSITE (named "...PaddleRunsBeforePlaidAndDB"), and that expectation was
// wrong in a way the test could not see, because it only ever exercised the all-succeeds path. The
// two external legs are NOT equally reversible: a Plaid disconnect costs the user a re-link, whereas
// ArchiveAndAnonymizeCustomer destroys PII at Paddle and cannot be undone by us or by Paddle. With
// Paddle first, EVERY abort after it — a Plaid outage, the routine ErrSideEffectClaimBusy deferral,
// or (the case that prompted this) the user cancelling their deletion mid-flight — left a user whose
// deletion did not happen with their subscription already cancelled and their Paddle customer
// already anonymised. Reversing the order means the widest exposure window carries the cheapest
// possible loss, and the irreversible call is preceded by the freshest authorisation check.
//
// See deletion_service_authorisation_revoked_test.go for the tests that make the ordering
// load-bearing rather than incidental.
func TestExecuteDeletion_PlaidRunsBeforePaddleAndDB(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)
	mockPlaid := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaid, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("active")
	callOrder := []string{}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "CancelSubscription")
	})
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "ArchiveCustomer")
	})
	mockPlaid.On("RemoveAllUserItems", ctx, user.ID).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "RemoveAllUserItems")
	})
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "DeleteUserCascade")
	})
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	assert.NoError(t, err)
	assert.Equal(t,
		[]string{"RemoveAllUserItems", "CancelSubscription", "ArchiveCustomer", "DeleteUserCascade"},
		callOrder,
		"Execution order must be: Plaid remove, Paddle cancel, Paddle archive, DB cascade — "+
			"recoverable external work first, the irreversible anonymisation last",
	)
}

// mockAnalyticsForDeletion is a minimal phclient.Analytics stub for setter tests.
type mockAnalyticsForDeletion struct{}

func (m *mockAnalyticsForDeletion) Capture(distinctID, event string, properties map[string]interface{}) {
}
func (m *mockAnalyticsForDeletion) Identify(distinctID string, properties map[string]interface{}) {}
func (m *mockAnalyticsForDeletion) Close()                                                        {}

var _ phclient.Analytics = (*mockAnalyticsForDeletion)(nil)

// ─── Household owner-deletion guard (#1367 — Phase 3) ────────────────────────

type mockHouseholdReaderForDeletion struct{ mock.Mock }

func (m *mockHouseholdReaderForDeletion) GetHouseholdForUser(ctx context.Context, userID string) (*domain.Household, bool, bool, error) {
	args := m.Called(ctx, userID)
	var h *domain.Household
	if args.Get(0) != nil {
		h = args.Get(0).(*domain.Household)
	}
	return h, args.Bool(1), args.Bool(2), args.Error(3)
}
func (m *mockHouseholdReaderForDeletion) CountActiveMembers(ctx context.Context, householdID string) (int, error) {
	args := m.Called(ctx, householdID)
	return args.Int(0), args.Error(1)
}

// LockHouseholdForUpdate is not exercised by the non-transactional-path tests below
// (only the SetDB-wired transactional path uses a real, tx-scoped HouseholdRepository
// for locking); stubbed to satisfy the expanded HouseholdReader interface.
func (m *mockHouseholdReaderForDeletion) LockHouseholdForUpdate(ctx context.Context, householdID string) (string, error) {
	args := m.Called(ctx, householdID)
	return args.String(0), args.Error(1)
}

type mockHouseholdInviteRevokerForDeletion struct{ mock.Mock }

func (m *mockHouseholdInviteRevokerForDeletion) RevokeAllPendingForHousehold(ctx context.Context, householdID string) error {
	args := m.Called(ctx, householdID)
	return args.Error(0)
}

func TestRequestDeletion_OwnerWithActiveMembers_Blocked(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader := &mockHouseholdReaderForDeletion{}

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(hhReader)

	hhReader.On("GetHouseholdForUser", ctx, "owner-1").Return(&domain.Household{ID: "hh-1"}, true, true, nil)
	hhReader.On("CountActiveMembers", ctx, "hh-1").Return(2, nil)

	_, _, err := service.RequestDeletion(ctx, "owner-1", "owner@example.com")
	require.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)
	mockRepo.AssertNotCalled(t, "MarkForDeletion", mock.Anything, mock.Anything, mock.Anything)
}

func TestRequestDeletion_OwnerWithNoOtherMembers_ProceedsAndRevokesInvites(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader := &mockHouseholdReaderForDeletion{}
	inviteRevoker := &mockHouseholdInviteRevokerForDeletion{}

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(hhReader)
	service.SetHouseholdInviteRevoker(inviteRevoker)

	hhReader.On("GetHouseholdForUser", ctx, "owner-1").Return(&domain.Household{ID: "hh-1"}, true, true, nil)
	hhReader.On("CountActiveMembers", ctx, "hh-1").Return(1, nil) // owner only
	inviteRevoker.On("RevokeAllPendingForHousehold", ctx, "hh-1").Return(nil)
	mockRepo.On("MarkForDeletion", ctx, "owner-1", mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, "owner@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", "owner-1").Return(nil)

	_, _, err := service.RequestDeletion(ctx, "owner-1", "owner@example.com")
	require.NoError(t, err)
	inviteRevoker.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// ─── Transactional owner-guard (#1367 — Codex review, High finding 4) ────────
//
// When SetDB is wired, the household lock/check/revoke-invites/mark-for-deletion
// sequence runs inside ONE transaction (via fresh tx-scoped repositories, not the
// pool-bound HouseholdReader/HouseholdInviteRevoker), so an in-flight AcceptInvite
// cannot commit a new member between this guard's check and MarkForDeletion.

func TestRequestDeletion_Transactional_OwnerWithActiveMembers_RollsBack(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(&mockHouseholdReaderForDeletion{}) // gating flag only; unused once SetDB is wired
	service.SetDB(pool)

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectRollback()

	_, _, err = service.RequestDeletion(ctx, "owner-1", "owner@example.com")
	require.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)
	require.NoError(t, pool.ExpectationsWereMet())
	mockRepo.AssertNotCalled(t, "MarkForDeletion", mock.Anything, mock.Anything, mock.Anything)
}

func TestRequestDeletion_Transactional_OwnerWithNoOtherMembers_LocksRevokesAndMarks(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(&mockHouseholdReaderForDeletion{})
	service.SetDB(pool)

	mockEmail.On("SendDeletionConfirmation", ctx, "owner@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", "owner-1").Return(nil)

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectExec("UPDATE household_invites SET status = 'revoked'").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE users SET deletion_requested_at").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	_, _, err = service.RequestDeletion(ctx, "owner-1", "owner@example.com")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestRequestDeletion_Transactional_NonOwner_JustMarksForDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(&mockHouseholdReaderForDeletion{})
	service.SetDB(pool)

	mockEmail.On("SendDeletionConfirmation", ctx, "member@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", "member-1").Return(nil)

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectExec("UPDATE users SET deletion_requested_at").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	_, _, err = service.RequestDeletion(ctx, "member-1", "member@example.com")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestRequestDeletion_NonOwner_ProceedsNormally(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader := &mockHouseholdReaderForDeletion{}

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(hhReader)

	hhReader.On("GetHouseholdForUser", ctx, "member-1").Return(&domain.Household{ID: "hh-1"}, false, true, nil)
	mockRepo.On("MarkForDeletion", ctx, "member-1", mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, "member@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", "member-1").Return(nil)

	_, _, err := service.RequestDeletion(ctx, "member-1", "member@example.com")
	require.NoError(t, err)
	hhReader.AssertNotCalled(t, "CountActiveMembers", mock.Anything, mock.Anything)
}

func TestDeletionService_SetAnalytics_DoesNotPanic(t *testing.T) {
	repo := new(MockProfileRepositoryForDeletion)
	email := new(MockEmailService)
	clerkSvc := new(MockClerkService)
	auditLog := new(MockAuditLogger)
	plaid := new(MockPlaidItemCleanupService)
	svc := NewDeletionService(repo, email, clerkSvc, auditLog, plaid, 30, "test-secret-key")
	ds := svc.(*deletionService)
	require.NotPanics(t, func() {
		svc.SetAnalytics(&mockAnalyticsForDeletion{})
	}, "SetAnalytics with a valid analytics must not panic")
	assert.NotNil(t, ds.analytics, "analytics field should be set after SetAnalytics(non-nil)")
	require.NotPanics(t, func() {
		svc.SetAnalytics(nil)
	}, "SetAnalytics with nil must not panic")
	assert.Nil(t, ds.analytics, "analytics field should be nil after SetAnalytics(nil)")
}

// --- Issue #1413 Part C: the grant-vs-delete race guard ---
//
// ExecuteDeletion is shared by two callers: the retention cleanup worker (auto-deletion of
// long-inactive accounts — DeletionRequestedAt is always nil there; all three
// GetUsersNeedingRetention30dNotice/GetUsersNeedingRetention7dNotice/GetUsersWithExpiredRetention
// queries filter on deletion_requested_at IS NULL) and the scheduled deletion worker (a
// user-requested GDPR deletion — DeletionRequestedAt is always set there, via
// GetUsersScheduledForDeletion). A ratified product decision: a user's own GDPR deletion
// request beats an admin comp. The guard below is therefore conditional on
// DeletionRequestedAt == nil, never a blanket comp check — a blanket guard would illegally
// strand a comped user who has asked to be deleted.

// compedUser builds a domain.User with an admin manual tier override, for Part C guard tests.
func compedUser(endsAt *time.Time, deletionRequestedAt *time.Time) *domain.User {
	tier := "premium"
	return &domain.User{
		ID:                  "user-123",
		ClerkUserID:         "user_123",
		Email:               "test@example.com",
		IsDeleted:           false,
		ManualTierOverride:  &tier,
		ManualTierEndsAt:    endsAt,
		DeletionRequestedAt: deletionRequestedAt,
	}
}

// TestExecuteDeletion_ActiveComp_NoDeletionRequest_SkipsDeletion verifies the retention-worker
// path: a comped user with no user-initiated deletion request must not be auto-deleted, and
// the skip must be returned as a nil error (a legitimate outcome, not a failure).
func TestExecuteDeletion_ActiveComp_NoDeletionRequest_SkipsDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := compedUser(nil, nil) // no expiry, never requested deletion

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err, "a skipped auto-deletion is a legitimate outcome, not an error")
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_ActiveComp_WithDeletionRequest_ProceedsAnyway verifies the GDPR case:
// a user's own deletion request must proceed even while an admin comp is active. A blanket
// comp guard would illegally strand this user (Part C requirement).
func TestExecuteDeletion_ActiveComp_WithDeletionRequest_ProceedsAnyway(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	requestedAt := time.Now().Add(-24 * time.Hour)
	user := compedUser(nil, &requestedAt) // active comp, but user asked to be deleted

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindUserRequested)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID, mock.Anything)
	mockPlaidCleanup.AssertCalled(t, "RemoveAllUserItems", ctx, user.ID)
}

// TestExecuteDeletion_ExpiredComp_NoDeletionRequest_BeltAndBracesDoesNotShortCircuit is a
// corrected test (Issue #1413 Critical finding C1, round 3 remediation).
//
// Previously named "...ProceedsAnyway" and asserting that deletion completes for an expired comp
// with no deletion request. That was accurate under the OLD claimForDeletionPredicate
// (noActiveManualOverridePredicate — "still live right now"), which was itself the C1 bug: a
// lapsed-but-uncleared comp must delay deletion until the daily expiry job clears the column,
// exactly like the retention queries already required (see
// profile_repository.go's noManualOverrideAtAllPredicate). This test's actual, narrower purpose
// was to prove the cheap belt-and-braces short-circuit
// (HasActiveManualOverride() && DeletionRequestedAt == nil) does NOT itself fire for an EXPIRED
// override — i.e. execution reaches the real claim/predicate machinery rather than being wrongly
// (short-circuit-)skipped there too, since HasActiveManualOverride is false for an expired
// override. It does not, and should not, assert what the real predicate then decides — that is
// proven against a live database in
// TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_NotClaimed
// (profile_repository_claim_for_deletion_integration_test.go). ClaimForDeletion is mocked here to
// return the real, corrected answer (false) rather than the stale `true` this test used to
// assert.
func TestExecuteDeletion_ExpiredComp_NoDeletionRequest_BeltAndBracesDoesNotShortCircuit(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	expired := time.Now().Add(-1 * time.Hour)
	user := compedUser(&expired, nil)

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	// Reflects the corrected predicate's real answer for a lapsed-but-uncleared override with
	// no deletion request (see the doc comment above for the live-DB proof this mirrors).
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(false, nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err, "a denied claim is a legitimate skip, not an error")
	mockRepo.AssertCalled(t, "ClaimForDeletion", ctx, user.ID) // proves the belt-and-braces short-circuit did NOT fire here
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_NonComped_ProceedsNormally verifies the new guard does not affect
// ordinary (never-comped) deletions.
func TestExecuteDeletion_NonComped_ProceedsNormally(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID, mock.Anything)
}

// TestExecuteDeletion_ThreadsClaimKindToRepository proves ExecuteDeletion passes the CALLER's
// declared authorisation through to ClaimForDeletion verbatim, rather than deriving one from the
// user's row (Issue #1440, T0). Deriving it is exactly what cannot work: a user who cancelled has a
// row indistinguishable from a retention candidate, so an inferred kind would authorise the
// deletion the split exists to refuse.
//
// The user object here has no deletion request and no comp, so it is a plausible retention
// candidate on its face — which is precisely why the user-requested case must still forward
// user-requested and not "correct" it.
func TestExecuteDeletion_ThreadsClaimKindToRepository(t *testing.T) {
	for _, kind := range []repository.DeletionClaimKind{
		repository.DeletionKindUserRequested,
		repository.DeletionKindRetention,
	} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := context.Background()
			mockRepo := new(MockProfileRepositoryForDeletion)
			mockClerk := new(MockClerkService)
			mockAudit := new(MockAuditLogger)

			service := NewDeletionService(mockRepo, new(MockEmailService), mockClerk, mockAudit, nil, 30, "test-secret-key")
			service.SetWorkClaimsRepo(newAllowingClaimsRepo())

			user := &domain.User{ID: "user-123", ClerkUserID: "user_123", Email: "test@example.com"}
			mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
			mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
			mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
			mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

			require.NoError(t, service.ExecuteDeletion(ctx, user.ID, kind))
			assert.Equal(t, kind, mockRepo.LastClaimKind,
				"the caller's declared claim kind must reach ClaimForDeletion unmodified")
		})
	}
}

// --- Issue #1413 Critical finding C2 (closed for real, round 3): TWO claim gates now guard
// ExecuteDeletion's destructive work ---
//
// The Part C guard above (HasActiveManualOverride() && DeletionRequestedAt == nil) is a
// read-only check against the GetUserByID snapshot. It cannot, by itself, protect the
// irreversible external cleanup (Paddle cancel, Plaid disconnect) and cascade delete from a comp
// granted in the window between that snapshot and this call. It is kept as a cheap
// belt-and-braces short-circuit (unchanged, see above). The tests below use a user object that
// looks eligible from the snapshot (no comp visible) so that short-circuit does NOT fire, forcing
// execution through BOTH real gates:
//
//  1. claimsRepo.Claim (work_claims, EXCLUSIVITY) — replaces the old self-assigning
//     ClaimForDeletion-as-a-guard. A lost claim here means another worker holds the lease, or the
//     action was already completed; the predicate is never even re-checked.
//  2. profileRepo.ClaimForDeletion (CORRECTNESS re-check) — still a conditional UPDATE evaluated
//     against committed state at the moment of the claim, now gated behind gate 1 rather than
//     being the only gate.
//
// Both gates release the work claim on denial/error so a later run doesn't wait out the full
// lease, per the #1413 design doc's required call shape.

// TestExecuteDeletion_WorkClaimNotGranted_SkipsWithoutPredicateOrCleanup proves gate 1 alone
// blocks all destructive work AND the predicate re-check: even though the snapshot looked
// eligible, a lost work claim (simulating another worker holding the lease, or this action
// already completed) must prevent ClaimForDeletion, Paddle cancel, Paddle archive, Plaid
// disconnect, cascade delete, and Clerk deletion. Every one of those collaborators is
// deliberately left unstubbed: an unstubbed testify mock call panics, a stronger proof than
// AssertNotCalled alone.
func TestExecuteDeletion_WorkClaimNotGranted_SkipsWithoutPredicateOrCleanup(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return("", false, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	// ClaimForDeletion and everything below is deliberately left unstubbed on purpose.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err, "a claim not won is a legitimate skip, not an error")
	mockRepo.AssertNotCalled(t, "ClaimForDeletion", mock.Anything, mock.Anything)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockPaddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	mockAudit.AssertNotCalled(t, "Log", ctx, "user.deleted", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_WorkClaimErrors_PropagatesAndSkipsPredicateAndCleanup proves a DB error
// during the work-claims Claim call itself (as opposed to a lost claim) propagates as an error
// and still must not reach the predicate re-check or any destructive work.
func TestExecuteDeletion_WorkClaimErrors_PropagatesAndSkipsPredicateAndCleanup(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return("", false, errors.New("db unavailable"))
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	// Nothing below is stubbed on purpose.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.Error(t, err)
	mockRepo.AssertNotCalled(t, "ClaimForDeletion", mock.Anything, mock.Anything)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_PredicateDenied_ReleasesClaimAndSkipsCleanup proves gate 2 (the
// ClaimForDeletion re-check) still gates correctness even after gate 1 is won — e.g. a comp
// granted between the work claim and the predicate re-check — and that the work claim is
// released so a later run doesn't wait out the full lease.
func TestExecuteDeletion_PredicateDenied_ReleasesClaimAndSkipsCleanup(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(false, nil)
	// Everything below is deliberately left unstubbed on purpose.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err, "a denied predicate re-check is a legitimate skip, not an error")
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	mockAudit.AssertNotCalled(t, "Log", ctx, "user.deleted", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_PredicateErrors_ReleasesClaimAndPropagates proves a DB error during the
// predicate re-check itself (as opposed to a denial) propagates as an error, releases the work
// claim, and still must not run any destructive work.
func TestExecuteDeletion_PredicateErrors_ReleasesClaimAndPropagates(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(false, errors.New("db unavailable"))
	// Nothing below is stubbed on purpose.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.Error(t, err)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_BothClaimsGranted_ProceedsWithFullCleanupAndCompletes is the
// negative-direction proof: an over-tight claim/predicate that silently stopped ALL deletions
// would pass every "nothing bad happened" test above, so this asserts the destructive path
// genuinely still runs once BOTH gates are won, and that Complete is called with the exact
// token Claim returned — closing the loop the fencing token exists to protect.
func TestExecuteDeletion_BothClaimsGranted_ProceedsWithFullCleanupAndCompletes(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err)
	mockPlaidCleanup.AssertCalled(t, "RemoveAllUserItems", ctx, user.ID)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID, mock.Anything)
	mockClerk.AssertCalled(t, "DeleteUser", ctx, user.ClerkUserID)
	mockClaims.AssertCalled(t, "Complete", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken)
}

// TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed proves that a lost lease at
// Complete time (another worker reclaimed this tuple while we were mid-flight — the exact
// scenario the fencing token exists to detect) does not silently pass as success: ExecuteDeletion
// must still return nil (the deletion genuinely happened; there's nothing left to retry), but the
// caller-visible contract is that this case is surfaced loudly rather than swallowed. The
// production code logs via slog.Default() (not an injectable field), so slog.Default() is
// swapped for a buffer-backed logger for the duration of the call and restored via defer
// afterward — this makes the ERROR-level log record from ExecuteDeletion's Complete handling
// directly assertable rather than merely inspected in the source.
func TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken).
		Return(false, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(true, nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prevLogger)

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	assert.NoError(t, err, "the cascade already ran; a lost lease at Complete time must not be reported as a deletion failure")
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID, mock.Anything)
	mockClaims.AssertCalled(t, "Complete", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "level=ERROR", "a lost lease at Complete time must be logged at ERROR, not swallowed")
	assert.Contains(t, logOutput, user.ID, "the ERROR log must carry the identifying user_id")
}

// --- Issue #1413 finding H4: every path that wins the work claim must release it unless
// it durably completes. Before this fix, DeleteUserCascade failing returned WITHOUT
// releasing — leaking the claim for the full 15-minute lease and blocking a retry the whole
// time — and there was no deferred release at all, so a panic anywhere in the destructive
// section leaked the claim too. The three tests below are the regression coverage for that.

// TestExecuteDeletion_CascadeFails_ReleasesClaim is the direct regression test for the H4
// leak: DeleteUserCascade failing must release the work claim so the very next run can
// retry immediately, rather than waiting out the full lease untouched.
func TestExecuteDeletion_CascadeFails_ReleasesClaim(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	cascadeErr := errors.New("db write failed")
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(false, cascadeErr)
	// Clerk deletion and audit log deliberately left unstubbed — an unstubbed testify mock
	// call panics, proving the failure return happens before either of them.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err)
	assert.ErrorIs(t, err, cascadeErr)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_CascadeHouseholdOwnerGuard_ReleasesClaimAndPropagatesError is Issue #1440's
// Critical-finding regression at the SERVICE layer (Codex review): DeleteUserCascade now returns
// (false, domain.ErrHouseholdHasActiveMembers) — NOT a normal (false, nil) skip like a lapsed comp
// or an un-cleaned Plaid item — when the target is a household owner whose deletion would CASCADE
// away another real user's data (households.owner_user_id -> household_members /
// subscription_entitlements.payer_user_id). Unlike TestExecuteDeletion_CascadeAbortedByGuard above,
// this condition cannot self-heal on the next scheduled run, so it MUST be treated as a genuine
// error: ExecuteDeletion must propagate it (so every caller's existing Error-level logging fires,
// and the River-job callers retry and eventually surface it in the dead-letter queue) while still
// releasing the work claim so a future run — after a human resolves the household — is not stuck
// waiting out the rest of the 15-minute lease. The real DB proof that DeleteUserCascade itself
// returns this error in exactly this situation lives in
// profile_repository_delete_user_cascade_integration_test.go
// (TestDeleteUserCascade_Integration_HouseholdOwnerWithActiveNonOwnerMember_NotDeleted and its
// orphaned-entitlement sibling); this test is the service-layer proof that ExecuteDeletion responds
// to that signal correctly — as an error, not a silent skip.
func TestExecuteDeletion_CascadeHouseholdOwnerGuard_ReleasesClaimAndPropagatesError(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(false, domain.ErrHouseholdHasActiveMembers)
	// Clerk deletion and audit log deliberately left unstubbed — an unstubbed testify mock call
	// panics, proving the error return happens before either of them (the user was NOT deleted).

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err, "a household-owner guard trip must be a genuine error, not a silent (nil) skip — it cannot self-heal without a human resolving the household")
	assert.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_CascadeAbortedByGuard_ReleasesClaimNotCompleteAndReturnsNoError proves
// Issue #1413 round-4 Critical finding C1/C2's abort path is wired correctly at the service
// layer: DeleteUserCascade reporting (false, nil) — its own internal guard found the user no
// longer eligible (comp granted, deletion request cancelled) or an un-cleaned Plaid item still
// exists — is a NORMAL, EXPECTED outcome, not an error. ExecuteDeletion must:
//  1. return nil (no error) — the daily job must not log/alert this as a failure,
//  2. release the work claim (never Complete it) so the next scheduled run re-evaluates the
//     user from scratch instead of waiting out the rest of the 15-minute lease untouched, and
//  3. never reach Clerk deletion or the audit log — the user was NOT actually deleted.
//
// The real race-closing proof that DeleteUserCascade itself returns (false, nil) in these exact
// situations lives in profile_repository_delete_user_cascade_integration_test.go (real DB); this
// test is the service-layer proof that ExecuteDeletion responds to that signal correctly.
func TestExecuteDeletion_CascadeAbortedByGuard_ReleasesClaimNotCompleteAndReturnsNoError(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	// DeleteUserCascade's own guard found the precondition lost (or an un-cleaned Plaid item) —
	// (false, nil), NOT an error.
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Return(false, nil)
	// Clerk deletion and audit log deliberately left unstubbed — an unstubbed testify mock call
	// panics, proving ExecuteDeletion returns before reaching either: the user was not deleted.

	err := service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)

	require.NoError(t, err, "an aborted cascade is a normal, expected outcome — not a job failure")
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_ReleasesClaim_WithIndependentContext_EvenIfCallerContextCancelled
// proves the other half of H4: Release must use a bounded, independent context
// (context.WithoutCancel), not the caller's ctx — which may already be cancelled by the
// time a failure is being handled (e.g. a River job context cancelled by the same
// deadline/shutdown that is causing the failure). Releasing with an already-cancelled
// context would silently fail to release at all, leaking the claim for the rest of the
// lease. This test cancels the caller's context BEFORE calling ExecuteDeletion and asserts
// the context actually passed to Release is NOT the cancelled one.
func TestExecuteDeletion_ReleasesClaim_WithIndependentContext_EvenIfCallerContextCancelled(t *testing.T) {
	baseCtx, cancel := context.WithCancel(context.Background())
	cancel() // simulate the caller's context already being cancelled/timed out

	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	// Captured synchronously INSIDE the mocked Release call, not after: the production
	// closure's own `defer cancel()` fires as soon as Release returns, so a context
	// reference read only after ExecuteDeletion returns would already be cancelled by that
	// (correct, unrelated) cleanup — Err() must be snapshotted at call time to actually test
	// what the caller received, not what a later cleanup did to the same object.
	var releaseCalled bool
	var releaseCtxErrAtCallTime error
	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", baseCtx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Run(func(args mock.Arguments) {
			releaseCalled = true
			releaseCtxErrAtCallTime = args.Get(0).(context.Context).Err()
		}).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", baseCtx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", baseCtx, user.ID).Return(true, nil)
	cascadeErr := errors.New("db write failed")
	mockRepo.On("DeleteUserCascade", baseCtx, user.ID, mock.Anything).Return(false, cascadeErr)

	err := service.ExecuteDeletion(baseCtx, user.ID, repository.DeletionKindRetention)

	require.Error(t, err)
	require.True(t, releaseCalled, "Release must still be called even with a cancelled caller context")
	assert.Error(t, baseCtx.Err(), "sanity check: baseCtx really is cancelled")
	assert.NoError(t, releaseCtxErrAtCallTime, "Release's context must be independent of the cancelled caller context, or the release itself would silently fail")
}

// TestExecuteDeletion_PanicDuringCascade_ReleasesClaimThenRePanics proves the deferred
// release also covers a panic (e.g. an unexpected nil pointer deep in a repository call)
// during the destructive section — previously nothing released the claim in that case
// either, leaking it for the full lease on top of the process crashing. The panic must
// still propagate (this is not panic-swallowing), but the claim must be released first.
func TestExecuteDeletion_PanicDuringCascade_ReleasesClaimThenRePanics(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID, mock.Anything).Run(func(args mock.Arguments) {
		panic("simulated unexpected panic during cascade")
	}).Return(false, nil)

	require.Panics(t, func() {
		_ = service.ExecuteDeletion(ctx, user.ID, repository.DeletionKindRetention)
	}, "the panic must still propagate — this proves the claim is released first, not that panics are swallowed")

	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// mockDeletionCachePurger implements cache.Purger for deletion-service tests. It records the
// context each PurgeUser call was made with, so tests can assert on Err()/Deadline() at call time
// (a reference captured only after the call returns would already reflect the *test's* later
// cleanup, not what the production code actually passed in).
type mockDeletionCachePurger struct {
	mock.Mock
}

func (m *mockDeletionCachePurger) PurgeUser(ctx context.Context, userID, clerkUserID string) error {
	args := m.Called(ctx, userID, clerkUserID)
	return args.Error(0)
}

var _ cache.Purger = (*mockDeletionCachePurger)(nil)

// mockDeletionJobQueue implements queue.JobQueue for deletion-service tests (separate name from
// mockProfileJobQueue/MockJobQueue elsewhere in this package to avoid collisions).
type mockDeletionJobQueue struct {
	mock.Mock
}

func (m *mockDeletionJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, job)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *mockDeletionJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, tx, job)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *mockDeletionJobQueue) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, jobs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

func (m *mockDeletionJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, tx, jobs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

var _ queue.JobQueue = (*mockDeletionJobQueue)(nil)

// TestPurgeCacheForDeletedUser_PurgeAndRetryEnqueue_SurviveCallerCancellation is Issue #1531 M1's
// regression test. purgeCacheForDeletedUser is called from a River job's Work method AFTER
// DeleteUserCascade has already committed — River cancels a job's context on shutdown, retry, or
// timeout, and that cancellation can land in the narrow window between the commit and this
// method's own cache cleanup. Before the fix, PurgeUser(ctx, ...) and (on failure)
// enqueueCachePurgeRetry(ctx, ...) -> jobQueue.Enqueue(ctx, ...) both ran on that same raw,
// cancelled ctx: PurgeUser would fail with context.Canceled, and the "durable remedy" retry
// enqueue would ALSO fail with context.Canceled (same cancelled context) — logged and dropped,
// with no tombstone set and nothing scheduled. The deletion is marked complete while the user's
// cached PII survives, terminal rather than TTL-bounded.
//
// This test cancels the caller's context BEFORE calling purgeCacheForDeletedUser and asserts the
// contexts actually passed to PurgeUser and jobQueue.Enqueue are NOT the cancelled one (mirroring
// TestExecuteDeletion_ReleasesClaim_WithIndependentContext_EvenIfCallerContextCancelled above for
// H4's Release call).
func TestPurgeCacheForDeletedUser_PurgeAndRetryEnqueue_SurviveCallerCancellation(t *testing.T) {
	baseCtx, cancel := context.WithCancel(context.Background())
	cancel() // simulate a River job context cancelled (shutdown/retry/timeout) before cleanup runs

	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	clerkUserID := "user_123"
	householdID := "" // no household bump in play here — isolate the purge/retry pair

	purger := new(mockDeletionCachePurger)
	jq := new(mockDeletionJobQueue)

	var purgeCtxErrAtCallTime, enqueueCtxErrAtCallTime error
	var purgeCalled, enqueueCalled bool

	purger.On("PurgeUser", mock.Anything, userID, clerkUserID).
		Run(func(args mock.Arguments) {
			purgeCalled = true
			purgeCtxErrAtCallTime = args.Get(0).(context.Context).Err()
		}).
		Return(errors.New("redis: connection refused")) // force the durable-retry path
	jq.On("Enqueue", mock.Anything, mock.AnythingOfType("services.cachePurgeRetryJobArgs")).
		Run(func(args mock.Arguments) {
			enqueueCalled = true
			enqueueCtxErrAtCallTime = args.Get(0).(context.Context).Err()
		}).
		Return(&queue.JobResult{}, nil)

	service.SetCache(purger, cache.NewGenerations(nil), jq)

	deletionSvc, ok := service.(*deletionService)
	require.True(t, ok, "test relies on calling the unexported method directly")
	deletionSvc.purgeCacheForDeletedUser(baseCtx, userID, clerkUserID, householdID)

	require.True(t, purgeCalled, "PurgeUser must still be called even with a cancelled caller context")
	require.True(t, enqueueCalled, "the durable retry must still be enqueued even with a cancelled caller context")
	assert.Error(t, baseCtx.Err(), "sanity check: baseCtx really is cancelled")
	assert.NoError(t, purgeCtxErrAtCallTime,
		"PurgeUser's context must be independent of the cancelled caller context, or a River "+
			"cancellation landing here would fail the purge for no reason")
	assert.NoError(t, enqueueCtxErrAtCallTime,
		"the durable retry enqueue's context must be independent of the cancelled caller context — "+
			"otherwise the 'durable remedy' the PurgeUser failure comment relies on is on the SAME "+
			"cancelled context and fails identically, dropping the cache purge with no tombstone set "+
			"and nothing scheduled (Issue #1531 M1)")
}

// TestPurgeCacheForDeletedUser_RetryStillEnqueued_WhenPurgeExhaustsSharedDeadline is Issue #1531
// GA-1's regression test. M1 (above) detached purgeCtx/enqueue's ctx from the CALLER's
// cancellation, but PurgeUser and enqueueCachePurgeRetry still shared ONE 5s
// (bumpCacheTimeout) budget: purgeCtx itself. Redis SCAN...MATCH iterates the entire keyspace
// (COUNT is only a hint), so on a populated cache PurgeUser can legitimately consume the whole
// budget. Before the fix, enqueueCachePurgeRetry(purgeCtx, ...) then runs on an
// ALREADY-EXPIRED context — the durable retry is dropped exactly like the cancellation case M1
// closed, just via deadline exhaustion instead of cancellation.
//
// This test makes PurgeUser's mock sleep past bumpCacheTimeout (5s) before returning an error,
// so that by the time enqueueCachePurgeRetry runs, the shared purgeCtx (pre-fix) is already
// past its deadline. It asserts the retry is enqueued with a context that is NOT already
// expired.
func TestPurgeCacheForDeletedUser_RetryStillEnqueued_WhenPurgeExhaustsSharedDeadline(t *testing.T) {
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	clerkUserID := "user_123"
	householdID := ""

	purger := new(mockDeletionCachePurger)
	jq := new(mockDeletionJobQueue)

	var enqueueCtxErrAtCallTime error
	var enqueueCalled bool

	purger.On("PurgeUser", mock.Anything, userID, clerkUserID).
		Run(func(args mock.Arguments) {
			// Simulate a SCAN over a populated keyspace consuming the entire 5s budget: sleep
			// past bumpCacheTimeout so purgeCtx's deadline has already elapsed by the time this
			// returns.
			time.Sleep(5*time.Second + 200*time.Millisecond)
		}).
		Return(errors.New("redis: connection refused"))
	jq.On("Enqueue", mock.Anything, mock.AnythingOfType("services.cachePurgeRetryJobArgs")).
		Run(func(args mock.Arguments) {
			enqueueCalled = true
			enqueueCtxErrAtCallTime = args.Get(0).(context.Context).Err()
		}).
		Return(&queue.JobResult{}, nil)

	service.SetCache(purger, cache.NewGenerations(nil), jq)

	deletionSvc, ok := service.(*deletionService)
	require.True(t, ok, "test relies on calling the unexported method directly")
	deletionSvc.purgeCacheForDeletedUser(context.Background(), userID, clerkUserID, householdID)

	require.True(t, enqueueCalled, "the durable retry must still be enqueued even when PurgeUser consumes the whole shared budget")
	assert.NoError(t, enqueueCtxErrAtCallTime,
		"the durable retry enqueue must run on its OWN fresh deadline, not the purge's already-"+
			"exhausted one — otherwise River's Enqueue fails with context.DeadlineExceeded and the "+
			"retry is dropped exactly like the cancellation case M1 closed, just via deadline "+
			"exhaustion instead of cancellation (Issue #1531 GA-1)")
}
