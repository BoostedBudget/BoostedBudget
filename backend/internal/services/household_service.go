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
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
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
// be permanently removed on <date>") — meaningful for RemoveMember/OffboardExcessMembers,
// where the status='removed' row genuinely lingers until the purge worker deletes it.
//
// Disbanded is false for removeMemberTx's own zero-value default (RemoveMember/
// OffboardExcessMembers); ForceDisband flips it to true on every queued email it sends,
// since it deletes the entire household row (cascading household_members away
// immediately) in the SAME transaction as the removals — there is no lingering row left
// for PurgeDate to ever apply to. Round 4 Gap Analysis fix (#1371).
type QueuedRemovalEmail struct {
	ToEmail       string
	HouseholdName string
	PurgeDate     time.Time
	Disbanded     bool
}

// removalPurgeWindowDays is removalPurgeWindow expressed as a day-count — the single
// source of truth both for the time.Duration below and for the int day-count
// GetRemovedMemberForUpdate uses to enforce the same 30-day re-invite restore window
// (Gap Analysis Round 3, GT-3: without this enforcement, a delayed/paused Task 2 purge
// worker could let a 60+ day stale removal be silently restored via re-invite). Do not
// hardcode 30 anywhere else — reference this constant instead.
const removalPurgeWindowDays = 30

// removalPurgeWindow is how long a removed member's membership/shares/entitlement
// linger before PurgeRemovedMembership permanently deletes them — the window
// communicated in QueuedRemovalEmail.PurgeDate. Not specified by #1371 Task 0; chosen
// to match the task description's "now + 30 days" default. Revisit if product
// requirements narrow this once Task 2 (the purge worker) schedules real purge jobs.
const removalPurgeWindow = time.Duration(removalPurgeWindowDays) * 24 * time.Hour

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
	// already within its seat count, OR if the owner's subscription status — re-read
	// fresh under this method's own household-row lock — is read-only (Gap Analysis
	// Round 3, GT-2: closes a TOCTOU window where a caller's own owner read could be
	// stale by the time it calls this method). Safe for a background worker to call —
	// never nests a transaction. Returns the removal emails to send after commit. #1371 —
	// Phase 7 foundation; the decision of WHEN/how-many to call this with (grace
	// periods, over_capacity_since bookkeeping) is Task 6's responsibility, not this
	// method's.
	//
	// expectedOverCapacitySince is the over_capacity_since value the CALLER's own removal
	// decision was based on (e.g. household_over_capacity_check.go's autoOffboard, which
	// only calls this once its fresh re-read has determined the grace deadline computed
	// from that value has passed). This method re-verifies, immediately after taking its
	// own lock, that the CURRENT locked row's OverCapacitySince still matches — a
	// mismatch (including a nil current value) means a concurrent clear-then-re-flag
	// cycle (e.g. a subscription webhook) landed between the caller's decision and this
	// method's own LockHouseholdByID call, leaving a genuinely fresh (or cleared) anchor
	// under this lock that the caller's decision never saw. This closes a Round 9 Gap
	// Analysis finding (Codex adversarial review): without this check, the pre-fix
	// implementation silently ignored the fresh anchor, proceeding with LIFO removal
	// based on the caller's now-superseded decision. On a mismatch, this method commits
	// its (empty) transaction and returns (nil, domain.ErrOverCapacityAnchorMismatch) —
	// deliberately NOT the same "nil error, empty slice" shape as the read-only-owner
	// no-op below, because the CALLER must be able to tell the two apart: a read-only
	// owner means grace is moot and the caller may safely clear its over-capacity flag,
	// but an anchor mismatch means the household is STILL over capacity under a
	// different, legitimately fresh grace period that the caller must leave untouched
	// (third Codex adversarial pass — a follow-up bug found in autoOffboard's original,
	// unconditional post-call ClearOverCapacity). This method still does not own or
	// compute any grace-period logic itself — it only verifies a precondition the caller
	// already decided on still holds at the moment of the lock.
	OffboardExcessMembers(ctx context.Context, householdID string, expectedOverCapacitySince time.Time) ([]QueuedRemovalEmail, error)

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
	// SetProductAnalytics wires the consent-aware Gateway for generated product events
	// (currently the household entitlement projection). Nil-safe, like SetAnalytics.
	SetProductAnalytics(a phclient.ProductAnalytics)
	// SetMemberProfileResolver wires optional (nil-safe) Clerk name/avatar enrichment for
	// ListMembers. #1367 — Phase 3 Round 2 (GT-3).
	SetMemberProfileResolver(r MemberProfileResolver)

	// SetCacheInvalidation wires cache generation bumping for household writes that affect
	// users OTHER than the acting HTTP caller: a member removed by the owner, a joiner (and
	// the rest of the household) on invite acceptance, every member of a disbanded household,
	// and every member after an owner offboards excess seats. NewCacheInvalidationMiddleware
	// (Issue #1531 Task 10b-1) only bumps the ACTING user's (and their household's) generation
	// — it has no way to know a write also touches OTHER users' own cached analytics. This
	// closes that gap for every HouseholdService write path. gens == nil is a true no-op,
	// matching every other optional wiring setter in this codebase (SetAnalytics,
	// SetMemberProfileResolver, transaction_sync_service.go's SetCacheInvalidation). Issue
	// #1531 Task 10b-2b.
	SetCacheInvalidation(gens *cache.Generations)
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
	// productAnalytics is the consent-aware Gateway used for generated product events. Kept
	// separate from analytics (the legacy raw shape) so the existing household events are
	// untouched; nil-safe, so an unwired value makes every capture a no-op.
	productAnalytics phclient.ProductAnalytics
	memberProfiles   MemberProfileResolver

	// cacheGens mints/bumps generation tokens for household writes that affect users OTHER
	// than the acting HTTP caller (Issue #1531 Task 10b-2b) — nil when cache invalidation is
	// disabled/not wired; every bump helper below nil-guards on it.
	cacheGens *cache.Generations

	// acceptInviteAfterOwnerCheckHook is a test-only synchronization seam (see the call site
	// in AcceptInvite for why it exists). Always nil in production.
	acceptInviteAfterOwnerCheckHook func()

	// offboardExcessMembersAfterOwnerLockHook is a test-only synchronization seam, the
	// OffboardExcessMembers analog of acceptInviteAfterOwnerCheckHook above — see the call
	// site in OffboardExcessMembers for why it exists. Always nil in production.
	offboardExcessMembersAfterOwnerLockHook func()
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

// SetProductAnalytics wires the consent-aware Gateway used for generated product events.
// Nil-safe: an unset value makes every CaptureEvent a no-op, mirroring SetAnalytics.
func (s *householdService) SetProductAnalytics(a phclient.ProductAnalytics) {
	s.productAnalytics = a
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

// SetCacheInvalidation wires cache generation bumping. See the interface method's doc comment
// for why this exists and why gens is optional. Issue #1531 Task 10b-2b.
func (s *householdService) SetCacheInvalidation(gens *cache.Generations) {
	s.cacheGens = gens
}

// bumpCacheForUser invalidates a single user's cached analytics. Void by design, matching
// every other generation bump in this codebase (cache.Generations.Bump itself, the HTTP
// mutation middleware, transaction_sync_service.go's bumpCacheForSync): a bump failure must
// never fail an already-committed write, and Bump already logs+counts its own failures.
//
// Issue #1531 Final fixes: runs on context.WithoutCancel(ctx) plus a bounded WithTimeout,
// matching account_service.go's bumpCacheForDispossessedOwner, the HTTP mutation middleware,
// and the sync services' post-commit bump helpers. RemoveMember/AcceptInvite/ForceDisband/
// OffboardExcessMembers (and SetAccountShareVisibility, which shares these helpers) all call
// this AFTER their own write has committed — these are exactly the cross-user paths (Task
// 10b-2b) where a dropped bump invalidates a *non-actor's* cache, invisibly to the person who
// triggered the write, so a client disconnect or job cancellation landing in that window must
// not cancel it. WithoutCancel alone would leave the bump unbounded (it strips the parent's
// deadline too), hence the explicit WithTimeout. Issue #1531 Task 10b-2b.
func (s *householdService) bumpCacheForUser(ctx context.Context, userID string) {
	if s.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()
	s.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, "household_service")
}

// bumpCacheForUsers invalidates every user in userIDs. Callers pass a member list CAPTURED
// BEFORE the mutation when the mutation removes members (RemoveMember, OffboardExcessMembers,
// ForceDisband) — after a removal/disband commits, the membership rows are gone and there is
// no way to enumerate who to invalidate (mirrors Task 10b-1's identical precedent in the
// deletion path). Issue #1531 Task 10b-2b.
func (s *householdService) bumpCacheForUsers(ctx context.Context, userIDs []string) {
	for _, id := range userIDs {
		s.bumpCacheForUser(ctx, id)
	}
}

// bumpCacheForHousehold invalidates the household-scoped cache. Detaches from ctx the same way
// bumpCacheForUser does and for the same reason — see that doc comment. Issue #1531 Task 10b-2b.
func (s *householdService) bumpCacheForHousehold(ctx context.Context, householdID string) {
	if s.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()
	s.cacheGens.Bump(bumpCtx, cache.ScopeHousehold, householdID, "household_service")
}

// bumpCacheForCurrentMembers re-lists householdID's currently-active members (via
// s.householdRepo — the plain, POOL-bound field, NEVER a tx-scoped instance; always called
// AFTER the caller's own transaction has committed) and bumps every one of them. A true no-op
// when cacheGens is nil — this guard runs FIRST, before the extra ListMembers round-trip, so
// cache-disabled callers never pay for (or risk logging spurious errors from) a query whose
// result would only be discarded. A lookup failure is logged and swallowed, never propagated:
// the caller's write already committed.
//
// Issue #1531 Final fixes: the ListMembers lookup itself, not just the resulting bumps, runs on
// a detached context.WithoutCancel(ctx) + WithTimeout — otherwise a cancelled caller ctx fails
// the lookup FIRST, before bumpCacheForUsers/bumpCacheForUser get a chance to detach anything,
// and the bumps never happen at all. Issue #1531 Task 10b-2b.
func (s *householdService) bumpCacheForCurrentMembers(ctx context.Context, householdID string) {
	if s.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()
	members, err := s.householdRepo.ListMembers(bumpCtx, householdID)
	if err != nil {
		slog.Default().Error("cache invalidation: post-write member list lookup failed; "+
			"some members' household-scoped cache may serve stale data until TTL",
			"household_id", householdID, "error", err)
		return
	}
	s.bumpCacheForUsers(bumpCtx, memberUserIDs(members))
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

	// Household-row lock always taken first, per this codebase's lock-ordering
	// convention (see OffboardExcessMembers/ForceDisband/PurgeRemovedMembership) — Gap
	// Analysis Round 3, GT-1: without this, RemoveMember mutates household_members before
	// ever locking households (the grace-clear branch inside removeMemberTx below only
	// takes that lock conditionally, and only AFTER the member row is already updated),
	// which inverts the lock order every sibling method uses and creates a real deadlock
	// risk against a concurrent OffboardExcessMembers run (locks households first, then
	// the same member's household_members row). GetHouseholdForUser above is only a
	// non-locking lookup used to resolve which household actorUserID belongs to; this
	// re-fetch under FOR UPDATE is the authoritative, locked household struct used for
	// everything downstream. If the household was concurrently deleted between the two
	// reads (e.g. a racing ForceDisband), this surfaces as a legitimate
	// domain.ErrHouseholdNotFound rather than proceeding against a stale reference.
	household, cerr = hhRepo.LockHouseholdByID(ctx, household.ID)
	if cerr != nil {
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
	// Issue #1531 Task 10b-2b: bump the household scope, the actor, the just-removed target,
	// and every remaining member. Deliberately queried via s.householdRepo — the plain,
	// POOL-bound field, never the tx-scoped hhRepo local above — and only AFTER commit: this
	// keeps the read out of the transaction entirely (no new query for pgxmock/tx-based tests
	// to expect on the success path, and none at all on the auth-failure paths above, which
	// return before ever reaching this point). A post-commit ListMembers naturally excludes
	// targetUserID (already status='removed' by the just-committed write), so it is bumped
	// explicitly alongside actorUserID; both are harmless, cheap no-ops even when actorUserID
	// == targetUserID (self-leave) or when either is already present in the remaining list.
	s.bumpCacheForHousehold(ctx, household.ID)
	s.bumpCacheForUser(ctx, actorUserID)
	s.bumpCacheForUser(ctx, targetUserID)
	s.bumpCacheForCurrentMembers(ctx, household.ID)
	if email != nil {
		s.sendRemovalEmails(ctx, []QueuedRemovalEmail{*email})
	}
	return nil
}

// memberUserIDs projects a []domain.HouseholdMember to its UserID column, for cache-bump
// helpers that only need the ID list. Issue #1531 Task 10b-2b.
func memberUserIDs(members []domain.HouseholdMember) []string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.UserID
	}
	return ids
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
	// tx) against the owner's CURRENT effective-tier seat count, so an owner manually
	// removing members during grace gets immediate credit without waiting for the next
	// daily household_over_capacity_check worker run (Task 6, Part C).
	//
	// #1389: the owner is read via lockAndGetUser (FOR UPDATE), not a plain
	// userRepo.GetUserByID — mirroring Round 9's identical fix to OffboardExcessMembers's
	// own owner read (see OffboardExcessMembers's doc comment above its lockAndGetUser
	// call). This branch is reached by all 3 callers of removeMemberTx (RemoveMember,
	// ForceDisband, and OffboardExcessMembers's own removal loop), so locking it here once
	// makes every caller self-protected against the same stale-tier race Round 9 fixed,
	// rather than requiring each caller to lock the owner itself. For OffboardExcessMembers
	// specifically, the owner is already locked earlier in the same transaction — the FOR
	// UPDATE here is a harmless, reentrant no-op on the same tx, accepted as a small
	// redundant cost (an extra query per removed member) in exchange for removeMemberTx no
	// longer depending on caller discipline.
	if household.OverCapacitySince != nil {
		owner, err := s.lockAndGetUser(ctx, tx, household.OwnerUserID)
		if err != nil {
			return nil, err
		}
		ownerAccess, err := s.ownAccessInTransaction(ctx, tx, owner)
		if err != nil {
			return nil, err
		}
		ownerTier, err := s.tierConfig.GetTier(domain.TierName(ownerAccess.Tier))
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
		if err := s.emailService.SendHouseholdMemberRemoved(ctx, q.ToEmail, q.HouseholdName, q.PurgeDate, q.Disbanded); err != nil {
			slog.Default().Warn("household: failed to send member-removed email; removal already committed",
				"email", q.ToEmail, "household_name", q.HouseholdName, "error", err)
		}
	}
}

// OffboardExcessMembers implements the HouseholdService method of the same name — see
// the interface doc comment for the full contract.
func (s *householdService) OffboardExcessMembers(ctx context.Context, householdID string, expectedOverCapacitySince time.Time) ([]QueuedRemovalEmail, error) {
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

	// Round 9 Gap Analysis fix (Codex adversarial review): verify the anchor the caller's
	// own removal decision was based on still matches the CURRENT locked row, before doing
	// anything else — including before the owner-read-only check below. A concurrent
	// clear-then-re-flag cycle (e.g. a subscription webhook) can land between the caller's
	// own fresh read and this lock acquisition, leaving a genuinely fresh over_capacity_since
	// here that the caller's decision never saw. Unlike the read-only-owner case immediately
	// below, this is NOT reported the same way: return domain.ErrOverCapacityAnchorMismatch
	// (rather than a nil-error empty slice) so the caller can tell "grace is moot, safe to
	// clear" apart from "still over capacity under a different, fresh grace period — do NOT
	// clear it" (third Codex adversarial pass, household_over_capacity_check.go's autoOffboard).
	if household.OverCapacitySince == nil || !household.OverCapacitySince.Equal(expectedOverCapacitySince) {
		if cerr = tx.Commit(ctx); cerr != nil {
			return nil, cerr
		}
		return nil, domain.ErrOverCapacityAnchorMismatch
	}

	// Owner is read via lockAndGetUser (the same helper DeclineInvite/AcceptInvite already
	// use), NOT a plain userRepo.GetUserByID — a further Round 9 Gap Analysis fix (Codex
	// adversarial review, a later pass than the anchor-mismatch check above: while walking
	// the whole over-capacity lifecycle end-to-end to confirm that chain had converged
	// safe, this pass surfaced a DIFFERENT, pre-existing bug in this same function that
	// predates Round 9 entirely — Round 3-era code, untouched by any earlier fix this
	// round). Even with the household row already locked above, a plain unlocked read here
	// left a live race: a concurrent Paddle webhook could commit an owner tier UPGRADE
	// (e.g. household_2 -> household_4) in the window between this read and the actual
	// LIFO removal several lines below — removal that runs on THIS SAME transaction, still
	// holding the household lock, but with no lock at all on the user row in between. The
	// removal would then proceed using the now-stale, too-low household_2 seat limit,
	// incorrectly removing members who would actually fit under the just-committed,
	// current tier — a real path to incorrect member removal, not merely a stale-read
	// annoyance. lockAndGetUser closes it exactly like it already does for
	// DeclineInvite/AcceptInvite: the FOR UPDATE lock taken here is held for the remainder
	// of THIS transaction, so any concurrent writer to the owner's users row must wait
	// until this transaction commits or rolls back — the read below (and the seat-limit/
	// removal decision built on it) is therefore guaranteed fresh AND stays fresh for the
	// rest of this method's lifetime, not just at the instant of the read.
	owner, cerr := s.lockAndGetUser(ctx, tx, household.OwnerUserID)
	if cerr != nil {
		return nil, cerr
	}

	// Test-only synchronization seam (always nil in production): invoked immediately after
	// the owner is locked+read, while this transaction (and its owner-row lock) is still
	// open. Exists solely so the real-DB regression test for the fix above
	// (TestHouseholdServiceIntegration_OffboardExcessMembers_OwnerLockedDuringRemoval_ConcurrentTierUpgradeWaits)
	// can deterministically prove a concurrent owner-tier write is genuinely blocked at
	// this point, rather than relying on incidental lock timing that does not reliably
	// discriminate a fixed implementation from a broken one — mirroring
	// acceptInviteAfterOwnerCheckHook's identical rationale in AcceptInvite.
	if s.offboardExcessMembersAfterOwnerLockHook != nil {
		s.offboardExcessMembersAfterOwnerLockHook()
	}

	// Gap Analysis Round 3, GT-2: re-check the owner's subscription status FRESH, under
	// this method's own locks (the household row above, and now — per the fix just above —
	// the owner's own user row too), rather than trusting a caller's possibly-stale read.
	// household_over_capacity_check.go's processHousehold reads the owner once at the top
	// of a run, then may call this method (via autoOffboard) some time later in that SAME
	// run — if the owner's subscription lapses via a concurrent Paddle webhook in that
	// window, the caller's cached owner would still look active. A read-only owner who
	// ISN'T currently comped via an active manual_tier_override means the over-capacity
	// grace is moot: members already fall back to their own tier via the entitlement
	// resolver's owner-read-only branch (#1413 — that branch now reads
	// owner.ComputeEffectiveStatus(), same as this check, so the two can never disagree),
	// so there's nothing to LIFO-remove — skip straight to a normal commit instead of
	// computing a seat limit and removing anyone. This is a legitimate "nothing to do"
	// outcome, not an error; the caller (the daily worker's autoOffboard) already clears
	// the over-capacity flag unconditionally after any successful call here, regardless of
	// queue length.
	ownerAccess, cerr := s.ownAccessInTransaction(ctx, tx, owner)
	if cerr != nil {
		return nil, cerr
	}
	if domain.SubscriptionStatus(ownerAccess.Status).IsReadOnlySubscription() {
		if cerr = tx.Commit(ctx); cerr != nil {
			return nil, cerr
		}
		return []QueuedRemovalEmail{}, nil
	}

	ownerTier, cerr := s.tierConfig.GetTier(domain.TierName(ownerAccess.Tier))
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
	// Issue #1531 Task 10b-2b: bump every member captured BEFORE the LIFO removal loop above
	// (owner + every non-owner, including whoever just got offboarded) plus the household
	// scope itself. This runs unconditionally (even when queued is empty, i.e. this call was
	// a no-op) — bumpCacheForUsers/bumpCacheForHousehold are cheap no-ops when cacheGens is
	// nil, and bumping a household that had nothing removed is harmless. This closes the gap
	// left by the job-level bump in queue/jobs/household_over_capacity_check.go, which only
	// bumps ScopeHousehold — never the individually offboarded members' own ScopeUser.
	s.bumpCacheForHousehold(ctx, household.ID)
	s.bumpCacheForUsers(ctx, memberUserIDs(members))
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

	// #1569 T7 left this hard delete, and the AcceptInvite restore path that clears
	// removed_at (householdRepository.RestoreMember), deliberately in place — see the ruling
	// below for what DID change. Both destroy the fact and timing of a past removal, and they
	// have to move together: retaining a removal interval that this statement deletes 30 days
	// later buys nothing. Nothing reads household_members.removed_at outside this
	// removal/restore/purge machinery today, so there is no consumer to break or serve either
	// way; whoever changes it should decide whether an as-of membership read is actually
	// wanted first, and watch uq_household_members_active_user (global on user_id,
	// active-only) when making a re-invite insert a new row instead of resurrecting this one.
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

	// #1569 T7 — THE RULING ON THE 30-DAY PURGE, written here because an undocumented
	// status quo is the defect, not the behaviour.
	//
	// This used to be shareRepo.DeleteSharesForUser: a hard DELETE of every
	// household_account_shares row for the departed member. The defensible intent was
	// privacy — a removed member's grants should not linger in someone else's household
	// forever. That intent is real, and it is in genuine tension with #1569's thesis that
	// history is retained unless the user explicitly deletes it. It loses anyway, on an
	// asymmetry:
	//
	//   - The departing member's FINANCIAL DATA is untouched either way. It lives in their
	//     own user_accounts / accounts / transactions / balance_snapshots, owned by them,
	//     and deleting the grant row moves none of it.
	//   - What the grant row records is the OTHER direction: what the remaining members
	//     were legitimately shown, and between which two instants. That is the household's
	//     audit of its own past, not a copy of the leaver's data.
	//
	// So the delete did not protect the leaver; it destroyed the record of a disclosure
	// that had already happened, held by the party that received it. And it did so
	// silently: with the row gone, a point-in-time household read for a date INSIDE the
	// shared window has nothing to resolve against and reports that nothing was ever
	// shared — the one answer that is certainly false.
	//
	// The revocation, not the deletion, is what enforces the privacy interest, and it
	// already happened at removal time (removeMemberTx -> RevokeSharesForUser, same
	// transaction). revoked_at closes the interval, and every read is bounded by it:
	// nothing after the departure is visible, at any asOf, ever. Re-closing here is an
	// idempotent belt-and-braces for a grant that somehow survived the removal still open
	// — it cannot re-open one, and the rejoin guard above has already established that
	// this user has no live membership whose grants we could damage.
	//
	// If the leaver wants the row itself gone, that is GDPR erasure, and erasure still
	// destroys it: household_account_shares.user_account_id is ON DELETE CASCADE from
	// user_accounts, which DeleteUserCascade removes. Pinned by
	// TestHouseholdServiceIntegration_PurgeRemovedMembership_GDPRErasureStillDestroysGrants.
	//
	// TWO LIMITS OF THIS RULING, both real and both accepted:
	//
	//   1. A retained grant is created by exactly two paths — the accept fan-out's InsertShare
	//      and SetShareVisibility's interval INSERT — and BOTH require an active membership in
	//      this household (the fan-out runs inside AcceptInvite; SetAccountShareVisibility
	//      holds LockActiveMembershipForUser). So a closed grant always attests to a real past
	//      membership, which is what licenses the as-of reads admitting it without a current
	//      membership check. It is not licensed by "InsertShare is the only writer" — it no
	//      longer is.
	//   2. GetForHouseholdScopeActiveNow joins the as-of grant to LIVE `accounts` columns (name, mask,
	//      official_name, current_balance). Balances are safe — every consumer takes the number
	//      from an asOf-bounded balance_snapshots row, never from accounts.current_balance —
	//      but a departed member's CURRENT account name and mask are now reachable at past
	//      instants where the row previously did not appear at all. That is a widening of what
	//      is legible, not of which periods are visible, and it is the same identifying detail
	//      the household saw while the account was shared. It is also bounded by that read's
	//      own `a.status = 'active'` filter: a departed member's account has to still be LIVE
	//      today to be legible at all, so a disconnected or replaced one is already invisible.
	//      Closing the rest properly needs an as-of read of account metadata, which
	//      account_status_history does not carry.
	if cerr = shareRepo.RevokeSharesForUser(ctx, householdID, userID); cerr != nil {
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
	// Issue #1531 Task 10b-2b: bump EVERY member of the just-disbanded household — owner
	// included, since the household row (and their entitlement) is gone too — captured from
	// `members` (fetched before the removal loop above; the household_members rows are gone
	// now that the transaction committed). Household scope is bumped too, even though the row
	// itself is deleted, purely for hygiene/consistency with every other mutation path; it is
	// a cheap no-op either way.
	s.bumpCacheForHousehold(ctx, household.ID)
	s.bumpCacheForUsers(ctx, memberUserIDs(members))
	// Round 4 Gap Analysis fix: the household row (and its cascaded household_members
	// rows) is already gone by this point — mark every queued email as disbanded so
	// sendRemovalEmails sends the corrected copy (no future-cleanup reference) instead of
	// the individual-removal/auto-offboard copy's "cleaned up after 30 days" language,
	// which is factually wrong here.
	for i := range queued {
		queued[i].Disbanded = true
	}
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

	if s.revenueCatEnabled() {
		owner, cerr = s.lockAndGetUser(ctx, tx, ownerUserID)
		if cerr != nil {
			return nil, cerr
		}
	}
	ownerAccess, cerr := s.ownAccessInTransaction(ctx, tx, owner)
	if cerr != nil {
		return nil, cerr
	}
	if !domain.SubscriptionStatus(ownerAccess.Status).IsActiveSubscription() {
		return nil, domain.ErrOwnerPlanInactive
	}
	tier, cerr := s.tierConfig.GetTier(domain.TierName(ownerAccess.Tier))
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
	callerPaid, cerr := s.callerHasLiveBillingInTransaction(ctx, tx, caller)
	if cerr != nil {
		return nil, cerr
	}
	if callerPaid {
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
	// not a double-lock bug. Verified this introduces no new deadlock cycle: every
	// household-write path that locks a user's row always locks its household (or invite)
	// row FIRST in the same transaction — this method (household, then caller, then
	// owner), OffboardExcessMembers (household, then owner), and removeMemberTx's
	// grace-clear branch, reached by RemoveMember/ForceDisband/OffboardExcessMembers
	// (household already locked by the caller, then owner — #1389). DeclineInvite locks an
	// invite row then the caller's own user row, never a household row, so it can't be
	// part of a household-vs-user cycle either. No path locks a users row before a
	// households row, so this can only ever put AcceptInvite in a one-directional wait
	// against unrelated single-lock writers (profile updates, Paddle webhooks) — never a
	// cycle.
	//
	// Round-3 correction (Issue #1440 follow-up, Codex review, Finding 7): this claim was FALSE for
	// one path from the moment DeleteUserCascade's household-owner guard (Issue #1440 Critical
	// finding, round 2) shipped, until it was fixed: that method locked the user row FIRST (its
	// eligibility recheck), then only touched the household row implicitly via its cascading
	// `DELETE FROM users` — the exact inverted order this comment already warned against, and a
	// genuine, empirically-reproduced deadlock (SQLSTATE 40P01) against THIS method's own step (4)
	// above. profile_repository.go's DeleteUserCascade now locks any household the target owns
	// FIRST (`FOR NO KEY UPDATE`, before its own user-row eligibility recheck), restoring this
	// invariant — see that method's own doc comment for the fix and
	// TestDeleteUserCascade_Integration_LockOrderMatchesAcceptInvite_NoDeadlockWithConcurrentAcceptInvite
	// (profile_repository_household_lock_order_integration_test.go) for the empirical proof, both
	// that the pre-fix order deadlocked against this exact lock sequence and that the fix closes it.
	owner, cerr := s.lockAndGetUser(ctx, tx, ownerUserID)
	if cerr != nil {
		return nil, cerr
	}
	ownerAccess, cerr := s.ownAccessInTransaction(ctx, tx, owner)
	if cerr != nil {
		return nil, cerr
	}
	if owner.DeletionRequestedAt != nil || domain.SubscriptionStatus(ownerAccess.Status).IsReadOnlySubscription() {
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

	ownerTier, cerr := s.tierConfig.GetTier(domain.TierName(ownerAccess.Tier))
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
	removedMember, cerr := hhRepo.GetRemovedMemberForUpdate(ctx, inv.HouseholdID, callerUserID, removalPurgeWindowDays)
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
		// Issue #1413 (this task): a malformed manual_tier_override must not propagate garbage
		// into household_members.prior_subscription_tier, later read back to revert a member's
		// tier when they leave the household — effectiveTierName falls back to the raw tier
		// instead.
		callerAccess, accessErr := s.ownAccessInTransaction(ctx, tx, caller)
		if accessErr != nil {
			return nil, accessErr
		}
		priorTier := callerAccess.Tier
		priorStatus := callerAccess.Status
		if !s.revenueCatEnabled() {
			priorStatus = caller.SubscriptionStatus
		}
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
	if cerr = entRepo.CreateHouseholdEntitlement(ctx, callerUserID, string(ownerTier.Name), ownerAccess.Status, ownerUserID, inv.HouseholdID); cerr != nil {
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
	// The household entitlement projection was just materialized for the joiner: the owner's
	// resolved tier, the occupied seat count after the join, and the tier's included seat limit.
	// Emitted only after commit, so a rejected or rolled-back join emits nothing.
	s.captureHouseholdProjection(ctx, ownerUserID, string(ownerTier.Name), activeCount+1, ownerTier.Pricing.UsersIncluded)
	// Issue #1531 Task 10b-2b: bump the joiner, the owner, the household, and every existing
	// member — the joiner's own household context just appeared, and every existing member's
	// household-scoped analytics (e.g. combined spending) now include the new joiner's data.
	// Queried via s.householdRepo (the plain, POOL-bound field, never the tx-scoped hhRepo
	// local above) and only AFTER commit — read-your-writes still holds (this is a separate
	// connection reading already-committed data), and keeping this out of the transaction
	// means no new query for any validation-failure path above (which all return before
	// reaching this point) to unexpectedly trip on. callerUserID/ownerUserID are also bumped
	// explicitly in case either is ever absent from the list (e.g. a future read-model
	// change) — a redundant bump is harmless, a missed one is not.
	s.bumpCacheForHousehold(ctx, inv.HouseholdID)
	s.bumpCacheForUser(ctx, callerUserID)
	s.bumpCacheForUser(ctx, ownerUserID)
	s.bumpCacheForCurrentMembers(ctx, inv.HouseholdID)
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

// effectiveTierName returns owner's effective tier name, falling back to the raw SubscriptionTier
// when an active manual_tier_override names no tier configured in s.tierConfig (Issue #1413 —
// the tier axis of the malformed-override rule, this task). manual_tier_override is an
// unconstrained VARCHAR(50) with no CHECK constraint, so its value can be garbage (a legacy row,
// a direct SQL write, a write made while the tier config was nil); every call site in this file
// that previously passed owner.ComputeEffectiveTier() straight into s.tierConfig.GetTier() would
// error on such a value — this is the single seam all of them now share, using this service's
// own tierConfig field directly (mirroring PaddleCheckoutHandler.isConfiguredOverrideTier's
// identical "handler's own tierConfig" convention) rather than a third validator-func mechanism.
func (s *householdService) effectiveTierName(owner *domain.User) string {
	tier, overrideValue, hasOverride := owner.EffectiveTierWithActiveOverride()
	if hasOverride && s.tierConfig != nil {
		if _, err := s.tierConfig.GetTier(domain.TierName(overrideValue)); err != nil {
			slog.Warn("rejecting manual tier override on tier axis: value does not name a configured tier",
				"user_id", owner.ID, "manual_tier_override", overrideValue)
			return owner.SubscriptionTier
		}
	}
	return tier
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
//
// #1413 PinnedRaw: the status switch below deliberately reads the RAW user.SubscriptionStatus,
// not ComputeEffectiveStatus(). This predicate is about Paddle BILLING truth (refund/double-charge
// risk), not access — a comped user's forced "active" effective status must not block someone
// whose real Paddle subscription is genuinely cancelled/inactive. See
// TestCallerHasLivePaddleSubscription_CompedUser_WithPaddleSub_RawCancelled_PinnedRaw for the pin
// (TestCallerHasLivePaddleSubscription_CompedUser_NoPaddleBillingRelationship_JoinsFreely covers
// the nil-PaddleSubscriptionID short-circuit above, which never reaches this switch at all).
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
