package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

type RevenueCatProvider interface {
	EnsureCustomer(context.Context, string) error
	FetchEntitlements(context.Context, string) ([]rc.Entitlement, error)
	FetchSubscriptions(context.Context, string) ([]rc.Subscription, error)
	Grant(context.Context, string, string, *time.Time) error
	Revoke(context.Context, string, string) error
}
type RevenueCatProfileReader interface {
	GetUserByID(context.Context, string) (*domain.User, error)
}
type RevenueCatService struct {
	analytics phclient.ProductAnalytics
	repo      repository.RevenueCatRepository
	provider  RevenueCatProvider
	profiles  RevenueCatProfileReader
}

func (s *RevenueCatService) SetAnalytics(analytics phclient.ProductAnalytics) {
	s.analytics = analytics
}

type revenueCatOperationPhase int

const (
	revenueCatOperationQueued revenueCatOperationPhase = iota
	revenueCatOperationApplied
	revenueCatOperationFailed
)

func (s *RevenueCatService) captureOperation(ctx context.Context, op domain.RevenueCatOperation, phase revenueCatOperationPhase) {
	if s.analytics == nil {
		return
	}
	// These events describe the target user's access, even when an admin acts. The Gateway
	// resolves that target's canonical identity and reads current DB consent before delivery;
	// operations retain the audit trail regardless.
	switch phase {
	case revenueCatOperationQueued:
		s.analytics.CaptureEvent(ctx, op.UserID, phclient.RevenueCatOperationQueuedPayload{
			Class:       phclient.RevenueCatOperationQueuedClass(op.Class),
			Entitlement: phclient.RevenueCatOperationQueuedEntitlement(op.Key),
			Revoke:      op.Revoke,
		})
	case revenueCatOperationApplied:
		s.analytics.CaptureEvent(ctx, op.UserID, phclient.RevenueCatOperationAppliedPayload{
			Class:       phclient.RevenueCatOperationAppliedClass(op.Class),
			Entitlement: phclient.RevenueCatOperationAppliedEntitlement(op.Key),
			Revoke:      op.Revoke,
		})
	case revenueCatOperationFailed:
		s.analytics.CaptureEvent(ctx, op.UserID, phclient.RevenueCatOperationFailedPayload{
			Class:       phclient.RevenueCatOperationFailedClass(op.Class),
			Entitlement: phclient.RevenueCatOperationFailedEntitlement(op.Key),
			Revoke:      op.Revoke,
		})
	}
}

// captureAdminOverride emits the target-user half of the admin tier-change stream. It is called
// only from RequestGrant, which requires a non-empty actor and is reached only from the admin
// handler, so it is inherently scoped to admin-initiated operations — a system-initiated
// operation (bootstrap, comp expiry, webhook-driven lapse) never reaches this call site. The
// acting admin is deliberately absent from the properties: the unsuffixed frontend twin carries
// the acting admin as its subject, and the two must not be combined.
func (s *RevenueCatService) captureAdminOverride(ctx context.Context, op domain.RevenueCatOperation, tier string) {
	if s.analytics == nil {
		return
	}
	if op.Revoke {
		s.analytics.CaptureEvent(ctx, op.UserID, phclient.AdminSubscriptionOverrideRemovedServerPayload{
			Tier:      phclient.AdminSubscriptionOverrideRemovedServerTier(tier),
			HadExpiry: op.ExpiresAt != nil,
		})
		return
	}
	s.analytics.CaptureEvent(ctx, op.UserID, phclient.AdminSubscriptionOverrideAppliedServerPayload{
		Tier:      phclient.AdminSubscriptionOverrideAppliedServerTier(tier),
		HadExpiry: op.ExpiresAt != nil,
	})
}

func NewRevenueCatService(repo repository.RevenueCatRepository, provider RevenueCatProvider, profiles RevenueCatProfileReader) *RevenueCatService {
	return &RevenueCatService{repo: repo, provider: provider, profiles: profiles}
}

func (s *RevenueCatService) ResolveOwnAccess(ctx context.Context, id string) (domain.RevenueCatAccess, error) {
	return s.ResolveOwnAccessAt(ctx, id, time.Now())
}
func (s *RevenueCatService) ResolveOwnAccessAt(ctx context.Context, id string, now time.Time) (domain.RevenueCatAccess, error) {
	state, err := s.repo.Read(ctx, id)
	// A pool wait or slow database read must not extend a cached grant's deadline.
	if readAt := time.Now(); readAt.After(now) {
		now = readAt
	}
	if err == nil {
		if access, e := state.ResolveAccess(now); e == nil {
			return access, nil
		}
	}
	// The locked recheck coalesces concurrent cache misses across all server instances.
	state, err = s.refresh(ctx, id, false, now)
	if err != nil {
		return domain.RevenueCatAccess{}, fmt.Errorf("%w: refresh failed", domain.ErrRevenueCatUnresolved)
	}
	// Billing I/O and the transaction commit can cross an entitlement expiry after
	// access was observed. Authorization uses the return-time instant, never the
	// earlier observation timestamp. A caller's later as-of instant still wins.
	if returnedAt := time.Now(); returnedAt.After(now) {
		now = returnedAt
	}
	return state.ResolveAccess(now)
}
func (s *RevenueCatService) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, id string) (string, error) {
	a, err := s.ResolveOwnAccess(ctx, id)
	return a.Status, err
}
func (s *RevenueCatService) Refresh(ctx context.Context, id string) (domain.RevenueCatState, error) {
	return s.refresh(ctx, id, true, time.Now())
}
func (s *RevenueCatService) refresh(ctx context.Context, id string, force bool, requested time.Time, erasure ...bool) (domain.RevenueCatState, error) {
	var state domain.RevenueCatState
	var priorGrants []domain.RevenueCatGrant
	var remoteErr error
	refreshed := false
	// ProfileRepository uses the pool. Never acquire a second connection while
	// holding the customer transaction: even a one-connection pool must progress.
	customer, user, identityErr := s.customerID(ctx, id)
	priorStatus := ""
	if user != nil {
		priorStatus = user.SubscriptionStatus
	}
	err := s.repo.WithCustomer(ctx, id, func(session repository.RevenueCatSession) error {
		var err error
		state, err = session.Read(ctx)
		if err != nil {
			return err
		}
		// refreshLocked replaces state.Grants with the fresh projection; keep the one it overwrites
		// so the lifecycle classification below can name the grant that ended.
		priorGrants = append([]domain.RevenueCatGrant(nil), state.Grants...)
		checkAt := requested
		if state.AccessUpdatedAt.After(checkAt) {
			checkAt = state.AccessUpdatedAt
		}
		if _, valid := state.ResolveAccess(checkAt); valid == nil && (!force || !state.AccessUpdatedAt.Before(requested)) {
			return nil
		}
		if identityErr != nil {
			remoteErr = identityErr
			state.AccessError = revenueCatErrorCode(identityErr)
			return session.Save(ctx, state)
		}
		if state.AccessUpdatedAt.IsZero() && state.ErasureStartedAt == nil && len(erasure) == 0 {
			if remoteErr = s.provider.EnsureCustomer(ctx, customer); remoteErr != nil {
				state.AccessError = revenueCatErrorCode(remoteErr)
				return session.Save(ctx, state)
			}
		}
		refreshed = true
		remoteErr = s.refreshLocked(ctx, session, customer, &state, erasure...)
		return nil // persist sanitized failure and prior confirmed snapshot, even on provider error.
	})
	if err != nil {
		return state, err
	}
	if refreshed {
		s.captureAccessTransition(ctx, id, priorStatus, priorGrants, state, len(erasure) > 0)
	}
	return state, remoteErr
}
func (s *RevenueCatService) customerID(ctx context.Context, id string) (string, *domain.User, error) {
	if s.provider == nil {
		return "", nil, errors.New("RevenueCat provider is not configured")
	}
	user, err := s.profiles.GetUserByID(ctx, id)
	if err != nil {
		return "", nil, err
	}
	if user == nil || user.ClerkUserID == "" || user.IsDeleted {
		return "", nil, errors.New("RevenueCat customer identity unavailable")
	}
	return user.ClerkUserID, user, nil
}

// captureAccessTransition emits the access-loss and retention-armed pair when a refresh flips a
// previously granting user (active/trial/trialing) to an inactive projection, plus the
// grant-lifecycle event naming the grant that ended. It is a pure side effect — resolution and
// persistence are identical whether or not analytics is configured. The prior status comes from the
// persisted users row and the prior grants from the projection this refresh overwrote, so a repeated
// inactive refresh does not re-fire. A user being erased never emits: the authority query would drop
// it anyway, and attempting it would recreate the PostHog person the erasure outbox just deleted.
func (s *RevenueCatService) captureAccessTransition(ctx context.Context, id, priorStatus string, priorGrants []domain.RevenueCatGrant, state domain.RevenueCatState, erasureRequested bool) {
	if s.analytics == nil || erasureRequested || state.ErasureStartedAt != nil {
		return
	}
	if priorStatus == "" {
		return
	}
	access, err := state.ResolveAccess(time.Now())
	if err != nil {
		return
	}
	// The trial boundary is checked before the inactive early-return because a trial that converted
	// to a paid grant resolves active, not inactive.
	if priorStatus == "trial" || priorStatus == "trialing" {
		if access.Status != "trial" {
			s.analytics.CaptureEvent(ctx, id, phclient.RevenueCatTrialEndedPayload{
				Entitlement: phclient.RevenueCatTrialEndedEntitlementTrialPremium,
				Converted:   access.Status == "active",
			})
		}
	}
	if priorStatus == "inactive" {
		return
	}
	if access.Status != "inactive" {
		return
	}
	// An admin revoke is applied by ProcessOperation, never by a refresh, so a refresh-driven
	// inactive projection is the comp-expiry / paid-lapse path the admin queue does not describe.
	s.captureGrantEnded(ctx, id, priorGrants)
	s.analytics.CaptureEvent(ctx, id, phclient.RevenueCatAccessInactivePayload{
		PendingOperations: int64(state.PendingOperations),
	})
	if priorStatus == "active" || priorStatus == "trial" || priorStatus == "trialing" {
		s.analytics.CaptureEvent(ctx, id, phclient.RevenueCatRetentionArmedPayload{
			DeferredForPendingOperations: state.PendingOperations > 0,
		})
	}
}

// captureGrantEnded names the grant class that backed the projection this refresh overwrote, when
// that projection was comp- or paid-backed. Expiry is ignored on purpose: the grant that just
// expired IS the transition being classified, and ResolveAccess would refuse the projection for it.
func (s *RevenueCatService) captureGrantEnded(ctx context.Context, id string, priorGrants []domain.RevenueCatGrant) {
	source, key := bestRevenueCatGrant(priorGrants)
	switch source {
	case "comp":
		s.analytics.CaptureEvent(ctx, id, phclient.RevenueCatCompExpiredPayload{
			Entitlement: phclient.RevenueCatCompExpiredEntitlement(key),
		})
	case "paid":
		s.analytics.CaptureEvent(ctx, id, phclient.RevenueCatPaidGrantLapsedPayload{
			Entitlement: phclient.RevenueCatPaidGrantLapsedEntitlement(key),
		})
	}
}

// bestRevenueCatGrant returns the highest-ranked grant's source and key, mirroring
// domain.RevenueCatState.ResolveAccess's ranking (trial weakest, then comp/paid by tier) while
// ignoring expiry, for the reason above.
func bestRevenueCatGrant(grants []domain.RevenueCatGrant) (source, key string) {
	best := 0
	for _, g := range grants {
		_, src, rank, ok := domain.RevenueCatKey(g.Key)
		if !ok {
			continue
		}
		score := rank * 10
		switch src {
		case "trial":
			score = 1
		case "paid":
			score++
		}
		if score > best {
			best, source, key = score, src, g.Key
		}
	}
	return source, key
}

// captureTrialStarted emits the trial lifecycle boundary when a trial-class operation is applied. A
// trial grant is system-initiated (registration or bootstrap import) and never carries an actor, so
// the admin queue's revenuecat_operation_* stream does not cover this boundary. Nil-safe, and never
// emitted for a failed or revoked operation — access did not change.
func (s *RevenueCatService) captureTrialStarted(ctx context.Context, op domain.RevenueCatOperation, applied bool) {
	if s.analytics == nil || !applied || op.Revoke || op.Class != "trial" {
		return
	}
	s.analytics.CaptureEvent(ctx, op.UserID, phclient.RevenueCatTrialStartedPayload{
		Entitlement: phclient.RevenueCatTrialStartedEntitlementTrialPremium,
	})
}
func (s *RevenueCatService) refreshLocked(ctx context.Context, session repository.RevenueCatSession, customer string, state *domain.RevenueCatState, erasure ...bool) error {
	grants, err := s.provider.FetchEntitlements(ctx, customer)
	if (state.ErasureStartedAt != nil || len(erasure) > 0) && revenueCatCustomerMissing(err) {
		grants, err = nil, nil
	}
	if err == nil {
		applyRevenueCatGrants(state, grants, time.Now())
	}
	if err != nil {
		state.AccessError = revenueCatErrorCode(err)
		if saveErr := session.Save(ctx, *state); saveErr != nil {
			return saveErr
		}
		return err
	}
	billing, billingErr := s.provider.FetchSubscriptions(ctx, customer)
	if len(erasure) > 0 && revenueCatCustomerMissing(billingErr) {
		if state.BillingUpdatedAt != nil {
			state.PreserveBilling = true
			return session.Save(ctx, *state)
		}
		billing, billingErr = nil, nil
	}
	// Keep the last encrypted billing identities after remote erasure so Paddle
	// cleanup remains retryable until the local cascade commits.
	if state.ErasureStartedAt != nil {
		state.PreserveBilling = true
		return session.Save(ctx, *state)
	}
	if billingErr != nil {
		state.BillingError = revenueCatErrorCode(billingErr)
	} else {
		state.PreserveBilling = false
		state.Billing = make([]domain.RevenueCatBilling, 0, len(billing))
		for _, b := range billing {
			state.Billing = append(state.Billing, domain.RevenueCatBilling{ID: b.ID, ProductID: b.ProductID, StoreSubscriptionIdentifier: b.StoreSubscriptionIdentifier, Status: b.Status, Store: b.Store, Environment: b.Environment, AutoRenewalStatus: b.AutoRenewalStatus, CurrentPeriodEndsAt: b.CurrentPeriodEndsAt, EndsAt: b.EndsAt, ManagementURL: b.ManagementURL})
		}
		now := time.Now()
		state.BillingUpdatedAt = &now
		state.BillingError = ""
	}
	return session.Save(ctx, *state)
}
// applyRevenueCatGrants projects a FRESH provider read, which is authoritative about what access
// the customer has NOW. A grant that can no longer resolve — already lapsed, an unrecognised key,
// or a duplicate — is DROPPED rather than failing the whole refresh. Failing here aborted the
// refresh BEFORE Save, so the inactive projection (retention arming + plaid_cleanup_pending_at)
// was never persisted, and a persistently-returned lapsed grant left the user indefinitely active.
// An empty projection is meaningful: it resolves inactive.
//
// The cached-state rule is untouched — domain.RevenueCatState.ResolveAccess still refuses an expired
// CACHED grant, which forces this refresh; this function only decides the projection that refresh
// writes. Write decisions (applyOperation/revenueCatDesired) still reject unrecognised keys before
// they can ever reach here.
func applyRevenueCatGrants(state *domain.RevenueCatState, grants []rc.Entitlement, now time.Time) {
	projected := make([]domain.RevenueCatGrant, 0, len(grants))
	seen := map[string]bool{}
	for _, g := range grants {
		if _, _, _, ok := domain.RevenueCatKey(g.LookupKey); !ok {
			continue
		}
		if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
			continue
		}
		if seen[g.LookupKey] {
			continue
		}
		seen[g.LookupKey] = true
		projected = append(projected, domain.RevenueCatGrant{Key: g.LookupKey, ExpiresAt: g.ExpiresAt})
	}
	state.Grants = projected
	state.AccessUpdatedAt = now
	state.AccessError = ""
}
func revenueCatErrorCode(err error) string {
	var httpErr *rc.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 429 {
			return "rate_limited"
		}
		if httpErr.StatusCode == 401 || httpErr.StatusCode == 403 {
			return "provider_authentication_failed"
		}
	}
	if errors.Is(err, domain.ErrRevenueCatUnresolved) {
		return "invalid_entitlements"
	}
	return "provider_unavailable"
}

func (s *RevenueCatService) GrantTrialTx(ctx context.Context, tx pgx.Tx, id string) error {
	return s.repo.CreateTrialTx(ctx, tx, id, time.Now().UTC().Truncate(time.Millisecond))
}

// GrantTrial supports the existing registration interface. Registration should use
// GrantTrialTx when it owns the transaction so eligibility cannot be lost on a crash.
func (s *RevenueCatService) GrantTrial(ctx context.Context, id string) error {
	return errors.New("RevenueCat trial issuance requires registration transaction")
}
func (s *RevenueCatService) RequestGrant(ctx context.Context, id, class, tier string, end *time.Time, revoke bool, actorID, reason string) (domain.RevenueCatOperation, error) {
	reason = strings.TrimSpace(reason)
	op := domain.RevenueCatOperation{UserID: id, Class: class, Key: class + "_" + tier, ExpiresAt: end, Revoke: revoke, Reason: reason, CreatedAt: time.Now().UTC()}
	_, source, _, valid := domain.RevenueCatKey(op.Key)
	if !valid || source == "paid" || id == "" || actorID == "" || reason == "" || len(reason) > 1000 {
		return op, errors.New("valid grant class, tier, actor and reason are required")
	}
	if !revoke && ((end != nil && !end.After(op.CreatedAt)) || (class == "trial" && end == nil)) {
		return op, errors.New("grant expiry must be in the future; trials must be finite")
	}
	if end != nil {
		rounded := end.UTC().Truncate(time.Millisecond)
		op.ExpiresAt = &rounded
	}
	op.ActorID = &actorID
	err := s.repo.RequestOperation(ctx, &op)
	if err == nil {
		s.captureOperation(ctx, op, revenueCatOperationQueued)
		s.captureAdminOverride(ctx, op, tier)
	}
	return op, err
}

// ProcessOperation holds the same customer lock as refresh. Remote changes cannot
// be rolled back with PostgreSQL: the committed intent is retried by reading the
// provider first, which recognizes successful requests with ambiguous responses.
func (s *RevenueCatService) ProcessOperation(ctx context.Context, userID, operationID string) error {
	var workErr error
	var attempted *domain.RevenueCatOperation
	customer, _, identityErr := s.customerID(ctx, userID)
	err := s.repo.WithCustomer(ctx, userID, func(session repository.RevenueCatSession) error {
		op, err := session.Operation(ctx, operationID)
		if err != nil {
			return err
		}
		if op.Stage != "pending" {
			return nil
		}
		attempted = &op
		if identityErr != nil {
			workErr = identityErr
			return session.FinishOperation(ctx, op.ID, "pending", revenueCatErrorCode(identityErr))
		}
		state, err := session.Read(ctx)
		if err != nil {
			return err
		}
		if state.ErasureStartedAt != nil {
			return domain.ErrRevenueCatErasureStarted
		}
		workErr = s.provider.EnsureCustomer(ctx, customer)
		if workErr == nil {
			workErr = s.applyOperation(ctx, session, customer, op, &state)
		}
		stage, message := "applied", ""
		if workErr != nil {
			stage = "pending"
			message = revenueCatErrorCode(workErr)
		}
		return session.FinishOperation(ctx, op.ID, stage, message)
	})
	if err != nil {
		return err
	}
	if attempted != nil {
		phase := revenueCatOperationApplied
		if workErr != nil {
			phase = revenueCatOperationFailed
		}
		s.captureOperation(ctx, *attempted, phase)
		s.captureTrialStarted(ctx, *attempted, workErr == nil)
	}
	return workErr
}
func (s *RevenueCatService) applyOperation(ctx context.Context, session repository.RevenueCatSession, customer string, op domain.RevenueCatOperation, state *domain.RevenueCatState) error {
	grants, err := s.provider.FetchEntitlements(ctx, customer)
	if err != nil {
		return err
	}
	if _, source, _, ok := domain.RevenueCatKey(op.Key); !ok || source != op.Class || (source != "trial" && source != "comp") {
		return errors.New("invalid grant operation")
	}
	// Superseded records were excluded under the lock. Remove only this promotional
	// class, leaving paid, the other grant class, and household coverage untouched.
	for _, g := range grants {
		_, source, _, ok := domain.RevenueCatKey(g.LookupKey)
		if !ok {
			return domain.ErrRevenueCatUnresolved
		}
		if source != op.Class {
			continue
		}
		if !op.Revoke && g.LookupKey == op.Key && sameRevenueCatExpiry(g.ExpiresAt, op.ExpiresAt) {
			continue
		}
		if err = s.provider.Revoke(ctx, customer, g.LookupKey); err != nil {
			// A timeout may have applied. The final readback below is authoritative.
			break
		}
	}
	observed, readErr := s.provider.FetchEntitlements(ctx, customer)
	if readErr != nil {
		return readErr
	}
	if !revenueCatDesired(observed, op) && !op.Revoke {
		// Do not grant while a conflicting old promotion still exists: retries must
		// first complete revoke to avoid RC's close-expiry duplicate suppression.
		for _, g := range observed {
			_, source, _, _ := domain.RevenueCatKey(g.LookupKey)
			if source == op.Class {
				return errors.New("promotional revocation not yet confirmed")
			}
		}
		if op.ExpiresAt == nil || op.ExpiresAt.After(time.Now()) {
			err = s.provider.Grant(ctx, customer, op.Key, op.ExpiresAt)
		}
	}
	final, readErr := s.provider.FetchEntitlements(ctx, customer)
	if readErr != nil {
		return readErr
	}
	if !revenueCatDesired(final, op) {
		if err != nil {
			return err
		}
		return errors.New("grant change not yet confirmed")
	}
	applyRevenueCatGrants(state, final, time.Now())
	return session.Save(ctx, *state)
}
func sameRevenueCatExpiry(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.UnixMilli() == b.UnixMilli()
}
func revenueCatDesired(grants []rc.Entitlement, op domain.RevenueCatOperation) bool {
	matched := false
	for _, g := range grants {
		_, source, _, ok := domain.RevenueCatKey(g.LookupKey)
		if !ok {
			return false
		}
		if source != op.Class {
			continue
		}
		if op.Revoke || g.LookupKey != op.Key || !sameRevenueCatExpiry(g.ExpiresAt, op.ExpiresAt) {
			return false
		}
		matched = true
	}
	// An intent whose fixed expiry has passed is successfully absent, not renewed.
	return matched || op.Revoke || (op.ExpiresAt != nil && !op.ExpiresAt.After(time.Now()))
}
