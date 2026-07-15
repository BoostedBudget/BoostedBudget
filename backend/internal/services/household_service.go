package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
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

// HouseholdMemberView is the GET /households/me/members response shape: membership data
// plus a best-effort display name + avatar resolved from Clerk. The issue requires
// "name, avatar, role, joined_at", but that identity data lives in Clerk, not the users
// table — domain.HouseholdMember deliberately stays bare (role/status/joined_at only);
// this view composes it with the optional Clerk lookup. DisplayName/AvatarURL are blank
// (never an error) when no MemberProfileResolver is wired, or when resolving a specific
// member's profile fails — a single bad Clerk lookup must not break the whole list.
// #1367 — Phase 3 Round 2 (GT-3).
type HouseholdMemberView struct {
	UserID      string                       `json:"user_id"`
	Role        domain.HouseholdRole         `json:"role"`
	Status      domain.HouseholdMemberStatus `json:"status"`
	JoinedAt    time.Time                    `json:"joined_at"`
	DisplayName string                       `json:"display_name,omitempty"`
	AvatarURL   string                       `json:"avatar_url,omitempty"`
}

// QueuedRemovalEmail is a "you've been removed from a household" notification collected
// during a removal transaction (removeMemberTx) for the caller to send AFTER that
// transaction commits — never inside it. RemoveMember/OffboardExcessMembers/
// ForceDisband each send these via sendRemovalEmails (EmailService.
// SendHouseholdMemberRemoved) immediately after their own tx.Commit succeeds (#1371 —
// Task 3). PurgeDate is now+30d, matching the purge-window messaging ("your data will
// be permanently removed on <date>").
type QueuedRemovalEmail struct {
	ToEmail       string
	HouseholdName string
	PurgeDate     time.Time
}

// removalPurgeWindow is how long a removed member's membership/shares/entitlement
// linger before PurgeRemovedMembership permanently deletes them — the window
// communicated in QueuedRemovalEmail.PurgeDate. Not specified by #1371 Task 0; chosen
// to match the task description's "now + 30 days" default. Revisit if product
// requirements narrow this once Task 2 (the purge worker) schedules real purge jobs.
const removalPurgeWindow = 30 * 24 * time.Hour

// MemberProfileResolver resolves a household member's display name + avatar. Optional
// (nil-safe) — when HouseholdService.SetMemberProfileResolver is never called,
// ListMembers returns bare membership data with empty DisplayName/AvatarURL rather than
// erroring. #1367 — Phase 3 Round 2 (GT-3).
type MemberProfileResolver interface {
	ResolveProfile(ctx context.Context, userID string) (displayName, avatarURL string, err error)
}

// householdIdentityLookup narrows repository.UserIdentityRepository to what
// clerkMemberProfileResolver needs.
type householdIdentityLookup interface {
	GetByUserID(ctx context.Context, userID string) ([]*domain.UserIdentity, error)
}

// clerkMemberProfileResolver is the production MemberProfileResolver: maps an internal
// user_id to its primary Clerk ID via UserIdentityRepository (GetByUserID is documented
// to order primary-first), then fetches display name + avatar live from Clerk. Households
// cap at <=5 members (config/subscription_tiers.yaml UsersIncluded), so this bounded
// per-member fetch is intentional — no batch Clerk API call and no cache (YAGNI); see Gap
// Analysis Round 2, GT-3.
type clerkMemberProfileResolver struct {
	identityRepo householdIdentityLookup
	clerkGetter  ClerkUserGetter
}

// NewClerkMemberProfileResolver builds the production MemberProfileResolver. Wired via
// HouseholdService.SetMemberProfileResolver in cmd/server/main.go.
func NewClerkMemberProfileResolver(identityRepo householdIdentityLookup, clerkGetter ClerkUserGetter) MemberProfileResolver {
	return &clerkMemberProfileResolver{identityRepo: identityRepo, clerkGetter: clerkGetter}
}

func (r *clerkMemberProfileResolver) ResolveProfile(ctx context.Context, userID string) (string, string, error) {
	identities, err := r.identityRepo.GetByUserID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	if len(identities) == 0 {
		return "", "", nil // no linked Clerk identity — nothing to show, not an error
	}
	clerkUser, err := r.clerkGetter.GetUser(ctx, identities[0].ClerkUserID)
	if err != nil {
		return "", "", err
	}
	return clerkDisplayName(clerkUser), clerkAvatarURL(clerkUser), nil
}

// clerkDisplayName picks the best available display name: Username, then "First Last",
// then blank. Deliberately never falls back to email — that's more sensitive than a
// missing name and handled separately if ever needed.
func clerkDisplayName(u *clerk.User) string {
	if u.Username != nil && *u.Username != "" {
		return *u.Username
	}
	var first, last string
	if u.FirstName != nil {
		first = *u.FirstName
	}
	if u.LastName != nil {
		last = *u.LastName
	}
	return strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
}

func clerkAvatarURL(u *clerk.User) string {
	if u.ImageURL != nil {
		return *u.ImageURL
	}
	return ""
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

	ListMembers(ctx context.Context, userID string) ([]HouseholdMemberView, error)
	RemoveMember(ctx context.Context, actorUserID, targetUserID string) error

	// OffboardExcessMembers brings householdID back into compliance with its owner's
	// current effective seat count by removing the most-recently-joined active
	// non-owner members (LIFO) one at a time, until active count <= allowed seats, all
	// inside one transaction. A no-op (empty slice, nil error) if the household is
	// already within its seat count. Safe for a background worker to call — never
	// nests a transaction. Returns the removal emails to send after commit. #1371 —
	// Phase 7 foundation; the decision of WHEN/how-many to call this with (grace
	// periods, over_capacity_since bookkeeping) is Task 6's responsibility, not this
	// method's.
	OffboardExcessMembers(ctx context.Context, householdID string) ([]QueuedRemovalEmail, error)

	// PurgeRemovedMembership permanently deletes a household_members row (identified by
	// its own id) that is already status='removed', along with that member's
	// household_account_shares rows for the household and their household-scoped
	// entitlement, all inside one transaction. householdID is the row's own household —
	// the caller (the daily purge worker) already knows it from its scan, and it's used
	// to take the household-row lock FIRST, per this codebase's lock-ordering convention
	// (see AcceptInvite/DeleteHousehold/OffboardExcessMembers/ForceDisband). Does NOT
	// touch the member's own accounts/transactions/budgets. Returns
	// domain.ErrHouseholdNotFound if the household row can't be locked, or
	// domain.ErrMemberNotFound if no such removed row exists. #1371 — Phase 7 foundation
	// (run ~30 days after removal).
	PurgeRemovedMembership(ctx context.Context, householdID, membershipID string) error

	// ForceDisband removes every active non-owner member of ownerUserID's household,
	// then dissolves the household exactly like DeleteHousehold does — all inside one
	// transaction (unlike the public DeleteHousehold, which refuses to disband while
	// active non-owner members remain). Returns domain.ErrNotHouseholdOwner if
	// ownerUserID does not own a household. Returns the removal emails to send after
	// commit. #1371 — Phase 7 foundation.
	ForceDisband(ctx context.Context, ownerUserID string) ([]QueuedRemovalEmail, error)

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

	// ActiveMemberUserIDs returns the user IDs of all active members of householdID
	// (owner included), in joined_at order. One query. Issue #1369 — Phase 5.
	ActiveMemberUserIDs(ctx context.Context, householdID string) ([]string, error)

	// ResolveScope turns a `scope` query-param value into a validated
	// *domain.HouseholdScope for the requesting user, or nil for personal scope.
	// `""` and `"personal"` both return (nil, nil) — today's single-user behavior.
	// `"household"` resolves the caller's own active household membership and its
	// current active members; a caller with no active household membership gets
	// domain.ErrHouseholdNotFound (mapped to HTTP 404 by callers). Repositories must
	// trust only a HouseholdScope returned from here — never one built from raw
	// request input. Issue #1369 — Phase 5.
	ResolveScope(ctx context.Context, requestingUserID, scopeParam string) (*domain.HouseholdScope, error)

	SetAnalytics(a phclient.Analytics)
	// SetMemberProfileResolver wires optional (nil-safe) Clerk name/avatar enrichment for
	// ListMembers. #1367 — Phase 3 Round 2 (GT-3).
	SetMemberProfileResolver(r MemberProfileResolver)
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
	memberProfiles  MemberProfileResolver

	// acceptInviteAfterOwnerCheckHook is a test-only synchronization seam (see the call site
	// in AcceptInvite for why it exists). Always nil in production.
	acceptInviteAfterOwnerCheckHook func()
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

// SetMemberProfileResolver wires in the optional, nil-safe Clerk name/avatar enrichment
// for ListMembers. #1367 — Phase 3 Round 2 (GT-3).
func (s *householdService) SetMemberProfileResolver(r MemberProfileResolver) {
	s.memberProfiles = r
}

// ─── Household CRUD ───────────────────────────────────────────────────────────

func (s *householdService) CreateHousehold(ctx context.Context, ownerUserID, name string) (*domain.Household, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var cerr error
	// Unconditional rollback-on-defer (Gap Analysis Round 5, GT-7): a panic mid-transaction
	// unwinds past every `cerr = ...; return cerr` assignment, so a conditional
	// `if cerr != nil` check here would never fire and the transaction would leak under the
	// app's panic-recovery middleware. Rollback after a successful Commit is a documented
	// pgx no-op (ErrTxClosed, discarded) — this matches the existing codebase idiom already
	// used in registration_service.go/profile_service.go.
	defer func() {
		_ = tx.Rollback(ctx)
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
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
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

func (s *householdService) ListMembers(ctx context.Context, userID string) ([]HouseholdMemberView, error) {
	household, _, found, err := s.householdRepo.GetHouseholdForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.ErrHouseholdNotFound
	}
	members, err := s.householdRepo.ListMembers(ctx, household.ID)
	if err != nil {
		return nil, err
	}

	views := make([]HouseholdMemberView, len(members))
	for i, m := range members {
		views[i] = HouseholdMemberView{
			UserID:   m.UserID,
			Role:     m.Role,
			Status:   m.Status,
			JoinedAt: m.JoinedAt,
		}
		if s.memberProfiles == nil {
			continue
		}
		name, avatar, perr := s.memberProfiles.ResolveProfile(ctx, m.UserID)
		if perr != nil {
			// Fail-soft per member — one bad Clerk lookup must not break the whole list.
			slog.Default().Warn("household: failed to resolve member profile; leaving name/avatar blank",
				"user_id", m.UserID, "error", perr)
			continue
		}
		views[i].DisplayName = name
		views[i].AvatarURL = avatar
	}
	return views, nil
}

// ─── Phase 5 (#1369): household aggregation scope resolution ────────────────

func (s *householdService) ActiveMemberUserIDs(ctx context.Context, householdID string) ([]string, error) {
	members, err := s.householdRepo.ListMembers(ctx, householdID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.UserID
	}
	return ids, nil
}

func (s *householdService) ResolveScope(ctx context.Context, requestingUserID, scopeParam string) (*domain.HouseholdScope, error) {
	if scopeParam == "" || scopeParam == "personal" {
		return nil, nil
	}
	household, _, found, err := s.householdRepo.GetHouseholdForUser(ctx, requestingUserID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.ErrHouseholdNotFound
	}
	memberIDs, err := s.ActiveMemberUserIDs(ctx, household.ID)
	if err != nil {
		return nil, err
	}
	return &domain.HouseholdScope{HouseholdID: household.ID, MemberUserIDs: memberIDs}, nil
}

func (s *householdService) RemoveMember(ctx context.Context, actorUserID, targetUserID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	var cerr error
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)

	household, _, found, cerr := hhRepo.GetHouseholdForUser(ctx, actorUserID)
	if cerr != nil {
		return cerr
	}
	if !found {
		cerr = domain.ErrMemberNotFound
		return cerr
	}

	email, cerr := s.removeMemberTx(ctx, tx, household, actorUserID, targetUserID)
	if cerr != nil {
		return cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return cerr
	}
	if actorUserID == targetUserID {
		s.capture(targetUserID, phclient.EventHouseholdMemberLeft)
	} else {
		s.capture(actorUserID, phclient.EventHouseholdMemberRemoved)
	}
	if email != nil {
		s.sendRemovalEmails(ctx, []QueuedRemovalEmail{*email})
	}
	return nil
}

// removeMemberTx contains the actual soft-remove + expire-entitlement + revoke-shares
// sequence, scoped to the caller-owned transaction tx — it never Begins/Commits/
// Rollbacks. Extracted from RemoveMember (#1371 — Phase 7 foundation) so
// OffboardExcessMembers and ForceDisband can call it multiple times within ONE shared
// transaction, and so a future force-disband endpoint never has to nest a transaction.
// household must already be resolved/locked by the caller (GetHouseholdForUser,
// LockHouseholdByID, etc.) — this never looks it up itself, so it stays reusable across
// every caller's own resolution strategy (by actor's membership, by householdID, ...).
//
// Authorization (unchanged from the original RemoveMember): the owner can remove any
// non-owner member; any member can self-leave; the owner can never be removed via this
// path (owner exit is DeleteHousehold/ForceDisband only — Codex #1367 review, High
// finding 4).
func (s *householdService) removeMemberTx(ctx context.Context, tx pgx.Tx, household *domain.Household, actorUserID, targetUserID string) (*QueuedRemovalEmail, error) {
	isSelfLeave := actorUserID == targetUserID
	isOwner := actorUserID == household.OwnerUserID
	if !isSelfLeave && !isOwner {
		return nil, domain.ErrNotHouseholdOwner
	}
	if targetUserID == household.OwnerUserID {
		return nil, domain.ErrCannotRemoveOwner
	}

	hhRepo := repository.NewHouseholdRepository(tx)
	entRepo := repository.NewEntitlementRepository(tx)
	shareRepo := repository.NewHouseholdShareRepository(tx)

	if err := hhRepo.RemoveMember(ctx, household.ID, targetUserID); err != nil {
		return nil, err
	}
	if err := entRepo.ExpireActiveEntitlement(ctx, targetUserID); err != nil {
		return nil, err
	}
	if err := shareRepo.RevokeSharesForUser(ctx, household.ID, targetUserID); err != nil {
		return nil, err
	}

	// #1371 — Task 6, Part D: grace-clear on in-app member removal. Only checked (and
	// only EVER checked) when the household is currently in over-capacity grace —
	// households never in grace skip this entirely, no extra queries. Re-counts
	// activeCount POST-removal (the removal above already committed within this same
	// tx) against the owner's CURRENT effective-tier seat count (same technique as
	// paddle_checkout.go's checkDowngradeSeatCapacity — Task 5), so an owner manually
	// removing members during grace gets immediate credit without waiting for the next
	// daily household_over_capacity_check worker run (Task 6, Part C).
	if household.OverCapacitySince != nil {
		owner, err := s.userRepo.GetUserByID(ctx, household.OwnerUserID)
		if err != nil {
			return nil, err
		}
		ownerTier, err := s.tierConfig.GetTier(domain.TierName(owner.ComputeEffectiveTier()))
		if err != nil {
			return nil, err
		}
		activeCount, err := hhRepo.CountActiveMembers(ctx, household.ID)
		if err != nil {
			return nil, err
		}
		if activeCount <= ownerTier.Pricing.UsersIncluded {
			if err := hhRepo.ClearOverCapacity(ctx, household.ID); err != nil {
				return nil, err
			}
		}
	}

	// Collect (never send) the removal notification for the caller to fire after commit
	// — see sendRemovalEmails (#1371 — Task 3).
	target, err := s.userRepo.GetUserByID(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	return &QueuedRemovalEmail{
		ToEmail:       target.Email,
		HouseholdName: household.Name,
		PurgeDate:     time.Now().Add(removalPurgeWindow),
	}, nil
}

// sendRemovalEmails is the best-effort, log-and-continue notification step called by
// RemoveMember/OffboardExcessMembers/ForceDisband immediately AFTER their own
// tx.Commit succeeds — never before. By the time this runs, the removal is already
// durable, so a failed send must never be surfaced as (or cause a rollback of) the
// caller's removal error; it is only ever logged. Nil-safe: no-ops if no EmailService
// was injected, matching this codebase's analytics/email nil-safe convention (see
// CLAUDE.md "No-op safe"). #1371 — Task 3.
func (s *householdService) sendRemovalEmails(ctx context.Context, queued []QueuedRemovalEmail) {
	if s.emailService == nil {
		return
	}
	for _, q := range queued {
		if err := s.emailService.SendHouseholdMemberRemoved(ctx, q.ToEmail, q.HouseholdName, q.PurgeDate); err != nil {
			slog.Default().Warn("household: failed to send member-removed email; removal already committed",
				"email", q.ToEmail, "household_name", q.HouseholdName, "error", err)
		}
	}
}

// OffboardExcessMembers implements the HouseholdService method of the same name — see
// the interface doc comment for the full contract.
func (s *householdService) OffboardExcessMembers(ctx context.Context, householdID string) ([]QueuedRemovalEmail, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var cerr error
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)

	// Household-row lock always taken first, per this codebase's lock-ordering
	// convention (see AcceptInvite/DeleteHousehold).
	household, cerr := hhRepo.LockHouseholdByID(ctx, householdID)
	if cerr != nil {
		return nil, cerr
	}

	owner, cerr := s.userRepo.GetUserByID(ctx, household.OwnerUserID)
	if cerr != nil {
		return nil, cerr
	}
	ownerTier, cerr := s.tierConfig.GetTier(domain.TierName(owner.ComputeEffectiveTier()))
	if cerr != nil {
		return nil, cerr
	}

	members, cerr := hhRepo.ListMembers(ctx, householdID) // active members, joined_at ASC, incl. owner
	if cerr != nil {
		return nil, cerr
	}
	nonOwner := nonOwnerMembers(members, household.OwnerUserID)

	excess := len(members) - ownerTier.Pricing.UsersIncluded
	var queued []QueuedRemovalEmail
	// Walk nonOwner from the tail (most-recently-joined, since it's ASC by joined_at) —
	// LIFO auto-offboarding per #1371.
	for i := len(nonOwner) - 1; i >= 0 && excess > 0; i-- {
		email, err := s.removeMemberTx(ctx, tx, household, household.OwnerUserID, nonOwner[i].UserID)
		if err != nil {
			return nil, err
		}
		if email != nil {
			queued = append(queued, *email)
		}
		excess--
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return nil, cerr
	}
	for range queued {
		s.capture(household.OwnerUserID, phclient.EventHouseholdMemberAutoOffboarded)
	}
	s.sendRemovalEmails(ctx, queued)
	return queued, nil
}

// PurgeRemovedMembership implements the HouseholdService method of the same name — see
// the interface doc comment for the full contract.
func (s *householdService) PurgeRemovedMembership(ctx context.Context, householdID, membershipID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	var cerr error
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)
	shareRepo := repository.NewHouseholdShareRepository(tx)
	entRepo := repository.NewEntitlementRepository(tx)

	// Household-row lock always taken first, per this codebase's lock-ordering
	// convention (see AcceptInvite/DeleteHousehold/OffboardExcessMembers/ForceDisband).
	// This is the daily purge worker's sibling to AcceptInvite's re-invite-restore path
	// (which also locks the household first, then does its own row-level
	// GetRemovedMemberForUpdate before flipping status back to 'active') — taking the
	// same household lock here adds a real layer of defense-in-depth against the two
	// transactions racing, even though the row-level guards below (WHERE status='removed'
	// on the delete, plus the rejoin-race check) already prevent actual data corruption
	// either way.
	if _, cerr = hhRepo.LockHouseholdByID(ctx, householdID); cerr != nil {
		return cerr
	}

	_, userID, cerr := hhRepo.DeleteRemovedMember(ctx, membershipID)
	if cerr != nil {
		return cerr
	}

	// Guard against a rejoin race (Codex review finding): the deleted row only tells us
	// WHO the stale removal belonged to, not whether they're still gone. If the user has
	// since rejoined this same household (a new active membership row exists — from a
	// fresh AcceptInvite, with its own fresh shares/entitlement), the shares/entitlement
	// we're about to touch belong to that CURRENT membership, not the stale one just
	// deleted. Blindly cleaning up by (household_id, user_id) alone would wipe the
	// rejoined member's live data. Deleting the stale historical row above is always
	// safe regardless — only the destructive cleanup below needs this check.
	if _, cerr = hhRepo.GetActiveMember(ctx, householdID, userID); cerr == nil {
		return tx.Commit(ctx)
	}
	if !errors.Is(cerr, domain.ErrMemberNotFound) {
		return cerr
	}

	if cerr = shareRepo.DeleteSharesForUser(ctx, householdID, userID); cerr != nil {
		return cerr
	}
	// Scoped (not the broad ExpireActiveEntitlement) — a removed member may have since
	// started their own subscription; the scoped method can't accidentally close it out.
	if cerr = entRepo.ExpireActiveHouseholdEntitlement(ctx, userID, householdID); cerr != nil {
		return cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return cerr
	}
	s.capture(userID, phclient.EventHouseholdMembershipPurged)
	return nil
}

// ForceDisband implements the HouseholdService method of the same name — see the
// interface doc comment for the full contract.
func (s *householdService) ForceDisband(ctx context.Context, ownerUserID string) ([]QueuedRemovalEmail, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var cerr error
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)
	entRepo := repository.NewEntitlementRepository(tx)

	household, isOwner, found, cerr := hhRepo.GetHouseholdForUser(ctx, ownerUserID)
	if cerr != nil {
		return nil, cerr
	}
	if !found || !isOwner {
		cerr = domain.ErrNotHouseholdOwner
		return nil, cerr
	}
	// Household-row lock first, matching DeleteHousehold/AcceptInvite lock ordering.
	if _, cerr = hhRepo.LockHouseholdForUpdate(ctx, household.ID); cerr != nil {
		return nil, cerr
	}

	members, cerr := hhRepo.ListMembers(ctx, household.ID)
	if cerr != nil {
		return nil, cerr
	}
	var queued []QueuedRemovalEmail
	for _, m := range nonOwnerMembers(members, household.OwnerUserID) {
		email, err := s.removeMemberTx(ctx, tx, household, ownerUserID, m.UserID)
		if err != nil {
			return nil, err
		}
		if email != nil {
			queued = append(queued, *email)
		}
	}

	// Dissolve exactly like DeleteHousehold — composed directly (not via the public
	// DeleteHousehold method, which opens its own transaction and rejects disbanding
	// while active non-owner members remain) since every member was just force-removed
	// above within this same transaction.
	if cerr = hhRepo.DeleteHousehold(ctx, household.ID, ownerUserID); cerr != nil {
		return nil, cerr
	}
	if cerr = entRepo.ExpireActiveEntitlement(ctx, ownerUserID); cerr != nil {
		return nil, cerr
	}

	if cerr = tx.Commit(ctx); cerr != nil {
		return nil, cerr
	}
	s.capture(ownerUserID, phclient.EventHouseholdForceDisbanded)
	s.sendRemovalEmails(ctx, queued)
	return queued, nil
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
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
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
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
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
	// Unconditional rollback-on-defer; see Gap Analysis Round 5, GT-7.
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	inviteRepo := repository.NewHouseholdInviteRepository(tx, s.enc)
	hhRepo := repository.NewHouseholdRepository(tx)
	shareRepo := repository.NewHouseholdShareRepository(tx)
	entRepo := repository.NewEntitlementRepository(tx)

	// (0) Non-locking lookup of which household this token belongs to — deliberately
	// BEFORE any row lock. #1367 Gap Analysis Round 4B, GT-6: locking the invite row
	// first (as this used to) inverts DeleteHousehold's lock order (household, then —
	// via ON DELETE CASCADE — its invites), which can deadlock a concurrent accept
	// against a concurrent disband. Locking the household FIRST here, matching
	// CreateInvite/DeleteHousehold, makes the acquisition order consistent everywhere.
	// This lookup is NOT authoritative — never branch on its result beyond "which
	// household to lock"; the real validation happens after the invite is actually
	// locked in step (1)(2) below.
	householdID, cerr := inviteRepo.GetHouseholdIDByTokenHash(ctx, hashToken(rawToken))
	if cerr != nil {
		return nil, cerr
	}

	// (1) Lock household; owner must not be deleted/read-only; seat re-check.
	ownerUserID, cerr := hhRepo.LockHouseholdForUpdate(ctx, householdID)
	if cerr != nil {
		return nil, cerr
	}

	// (2) Lock invite (the authoritative, race-safe read); validate pending & not expired.
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

	// (4) Household is already locked (step 1). Also lock the OWNER's own user row before
	// reading their deletion/subscription status (Gap Analysis Round 5, GT-8): without
	// this, a concurrent Paddle webhook or the trial-expiry job could commit an
	// active->cancelled transition on the owner's row between this read and our commit,
	// letting a membership + household entitlement get inserted for an owner whose plan
	// just lapsed. Locking closes the race outright; ownerUserID is expected to differ from
	// callerUserID in the normal case (CreateInvite rejects self-invites by email at
	// creation time), so this ordinarily locks a different row than the caller-lock above.
	// Even in the theoretical edge case where they coincide (e.g. the owner's email later
	// changes to match a still-pending invite's invitee_email), re-acquiring the same row's
	// FOR UPDATE lock twice within one transaction is a safe, idempotent no-op in Postgres —
	// not a double-lock bug. Verified this introduces no new deadlock cycle: no other
	// household-write path (DeleteHousehold, CreateInvite, RevokeInvite,
	// RemoveMember, DeclineInvite) ever takes a FOR UPDATE lock on the owner's own users
	// row, so this can only ever put AcceptInvite in a one-directional wait against
	// unrelated single-lock writers (profile updates, Paddle webhooks) — never a cycle.
	owner, cerr := s.lockAndGetUser(ctx, tx, ownerUserID)
	if cerr != nil {
		return nil, cerr
	}
	if owner.DeletionRequestedAt != nil || domain.SubscriptionStatus(owner.SubscriptionStatus).IsReadOnlySubscription() {
		cerr = domain.ErrOwnerPlanInactive
		return nil, cerr
	}

	// Test-only synchronization seam (always nil in production): invoked immediately after
	// the owner-status decision, while this transaction (and its owner-row lock) is still
	// open. Exists solely so the real-DB regression test for this fix
	// (TestHouseholdServiceIntegration_AcceptInvite_OwnerStatusRace_NoStaleRead, #1367 Gap
	// Analysis Round 5, GT-8) can deterministically prove a concurrent owner-status write is
	// genuinely blocked at this point, rather than relying on incidental lock timing that
	// does not reliably discriminate a fixed implementation from a broken one.
	if s.acceptInviteAfterOwnerCheckHook != nil {
		s.acceptInviteAfterOwnerCheckHook()
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

	// (5) Restore-or-insert membership (#1371 Task 4 — re-invite within the 30-day
	// preservation window). If the caller has a 'removed' household_members row for THIS
	// household — they were removed less than purgeAfterDays ago and are now being
	// re-invited before Task 2's purge worker sweeps that row — reactivate the SAME row
	// instead of inserting a duplicate historical one: GetRemovedMemberForUpdate locks
	// the most recent removed row FOR UPDATE, then RestoreMember flips it back to
	// status='active'. If no removed row exists (first-ever join, or the purge worker
	// already deleted it), fall through to the original fresh-insert path, unchanged.
	// Either branch is still gated by the seat check above — a restored member counts as
	// a new active member against the seat cap exactly like a fresh join does.
	var member *domain.HouseholdMember
	removedMember, cerr := hhRepo.GetRemovedMemberForUpdate(ctx, inv.HouseholdID, callerUserID)
	switch {
	case cerr == nil:
		// Restore path: reactivate the existing row rather than inserting a new one.
		// RestoreMember already maps a since-joined-elsewhere conflict to
		// ErrAlreadyInHousehold/ErrAlreadyOwnsHousehold (same contract InsertMember
		// uses below) — surface it as-is rather than swallowing it. It also RETURNs
		// the freshly-written updated_at (Codex review, #1371 Task 4) so the
		// in-memory struct we return to the caller never leaks the pre-restore,
		// stale updated_at into the API response.
		updatedAt, cerr := hhRepo.RestoreMember(ctx, removedMember.ID)
		if cerr != nil {
			return nil, cerr
		}
		removedMember.Status = domain.HouseholdMemberStatusActive
		removedMember.RemovedAt = nil
		removedMember.UpdatedAt = updatedAt
		member = removedMember
	case errors.Is(cerr, domain.ErrMemberNotFound):
		// Fresh-insert path: unchanged from before Task 4.
		priorTier := caller.ComputeEffectiveTier()
		priorStatus := caller.SubscriptionStatus
		member = &domain.HouseholdMember{
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
	default:
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

// nonOwnerMembers filters members down to non-owner rows, preserving order. Shared by
// OffboardExcessMembers (which needs the filtered slice for LIFO reverse iteration) and
// ForceDisband (which just ranges over it) — #1371 Phase 7 foundation.
func nonOwnerMembers(members []domain.HouseholdMember, ownerUserID string) []domain.HouseholdMember {
	nonOwner := make([]domain.HouseholdMember, 0, len(members))
	for _, m := range members {
		if m.UserID != ownerUserID {
			nonOwner = append(nonOwner, m)
		}
	}
	return nonOwner
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
