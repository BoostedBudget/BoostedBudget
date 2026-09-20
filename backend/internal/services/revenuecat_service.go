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
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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
			observability.RecordRevenueCatResolution(observability.RevenueCatResolutionResolved)
			return access, nil
		}
	}
	// The locked recheck coalesces concurrent cache misses across all server instances.
	state, err = s.refresh(ctx, id, false, now)
	if err != nil {
		observability.RecordRevenueCatResolution(observability.RevenueCatResolutionError)
		return domain.RevenueCatAccess{}, fmt.Errorf("%w: refresh failed", domain.ErrRevenueCatUnresolved)
	}
	// Billing I/O and the transaction commit can cross an entitlement expiry after
	// access was observed. Authorization uses the return-time instant, never the
	// earlier observation timestamp. A caller's later as-of instant still wins.
	if returnedAt := time.Now(); returnedAt.After(now) {
		now = returnedAt
	}
	access, err := state.ResolveAccess(now)
	if err != nil {
		observability.RecordRevenueCatResolution(observability.RevenueCatResolutionUnresolved)
		return access, err
	}
	observability.RecordRevenueCatResolution(observability.RevenueCatResolutionResolved)
	return access, nil
}
func (s *RevenueCatService) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, id string) (string, error) {
	a, err := s.ResolveOwnAccess(ctx, id)
	return a.Status, err
}
func (s *RevenueCatService) Refresh(ctx context.Context, id string) (domain.RevenueCatState, error) {
	return s.refresh(ctx, id, true, time.Now())
}
func (s *RevenueCatService) refresh(ctx context.Context, id string, force bool, requested time.Time, erasure ...bool) (domain.RevenueCatState, error) {
	start := time.Now()
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
		observability.ObserveRevenueCatRefresh(observability.RevenueCatRefreshError, time.Since(start).Seconds())
		return state, err
	}
	if refreshed {
		s.captureAccessTransition(ctx, id, priorStatus, priorGrants, state, len(erasure) > 0)
	}
	outcome := observability.RevenueCatRefreshNotDue
	if refreshed {
		outcome = observability.RevenueCatRefreshSuccess
		if remoteErr != nil {
			outcome = observability.RevenueCatRefreshError
		}
	}
	observability.ObserveRevenueCatRefresh(outcome, time.Since(start).Seconds())
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
	if erasureRequested || state.ErasureStartedAt != nil || priorStatus == "" {
		return
	}
	access, err := state.ResolveAccess(time.Now())
	if err != nil {
		return
	}
	// The lifecycle FLOW counters are recorded here, before the analytics gate, because they
	// describe the system's grant lifecycle rather than the user's consent-gated product stream.
	// The predicates are exactly the once-per-transition ones the product events below use, so a
	// repeated inactive refresh cannot re-count.
	//
	// The trial boundary is checked before the inactive early-return because a trial that converted
	// to a paid grant resolves active, not inactive.
	trialEnded := (priorStatus == "trial" || priorStatus == "trialing") && access.Status != "trial"
	if trialEnded && access.Status != "active" {
		observability.RecordRevenueCatGrantExpired(observability.RevenueCatGrantClassTrial)
	}
	armed := access.Status == "inactive" && (priorStatus == "active" || priorStatus == "trial" || priorStatus == "trialing")
	if armed {
		observability.RecordRevenueCatRetentionEpisodeArmed()
	}
	source, key := bestRevenueCatGrant(priorGrants)
	if access.Status == "inactive" && priorStatus != "inactive" &&
		(source == observability.RevenueCatGrantClassComp || source == observability.RevenueCatGrantClassPaid) {
		observability.RecordRevenueCatGrantExpired(source)
	}
	if s.analytics == nil {
		return
	}
	if trialEnded {
		s.analytics.CaptureEvent(ctx, id, phclient.RevenueCatTrialEndedPayload{
			Entitlement: phclient.RevenueCatTrialEndedEntitlementTrialPremium,
			Converted:   access.Status == "active",
		})
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
	revocationSource := phclient.RevenueCatAccessRevokedSourceWebhook
	if source == "paid" {
		revocationSource = phclient.RevenueCatAccessRevokedSourceRefund
	}
	s.captureRevocation(ctx, id, key, revocationSource)
	s.analytics.CaptureEvent(ctx, id, phclient.RevenueCatAccessInactivePayload{
		PendingOperations: int64(state.PendingOperations),
	})
	if armed {
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

// captureRevocation emits the refund/revocation event naming the entitlement whose access was
// revoked and the bounded source of the revocation. It is the one-event-per-revocation signal:
// the refresh path emits it once for the inactive flip (refund when the provider read dropped a
// paid entitlement, webhook when it dropped a non-paid one), and ProcessOperation emits it once
// for an applied admin revoke. Nil-safe, and an entitlement that is not a known RevenueCat key is
// refused rather than offered to the gateway for a guaranteed drop.
func (s *RevenueCatService) captureRevocation(ctx context.Context, userID, entitlement string, source phclient.RevenueCatAccessRevokedSource) {
	if s.analytics == nil || entitlement == "" {
		return
	}
	if _, _, _, ok := domain.RevenueCatKey(entitlement); !ok {
		return
	}
	s.analytics.CaptureEvent(ctx, userID, phclient.RevenueCatAccessRevokedPayload{
		Entitlement: phclient.RevenueCatAccessRevokedEntitlement(entitlement),
		Source:      source,
	})
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
	var outcomeStage string
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
			stage, message := revenueCatOperationOutcome(op, workErr)
			outcomeStage = stage
			return session.FinishOperation(ctx, op.ID, stage, message, op.Class)
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
		stage, message := revenueCatOperationOutcome(op, workErr)
		outcomeStage = stage
		return session.FinishOperation(ctx, op.ID, stage, message, op.Class)
	})
	if err != nil {
		return err
	}
	if attempted == nil {
		return nil
	}
	if outcomeStage == "abandoned" {
		// Terminal: FinishOperation already recorded the abandonment metric, and no retryable work
		// remains. Reporting the provider error here would mark the River job failed and emit a
		// retryable-failure product event for an operation that will never be retried.
		return nil
	}
	phase := revenueCatOperationApplied
	if workErr != nil {
		phase = revenueCatOperationFailed
	}
	s.captureOperation(ctx, *attempted, phase)
	s.captureTrialStarted(ctx, *attempted, workErr == nil)
	if attempted.Revoke && workErr == nil {
		s.captureRevocation(ctx, attempted.UserID, attempted.Key, phclient.RevenueCatAccessRevokedSourceAdmin)
	}
	return workErr
}

// revenueCatOperationOutcome maps a provider or identity failure to the stage FinishOperation
// writes. A success is 'applied'; a failure below the cap stays 'pending' and is retried; a failure
// that is the operation's RevenueCatMaxOperationAttempts-th is written to the terminal 'abandoned'
// stage. op.Attempts is the count of PRIOR failures, so the attempt in flight is failure number
// op.Attempts+1 and the cap is reached when that number equals the constant.
func revenueCatOperationOutcome(op domain.RevenueCatOperation, workErr error) (string, string) {
	if workErr == nil {
		return "applied", ""
	}
	if op.Attempts+1 >= RevenueCatMaxOperationAttempts {
		return "abandoned", revenueCatErrorCode(workErr)
	}
	return "pending", revenueCatErrorCode(workErr)
}

func (s *RevenueCatService) applyOperation(ctx context.Context, session repository.RevenueCatSession, customer string, op domain.RevenueCatOperation, state *domain.RevenueCatState) error {
	now := time.Now()
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
		if !op.Revoke && revenueCatGrantMatches(g, op, now) {
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
	if !revenueCatDesired(observed, op, now) && !op.Revoke {
		// Do not grant while a conflicting old promotion still exists: retries must
		// first complete revoke to avoid RC's close-expiry duplicate suppression.
		for _, g := range observed {
			_, source, _, _ := domain.RevenueCatKey(g.LookupKey)
			if source == op.Class {
				return errors.New("promotional revocation not yet confirmed")
			}
		}
		if op.ExpiresAt == nil || op.ExpiresAt.After(now) {
			err = s.provider.Grant(ctx, customer, op.Key, op.ExpiresAt)
		}
	}
	final, readErr := s.provider.FetchEntitlements(ctx, customer)
	if readErr != nil {
		return readErr
	}
	if !revenueCatDesired(final, op, now) {
		if err != nil {
			return err
		}
		return errors.New("grant change not yet confirmed")
	}
	applyRevenueCatGrants(state, final, now)
	return session.Save(ctx, *state)
}
func sameRevenueCatExpiry(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.UnixMilli() == b.UnixMilli()
}

// RevenueCat materialises a `duration:lifetime` promotional grant as a concrete,
// per-grant jittered expiry roughly 200 years out (the observed sentinel is ~8.097e12 ms), so a
// permanent (nil-expiry) operation can never match by exact equality. A genuine finite comp is
// always far below this horizon; only another effectively-permanent grant satisfies a nil op. The
// horizon is 10 years, far below the ~200y sentinel and far above any real promotion.
const revenueCatPermanentHorizon = 10 * 365 * 24 * time.Hour

// RevenueCatMaxOperationAttempts bounds how many times one operation may fail before it is
// abandoned. A permanently non-converging operation — a provider outage that never clears, or an
// intent the provider never reflects — otherwise stays stage='pending' forever: every failure
// rewrites 'pending', every repair pass re-enqueues it, and each attempt re-applies provider side
// effects (revoke/grant). At the repair worker's one-minute cadence, 500 attempts is roughly eight
// hours of continuous failure, far beyond any transient outage, after which the operation is written
// to the terminal 'abandoned' stage and drops out of every stage='pending' read.
//
// Operator recovery: 'abandoned' is terminal and the admin retry gate rejects any non-pending
// stage, so an abandoned operation is never replayed in place. Recovery is to issue a NEW grant for
// the same class and tier: RequestGrant reaches insertRevenueCatOperation, which inserts a fresh
// stage='pending' row at the next version with a clean attempt count, re-attempting the desired
// state. The abandoned row is retained as the audit trail of the failed intent.
const RevenueCatMaxOperationAttempts = 500

func revenueCatGrantMatches(g rc.Entitlement, op domain.RevenueCatOperation, now time.Time) bool {
	if g.LookupKey != op.Key {
		return false
	}
	if op.ExpiresAt == nil {
		return g.ExpiresAt == nil || !g.ExpiresAt.Before(now.Add(revenueCatPermanentHorizon))
	}
	return sameRevenueCatExpiry(g.ExpiresAt, op.ExpiresAt)
}
func revenueCatDesired(grants []rc.Entitlement, op domain.RevenueCatOperation, now time.Time) bool {
	matched := false
	for _, g := range grants {
		_, source, _, ok := domain.RevenueCatKey(g.LookupKey)
		if !ok {
			return false
		}
		if source != op.Class {
			continue
		}
		if op.Revoke || !revenueCatGrantMatches(g, op, now) {
			return false
		}
		matched = true
	}
	// An intent whose fixed expiry has passed is successfully absent, not renewed.
	return matched || op.Revoke || (op.ExpiresAt != nil && !op.ExpiresAt.After(now))
}
