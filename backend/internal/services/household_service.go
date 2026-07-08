package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// householdInviteTTL is how long a newly created invite remains acceptable before
// expiring. Not specified by #1367 — chosen as a reasonable default; revisit if
// product requirements narrow this.
const householdInviteTTL = 7 * 24 * time.Hour

// HouseholdAcceptResult is returned by AcceptInvite: the caller's new membership row
// plus the household they joined, for the operation layer to shape the HTTP response.
type HouseholdAcceptResult struct {
	Member    domain.HouseholdMember
	Household domain.Household
}

// HouseholdCreateInviteResult is returned by CreateInvite. InviteLink carries the raw,
// one-time token URL so the owner can share it manually if email delivery isn't fully
// configured (#1367 decision #4; full delivery tracked by #1026).
type HouseholdCreateInviteResult struct {
	Invite     domain.HouseholdInvite
	InviteLink string
}

// HouseholdService implements the Phase 3 (#1367) household write paths: CRUD, member
// management, and the invite lifecycle including the consent-gated accept transaction.
// Phase 1/2 read paths (GetHouseholdForUser, GetEntitlementSummary) already exist and
// are reused here rather than duplicated.
type HouseholdService interface {
	CreateHousehold(ctx context.Context, ownerUserID, name string) (*domain.Household, error)
	GetMyHousehold(ctx context.Context, userID string) (*domain.Household, domain.HouseholdEntitlementSummary, bool, error)
	UpdateHouseholdName(ctx context.Context, userID, name string) (*domain.Household, error)
	DeleteHousehold(ctx context.Context, userID string) error

	ListMembers(ctx context.Context, userID string) ([]domain.HouseholdMember, error)
	RemoveMember(ctx context.Context, actorUserID, targetUserID string) error

	CreateInvite(ctx context.Context, ownerUserID, inviteeEmail, frontendURL string) (*HouseholdCreateInviteResult, error)
	ListInvites(ctx context.Context, ownerUserID string) ([]domain.HouseholdInvite, error)
	RevokeInvite(ctx context.Context, ownerUserID, inviteID string) error
	GetInvitePreview(ctx context.Context, rawToken string) (*repository.HouseholdInvitePreview, error)
	// AcceptInvite and DeclineInvite deliberately take only callerUserID (not an
	// email claim) — the email-match gate compares against the caller's PERSISTED,
	// VERIFIED users.email (fetched+locked inside the transaction), never a Clerk JWT
	// claim, which could be stale or unverified relative to our DB record (Codex review
	// finding).
	AcceptInvite(ctx context.Context, callerUserID, rawToken string, consent bool, hiddenAccountIDs []string) (*HouseholdAcceptResult, error)
	DeclineInvite(ctx context.Context, callerUserID, rawToken string) error

	SetAnalytics(a phclient.Analytics)
}

type householdService struct {
	db              repository.DB
	householdRepo   repository.HouseholdRepository
	inviteRepo      repository.HouseholdInviteRepository
	shareRepo       repository.HouseholdShareRepository
	entitlementRepo repository.EntitlementRepository
	entitlementSvc  *HouseholdEntitlementService
	userRepo        userLoader
	tierConfig      *config.TierConfig
	emailService    EmailService
	enc             *appcrypto.FieldEncryptor
	analytics       phclient.Analytics
}

// NewHouseholdService creates a new HouseholdService. db must be the same pool used to
// construct householdRepo/inviteRepo/shareRepo/entitlementRepo (non-transactional reads
// use those directly; mutating multi-statement operations open a transaction on db and
// construct fresh tx-scoped repository instances for the duration of that transaction).
func NewHouseholdService(
	db repository.DB,
	householdRepo repository.HouseholdRepository,
	inviteRepo repository.HouseholdInviteRepository,
	shareRepo repository.HouseholdShareRepository,
	entitlementRepo repository.EntitlementRepository,
	entitlementSvc *HouseholdEntitlementService,
	userRepo userLoader,
	tierConfig *config.TierConfig,
	emailService EmailService,
	enc *appcrypto.FieldEncryptor,
) HouseholdService {
	return &householdService{
		db:              db,
		householdRepo:   householdRepo,
		inviteRepo:      inviteRepo,
		shareRepo:       shareRepo,
		entitlementRepo: entitlementRepo,
		entitlementSvc:  entitlementSvc,
		userRepo:        userRepo,
		tierConfig:      tierConfig,
		emailService:    emailService,
		enc:             enc,
	}
}

// SetAnalytics wires in the optional, nil-safe PostHog analytics client.
func (s *householdService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
}

func (s *householdService) capture(userID, event string) {
	if s.analytics != nil {
		s.analytics.Capture(userID, event, nil)
	}
}

// ─── Household CRUD ───────────────────────────────────────────────────────────

func (s *householdService) CreateHousehold(ctx context.Context, ownerUserID, name string) (*domain.Household, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var cerr error
	defer func() {
		if cerr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	hhRepo := repository.NewHouseholdRepository(tx)

	h := &domain.Household{OwnerUserID: ownerUserID, Name: name}
	if cerr = h.Validate(); cerr != nil {
		return nil, cerr
	}
	if cerr = hhRepo.CreateHousehold(ctx, h); cerr != nil {
		return nil, cerr
	}
	member := &domain.HouseholdMember{
		HouseholdID: h.ID,
		UserID:      ownerUserID,
		Role:        domain.HouseholdRoleOwner,
		Status:      domain.HouseholdMemberStatusActive,
	}
	if cerr = hhRepo.InsertMember(ctx, member); cerr != nil {
		return nil, cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return nil, cerr
	}
	s.capture(ownerUserID, phclient.EventHouseholdCreated)
	return h, nil
}

func (s *householdService) GetMyHousehold(ctx context.Context, userID string) (*domain.Household, domain.HouseholdEntitlementSummary, bool, error) {
	household, _, found, err := s.householdRepo.GetHouseholdForUser(ctx, userID)
	if err != nil {
		return nil, domain.HouseholdEntitlementSummary{}, false, err
	}
	if !found {
		return nil, domain.HouseholdEntitlementSummary{}, false, nil
	}
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, domain.HouseholdEntitlementSummary{}, false, err
	}
	summary, err := s.entitlementSvc.GetEntitlementSummary(ctx, user)
	if err != nil {
		return nil, domain.HouseholdEntitlementSummary{}, false, err
	}
	return household, summary, true, nil
}

func (s *householdService) UpdateHouseholdName(ctx context.Context, userID, name string) (*domain.Household, error) {
	household, isOwner, found, err := s.householdRepo.GetHouseholdForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !found || !isOwner {
		return nil, domain.ErrNotHouseholdOwner
	}
	candidate := &domain.Household{OwnerUserID: userID, Name: name}
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	if err := s.householdRepo.UpdateHouseholdName(ctx, household.ID, userID, name); err != nil {
		return nil, err
	}
	household.Name = name
	return household, nil
}

func (s *householdService) DeleteHousehold(ctx context.Context, userID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	var cerr error
	defer func() {
		if cerr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	hhRepo := repository.NewHouseholdRepository(tx)
	entRepo := repository.NewEntitlementRepository(tx)

	household, isOwner, found, cerr := hhRepo.GetHouseholdForUser(ctx, userID)
	if cerr != nil {
		return cerr
	}
	if !found || !isOwner {
		cerr = domain.ErrNotHouseholdOwner
		return cerr
	}
	if _, cerr = hhRepo.LockHouseholdForUpdate(ctx, household.ID); cerr != nil {
		return cerr
	}
	activeCount, cerr := hhRepo.CountActiveMembers(ctx, household.ID)
	if cerr != nil {
		return cerr
	}
	if activeCount > 1 {
		cerr = domain.ErrHouseholdHasActiveMembers
		return cerr
	}
	if cerr = hhRepo.DeleteHousehold(ctx, household.ID, userID); cerr != nil {
		return cerr
	}
	if cerr = entRepo.ExpireActiveEntitlement(ctx, userID); cerr != nil {
		return cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return cerr
	}
	s.capture(userID, phclient.EventHouseholdDisbanded)
	return nil
}

// ─── Member management ────────────────────────────────────────────────────────

func (s *householdService) ListMembers(ctx context.Context, userID string) ([]domain.HouseholdMember, error) {
	household, _, found, err := s.householdRepo.GetHouseholdForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.ErrHouseholdNotFound
	}
	return s.householdRepo.ListMembers(ctx, household.ID)
}

func (s *householdService) RemoveMember(ctx context.Context, actorUserID, targetUserID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	var cerr error
	defer func() {
		if cerr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	hhRepo := repository.NewHouseholdRepository(tx)
	entRepo := repository.NewEntitlementRepository(tx)
	shareRepo := repository.NewHouseholdShareRepository(tx)

	household, isOwner, found, cerr := hhRepo.GetHouseholdForUser(ctx, actorUserID)
	if cerr != nil {
		return cerr
	}
	if !found {
		cerr = domain.ErrMemberNotFound
		return cerr
	}
	isSelfLeave := actorUserID == targetUserID
	if !isSelfLeave && !isOwner {
		cerr = domain.ErrNotHouseholdOwner
		return cerr
	}
	if targetUserID == household.OwnerUserID {
		// Reject removing the owner via this path (self-leave or owner-remove) — owner
		// exit only via DeleteHousehold. Prevents an ownerless-but-active household
		// (Codex #1367 review, High finding 4).
		cerr = domain.ErrCannotRemoveOwner
		return cerr
	}

	if cerr = hhRepo.RemoveMember(ctx, household.ID, targetUserID); cerr != nil {
		return cerr
	}
	if cerr = entRepo.ExpireActiveEntitlement(ctx, targetUserID); cerr != nil {
		return cerr
	}
	if cerr = shareRepo.RevokeSharesForUser(ctx, household.ID, targetUserID); cerr != nil {
		return cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return cerr
	}
	if isSelfLeave {
		s.capture(targetUserID, phclient.EventHouseholdMemberLeft)
	} else {
		s.capture(actorUserID, phclient.EventHouseholdMemberRemoved)
	}
	return nil
}

// ─── Invite lifecycle ──────────────────────────────────────────────────────────

func (s *householdService) CreateInvite(ctx context.Context, ownerUserID, inviteeEmail, frontendURL string) (*HouseholdCreateInviteResult, error) {
	owner, err := s.userRepo.GetUserByID(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if sameEmail(owner.Email, inviteeEmail) {
		return nil, domain.ErrCannotInviteSelf
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var cerr error
	defer func() {
		if cerr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	hhRepo := repository.NewHouseholdRepository(tx)
	inviteRepo := repository.NewHouseholdInviteRepository(tx, s.enc)

	household, isOwner, found, cerr := hhRepo.GetHouseholdForUser(ctx, ownerUserID)
	if cerr != nil {
		return nil, cerr
	}
	if !found || !isOwner {
		cerr = domain.ErrNotHouseholdOwner
		return nil, cerr
	}
	if _, cerr = hhRepo.LockHouseholdForUpdate(ctx, household.ID); cerr != nil {
		return nil, cerr
	}

	tier, cerr := s.tierConfig.GetTier(domain.TierName(owner.ComputeEffectiveTier()))
	if cerr != nil {
		return nil, cerr
	}
	active, cerr := hhRepo.CountActiveMembers(ctx, household.ID)
	if cerr != nil {
		return nil, cerr
	}
	pending, cerr := hhRepo.CountPendingInvites(ctx, household.ID)
	if cerr != nil {
		return nil, cerr
	}
	if active+pending >= tier.Pricing.UsersIncluded {
		cerr = domain.ErrHouseholdSeatsFull
		return nil, cerr
	}

	rawToken, tokenHash, cerr := generateInviteToken()
	if cerr != nil {
		return nil, cerr
	}
	expiresAt := time.Now().Add(householdInviteTTL)
	inv := &domain.HouseholdInvite{
		HouseholdID:   household.ID,
		InviterUserID: ownerUserID,
		InviteeEmail:  inviteeEmail,
		TokenHash:     tokenHash,
		Status:        domain.HouseholdInviteStatusPending,
		ExpiresAt:     expiresAt,
	}
	if cerr = inviteRepo.Create(ctx, inv); cerr != nil {
		return nil, cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return nil, cerr
	}

	inviteLink := frontendURL + "/households/invites/" + rawToken
	// Best-effort — delivery is not guaranteed; the invite link above is always
	// returned so the owner can share it manually (#1367 decision #4, see #1026).
	_ = s.emailService.SendHouseholdInvite(ctx, inviteeEmail, owner.Email, household.Name, inviteLink, expiresAt)
	s.capture(ownerUserID, phclient.EventHouseholdInviteSent)

	return &HouseholdCreateInviteResult{Invite: *inv, InviteLink: inviteLink}, nil
}

func (s *householdService) ListInvites(ctx context.Context, ownerUserID string) ([]domain.HouseholdInvite, error) {
	household, isOwner, found, err := s.householdRepo.GetHouseholdForUser(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if !found || !isOwner {
		return nil, domain.ErrNotHouseholdOwner
	}
	return s.inviteRepo.ListPendingByHousehold(ctx, household.ID)
}

func (s *householdService) RevokeInvite(ctx context.Context, ownerUserID, inviteID string) error {
	household, isOwner, found, err := s.householdRepo.GetHouseholdForUser(ctx, ownerUserID)
	if err != nil {
		return err
	}
	if !found || !isOwner {
		return domain.ErrNotHouseholdOwner
	}
	return s.inviteRepo.Revoke(ctx, household.ID, inviteID)
}

func (s *householdService) GetInvitePreview(ctx context.Context, rawToken string) (*repository.HouseholdInvitePreview, error) {
	return s.inviteRepo.GetPreviewByTokenHash(ctx, hashToken(rawToken))
}

func (s *householdService) DeclineInvite(ctx context.Context, callerUserID, rawToken string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	var cerr error
	defer func() {
		if cerr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	inviteRepo := repository.NewHouseholdInviteRepository(tx, s.enc)

	inv, cerr := inviteRepo.GetByTokenHashForUpdate(ctx, hashToken(rawToken))
	if cerr != nil {
		return cerr
	}
	if cerr = checkInvitePending(inv); cerr != nil {
		return cerr
	}
	caller, cerr := s.lockAndGetUser(ctx, tx, callerUserID)
	if cerr != nil {
		return cerr
	}
	if !caller.EmailVerified || !sameEmail(caller.Email, inv.InviteeEmail) {
		cerr = domain.ErrInviteEmailMismatch
		return cerr
	}
	if cerr = inviteRepo.MarkDeclined(ctx, inv.ID); cerr != nil {
		return cerr
	}

	return tx.Commit(ctx)
}

// AcceptInvite is the consent-gated join transaction. See #1367 decision docs for the
// full step-by-step rationale; each guard below cites the review finding it closes.
func (s *householdService) AcceptInvite(ctx context.Context, callerUserID, rawToken string, consent bool, hiddenAccountIDs []string) (*HouseholdAcceptResult, error) {
	if !consent {
		return nil, domain.ErrInvalidConsent
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var cerr error
	defer func() {
		if cerr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	inviteRepo := repository.NewHouseholdInviteRepository(tx, s.enc)
	hhRepo := repository.NewHouseholdRepository(tx)
	shareRepo := repository.NewHouseholdShareRepository(tx)
	entRepo := repository.NewEntitlementRepository(tx)

	// (1)(2) Lock invite; validate pending & not expired.
	inv, cerr := inviteRepo.GetByTokenHashForUpdate(ctx, hashToken(rawToken))
	if cerr != nil {
		return nil, cerr
	}
	if cerr = checkInvitePending(inv); cerr != nil {
		return nil, cerr
	}

	// (3) Lock the caller's user row for the rest of this transaction so a concurrent
	// Paddle webhook cannot race the paying-invitee guard below (Codex review finding —
	// the lock blocks any conflicting UPDATE from committing until we do), then read via
	// the existing userRepo for a view that's guaranteed fresh as of the lock.
	caller, cerr := s.lockAndGetUser(ctx, tx, callerUserID)
	if cerr != nil {
		return nil, cerr
	}

	// (3a) Email match against the caller's own PERSISTED, VERIFIED account email — not
	// a Clerk JWT claim, which could be stale or unverified relative to our DB record
	// (Codex review finding).
	if !caller.EmailVerified || !sameEmail(caller.Email, inv.InviteeEmail) {
		cerr = domain.ErrInviteEmailMismatch
		return nil, cerr
	}

	// (3b) Paying-invitee guard (decision #6): block any live Paddle billing relationship.
	if callerHasLivePaddleSubscription(caller) {
		cerr = &domain.InviteeHasActivePaidSubscriptionError{EligibleAfter: caller.SubscriptionEndsAt}
		return nil, cerr
	}

	// (4) Lock household; owner must not be deleted/read-only; seat re-check.
	ownerUserID, cerr := hhRepo.LockHouseholdForUpdate(ctx, inv.HouseholdID)
	if cerr != nil {
		return nil, cerr
	}
	owner, cerr := s.userRepo.GetUserByID(ctx, ownerUserID)
	if cerr != nil {
		return nil, cerr
	}
	if owner.DeletionRequestedAt != nil || domain.SubscriptionStatus(owner.SubscriptionStatus).IsReadOnlySubscription() {
		cerr = domain.ErrOwnerPlanInactive
		return nil, cerr
	}
	ownerTier, cerr := s.tierConfig.GetTier(domain.TierName(owner.ComputeEffectiveTier()))
	if cerr != nil {
		return nil, cerr
	}
	activeCount, cerr := hhRepo.CountActiveMembers(ctx, inv.HouseholdID)
	if cerr != nil {
		return nil, cerr
	}
	if activeCount >= ownerTier.Pricing.UsersIncluded {
		cerr = domain.ErrHouseholdSeatsFull
		return nil, cerr
	}

	// (5) Insert membership, snapshotting the caller's prior tier/status for Phase 7 restore.
	priorTier := caller.ComputeEffectiveTier()
	priorStatus := caller.SubscriptionStatus
	member := &domain.HouseholdMember{
		HouseholdID:             inv.HouseholdID,
		UserID:                  callerUserID,
		Role:                    domain.HouseholdRoleMember,
		Status:                  domain.HouseholdMemberStatusActive,
		PriorSubscriptionTier:   &priorTier,
		PriorSubscriptionStatus: &priorStatus,
	}
	if cerr = hhRepo.InsertMember(ctx, member); cerr != nil {
		return nil, cerr
	}

	// (6) Validate hidden_account_ids are a subset of the caller's own accounts (IDOR guard).
	fingerprints, cerr := shareRepo.ListActiveUserAccountFingerprints(ctx, callerUserID)
	if cerr != nil {
		return nil, cerr
	}
	ownAccountIDs := make(map[string]bool, len(fingerprints))
	for _, fp := range fingerprints {
		ownAccountIDs[fp.UserAccountID] = true
	}
	hiddenSet := make(map[string]bool, len(hiddenAccountIDs))
	for _, id := range hiddenAccountIDs {
		if !ownAccountIDs[id] {
			cerr = domain.ErrInvalidConsent
			return nil, cerr
		}
		hiddenSet[id] = true
	}

	// (7) Append consent snapshot.
	shared, hidden := splitBySet(fingerprints, hiddenSet)
	snapshot, cerr := json.Marshal(map[string]any{"shared": shared, "hidden": hidden})
	if cerr != nil {
		return nil, cerr
	}
	if cerr = shareRepo.InsertConsent(ctx, &domain.HouseholdShareConsent{
		HouseholdID:    inv.HouseholdID,
		UserID:         callerUserID,
		ConsentVersion: domain.HouseholdShareConsentVersion,
		Snapshot:       json.RawMessage(snapshot),
	}); cerr != nil {
		return nil, cerr
	}

	// (8) Default-share fan-out.
	for _, fp := range fingerprints {
		visibility := domain.ShareVisibilityShared
		if hiddenSet[fp.UserAccountID] {
			visibility = domain.ShareVisibilityHidden
		}
		if cerr = shareRepo.InsertShare(ctx, inv.HouseholdID, fp.UserAccountID, fp.PhysicalBlindIdx, visibility); cerr != nil {
			return nil, cerr
		}
	}

	// (9) Defensive expire of any pre-existing open entitlement (guards the DB's
	// one-open-entitlement-per-user partial unique index).
	if cerr = entRepo.ExpireActiveEntitlement(ctx, callerUserID); cerr != nil {
		return nil, cerr
	}

	// (10) Insert the household entitlement (payer == owner satisfies the composite FK).
	if cerr = entRepo.CreateHouseholdEntitlement(ctx, callerUserID, string(ownerTier.Name), owner.SubscriptionStatus, ownerUserID, inv.HouseholdID); cerr != nil {
		return nil, cerr
	}

	// (11) Flip invite to accepted.
	if cerr = inviteRepo.MarkAccepted(ctx, inv.ID, callerUserID); cerr != nil {
		return nil, cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return nil, cerr
	}

	s.capture(callerUserID, phclient.EventHouseholdInviteAccepted)
	return &HouseholdAcceptResult{
		Member:    *member,
		Household: domain.Household{ID: inv.HouseholdID, OwnerUserID: ownerUserID},
	}, nil
}

// lockAndGetUser takes a row lock on userID's users row for the remainder of tx, then
// fetches the user via the existing (pool-bound) userRepo. Acquiring the lock first
// guarantees no concurrent writer (e.g. a Paddle webhook) can commit a conflicting
// update until this transaction commits or rolls back, so the subsequent read is
// guaranteed fresh as of the lock and stays fresh for the transaction's duration —
// closing the race a pool-only read would leave open (Codex review finding).
func (s *householdService) lockAndGetUser(ctx context.Context, tx pgx.Tx, userID string) (*domain.User, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}
	return s.userRepo.GetUserByID(ctx, userID)
}

// ─── helpers ───────────────────────────────────────────────────────────────────

// checkInvitePending maps an invite's current status/expiry to the sentinel the issue
// specifies: expired or non-pending (already accepted/declined/revoked) both surface as
// 401 "invite is no longer valid" at the HTTP layer, via ErrInviteExpired/ErrInviteNotPending.
func checkInvitePending(inv *domain.HouseholdInvite) error {
	if inv.Status != domain.HouseholdInviteStatusPending {
		return domain.ErrInviteNotPending
	}
	if !inv.ExpiresAt.After(time.Now()) {
		return domain.ErrInviteExpired
	}
	return nil
}

// sameEmail compares two email addresses using the same normalization the DB blind
// index uses, so "Friend@Example.com" and "friend@example.com " compare equal.
func sameEmail(a, b string) bool {
	return appcrypto.NormalizeEmailForBlindIndex(a) == appcrypto.NormalizeEmailForBlindIndex(b)
}

// callerHasLivePaddleSubscription implements the #1367 decision #6 predicate: block any
// invitee with a live Paddle billing relationship. "active" and "trialing" (a
// card-on-file paid trial that auto-converts) are obviously blocked; "past_due" is also
// blocked because it auto-recovers to "active" via webhook and would otherwise create
// paid+covered overlap. Local "trial" (60-day, no card), "cancelled"/"inactive"
// (terminated Paddle sub), and manual-tier-override comps (no PaddleSubscriptionID) all
// join freely — no refund risk.
func callerHasLivePaddleSubscription(u *domain.User) bool {
	if u.PaddleSubscriptionID == nil {
		return false
	}
	switch domain.SubscriptionStatus(u.SubscriptionStatus) {
	case domain.SubscriptionStatusActive, domain.SubscriptionStatusTrialing, domain.SubscriptionStatusPastDue:
		return true
	default:
		return false
	}
}

// splitBySet partitions fingerprints into shared/hidden account-ID lists for the
// consent snapshot, per the hiddenSet built from the caller's validated hidden IDs.
func splitBySet(fingerprints []repository.AccountFingerprint, hiddenSet map[string]bool) (shared, hidden []string) {
	for _, fp := range fingerprints {
		if hiddenSet[fp.UserAccountID] {
			hidden = append(hidden, fp.UserAccountID)
		} else {
			shared = append(shared, fp.UserAccountID)
		}
	}
	return shared, hidden
}

// generateInviteToken returns a fresh random raw token (returned to the caller once,
// used in the invite link) and its sha256 hex digest (the only thing persisted).
func generateInviteToken() (rawToken, tokenHash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	rawToken = base64.RawURLEncoding.EncodeToString(buf)
	return rawToken, hashToken(rawToken), nil
}

// hashToken reproduces the sha256 hex digest stored as token_hash, given a raw token.
// Token lookups are by unique index (token_hash), not a secret comparison in Go code,
// so no constant-time comparison is needed here.
func hashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
