package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// dashboardSFDo runs fn under sf, coalescing concurrent callers that share key onto one execution.
//
// #1569 round 11 item 1: `/dashboard/balance-summary` and `/dashboard/net-worth` are separate,
// uncached HTTP endpoints that a single dashboard page load fires together, and in personal (and
// household-current) scope they read byte-identical repository arguments — same userID, same
// cutoff, same scope. singleflight.Group.Do coalesces the genuinely CONCURRENT overlap between the
// two requests onto one repository call; it caches nothing past that window, so it introduces no
// new invalidation surface (a prior reviewer specifically warned that caching account/liveness maps
// is unattractive for exactly that reason — this is deliberately not a cache).
//
// #1569 round 11 fix round 1, item 2 — LEADER-CONTEXT CANCELLATION. Only ONE goroutine's ctx
// actually reaches fn per in-flight key: whichever caller's Do call first claims the key (the
// "leader") runs fn(ctx) under ITS OWN context, and every other concurrent caller for that key (a
// "follower") just waits on the leader's result. Because every key this package builds embeds
// userID (see each sf* method), leader and follower are always the SAME user's two concurrent
// widget requests, never a different user's — but within that they are still independent: an
// AbortController per widget, a component unmount, or a tab navigation racing one fetch can cancel
// the LEADER's context while the FOLLOWER's is perfectly healthy, and naively sharing the leader's
// result would hand that follower a spurious context.Canceled for a request that would otherwise
// have succeeded.
//
// Fixed WITHOUT detaching fn's context. A fully-detached context.WithoutCancel+WithTimeout, the
// shape internal/cache.Cache.GetOrLoad uses for the identical class of problem, was tried first and
// reverted: dashboard_service_test.go's mocks assert on the EXACT incoming ctx (e.g.
// `mockRepo.On("GetSnapshotsAsOfForUser", ctx, ...)` with a literal `ctx := context.Background()`),
// and a detached context is a different value by that comparison, so every such assertion panicked
// with "Unexpected Method Call." Rewriting those sites to `mock.Anything` was the alternative and
// was rejected here: those exact-ctx assertions are the only thing in that test file pinning that
// the real, caller-supplied context reaches the repository at all — a behavioural property, not
// merely an implementation detail, and not worth discarding for a fix with a narrower alternative.
//
// The narrower alternative: if the shared call's error is EXACTLY the shape a context abandonment
// produces (context.Canceled or context.DeadlineExceeded) AND this caller's OWN context is NOT
// done, the error cannot be this caller's — by construction, it can only have come from whichever
// goroutine's context actually executed fn (the leader). In that specific, narrow case, this caller
// retries fn directly ONCE, under its own still-healthy context, bypassing the singleflight group
// entirely — so there is no risk of joining another in-flight call and inheriting the same problem
// a second time. A caller whose own context IS done gets an honest reflection of that (the shared
// error, or its own on the direct-retry path) exactly as before this fix.
func dashboardSFDo[T any](sf *singleflight.Group, ctx context.Context, key string, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	v, err, _ := sf.Do(key, func() (any, error) {
		return fn(ctx)
	})
	if err != nil {
		if ctx.Err() == nil && isContextAbandonmentErr(err) {
			// This caller's context is healthy but the SHARED error is exactly the shape a
			// cancellation/timeout produces — it belongs to the leader, not to us. Retry solo.
			return fn(ctx)
		}
		return zero, err
	}
	return v.(T), nil
}

// isContextAbandonmentErr reports whether err is EXACTLY the shape a context being cancelled or
// timing out produces (via errors.Is, so a wrapped context.Canceled/DeadlineExceeded still
// matches) — never a coincidental data error that merely mentions cancellation in its text. Used
// only to decide whether a shared singleflight error could belong to a DIFFERENT caller's context
// than this one's (dashboardSFDo's doc comment, item 2).
func isContextAbandonmentErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// householdScopeKeyFragment renders scope's household ID and member set into a singleflight key
// fragment, so two requests naming the same household but a DIFFERENT member set can never
// collapse onto one coalesced result.
//
// #1569 round 11 fix round 1, item 1: this was originally keyed on (userID, householdID, cutoff)
// alone, which is unsound, not merely imprecise: household_service.go's ResolveScope re-derives
// MemberUserIDs FRESH FROM THE DATABASE on every request (it is not stable per (userID,
// householdID) — a membership change between two concurrent requests changes it), and
// analytics_repository.go's householdScopeArgs binds MemberUserIDs as a literal SQL parameter into
// every household CTE these three sf* methods' underlying repository calls use — the result
// genuinely depends on the member set, not just the household ID. Two concurrent requests carrying
// the same household ID but different member sets (a membership change racing a read) would have
// keyed identically and shared the WRONG member's result. Mirrors
// internal/cache.EncodeHouseholdScope's identical StringSetSorted precedent, which already folds
// MemberUserIDs into its Redis arg hash for exactly this reason, one layer over.
func householdScopeKeyFragment(scope *domain.HouseholdScope) string {
	seen := make(map[string]struct{}, len(scope.MemberUserIDs))
	members := make([]string, 0, len(scope.MemberUserIDs))
	for _, id := range scope.MemberUserIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		members = append(members, id)
	}
	sort.Strings(members)
	return scope.HouseholdID + ":" + strings.Join(members, ",")
}

// snapshotChainKey returns the preferred lookup key for a balance snapshot:
// the stable user_account_id if present, otherwise the raw account_id.
// Used throughout dashboard service to resolve replaced accounts (Issue #1109).
func snapshotChainKey(s domain.BalanceSnapshot) string {
	if s.UserAccountID != nil && *s.UserAccountID != "" {
		return *s.UserAccountID
	}
	return s.AccountID
}

// accountChainKey returns the canonical identity of the LOGICAL account (the re-link chain) an
// accounts row belongs to: its stable user_account_id if it has one, else its own id.
//
// ⚠ #1569 T10.2: THIS IS NO LONGER A DEDUP KEY, and must not become one again. Two maskless
// accounts at one institution are FORCED to share a user_accounts row (migration 000100), so
// collapsing to one row per chain key DELETES one live account's balance from every total — the
// defect T10.2 exists to remove. Its one remaining use is supersededChainMembers, which GROUPS by
// it and then decides within the group using each member's status; grouping is safe, collapsing is
// not. Everything below documents the older question — how a legacy NULL-user_account_id snapshot
// is recognised as the same logical account as its post-relink sibling — which is still live, and
// is now answered by that status test rather than by a seen-set.
//
// The historical text follows, because its dead ends are still the dead ends.
//
// Dedup keys used to come from here, not from snapshotChainKey, once a snapshot had been resolved to
// an account. balance_snapshots.user_account_id is NULLABLE, its backfill was one-shot
// (migrations/000038) and the FK is ON DELETE SET NULL, so one chain can legitimately produce a
// legacy row keyed on a raw (already replaced) account_id AND a post-relink sibling keyed on the
// user_account_id. Those two snapshot keys differ, so a seen-set built from snapshotChainKey does
// not collapse them and the chain is counted twice in the baseline while the current loop correctly
// counts it once — measured: a true $500 → $700 move (+40%) reported as −30%. Both snapshots resolve
// to accounts rows that share one user_account_id, which is why the resolved account's key collapses
// them and the snapshot's own key cannot. Issue #1537.
// ⚠ RESIDUAL LIMIT (#1537 rounds 13, 14). The legacy-row collapse this enables depends on the
// INACTIVE chain member's accounts.user_account_id being non-NULL. That column is nullable
// (migrations/000033_add_accounts_user_account_and_status.up.sql:24 adds it with no NOT NULL), and
// the NULL is REACHABLE, not a theoretical edge: when userAccountSvc.MatchOrCreate errors during
// linking, account_service.go:543-550 logs the error and DELIBERATELY continues without setting
// account.UserAccountID ("Continue without user_account_id - transactions can still be synced"), so
// an accounts row is persisted with a NULL stable id. transaction_sync_service.go:1202 then copies
// account.UserAccountID straight into the snapshot, propagating that NULL into balance_snapshots.
// When the user later re-links, account_service.go:510 marks the old row 'replaced' while its
// successor carries a real user_account_id — so the two rows produce DIFFERENT canonical keys here,
// the seen-set in baselineBalanceForComparison does not collapse them, and the chain is counted
// TWICE in the baseline.
//
// The consequence is a WRONG NUMBER, not a cosmetic gap: the baseline is overstated, so both
// ChangeAmount and ChangePercent are wrong, and a real increase can render as a decrease the same
// way the round-13 case above did.
//
// ⚠ accounts.replaced_by_id CANNOT fix this — do not reach for it. The column exists (migration
// 000033 adds `replaced_by_id UUID REFERENCES accounts(id)`) and domain.Account.ReplacedByID scans
// it, so it looks like the obvious chain pointer. But NO production path ever writes it non-NULL:
// its only writer is accountRepository.UpdateStatus, whose sole production caller
// (account_service.go:510) passes a literal nil, and the only other production statement touching
// the column (profile_repository.go:1709) sets it to NULL. Following it from a replaced account
// resolves to nil 100% of the time. It is linked_items.replaced_by_id that is genuinely populated
// (account_service.go:492); the two same-named columns are easy to conflate, and conflating them
// has already cost one investigation. See the note on domain.Account.ReplacedByID.
//
// The two-hop route that DOES have live data — replaced account → its linked_item →
// linked_items.replaced_by_id → the new item → its accounts — then still has to decide WHICH
// successor account corresponds to the replaced one, i.e. match on mask/type. That is precisely the
// matching MatchOrCreate had just failed at in order to create the NULL, so it is unreliable in
// exactly the case it would need to handle. Fixing the write side instead (passing a real
// replacedByID at account_service.go:510) additionally requires reordering account linking, because
// the old accounts are marked 'replaced' at :510 BEFORE the new accounts are converted and matched
// at :520+, so the successor id is not known yet at that point.
//
// This function therefore targets the realistic case — the balance_snapshots backfill was one-shot
// and that FK is ON DELETE SET NULL — rather than every case. The gap is recorded here, with its
// dead ends, rather than left for someone to rediscover.
func accountChainKey(a domain.Account) string {
	if a.UserAccountID != nil && *a.UserAccountID != "" {
		return *a.UserAccountID
	}
	return a.ID
}

// buildChainAwareAccountMap builds a map keyed by both account.ID and account.UserAccountID
// so that snapshots carrying a replaced account_id still resolve via the chain key (Issue #1109).
// baselineBalanceForComparison sums the 30-day-ago snapshots for the period comparison, applying
// the SAME AccountTypes filter the current total applies, over a DELIBERATELY DIFFERENT account
// universe.
//
// Issue #1537 round 9 fixed the filter half. Both callers previously summed every snapshot they were
// handed, while their current-balance loops applied a chain-aware accountMap lookup AND the
// AccountTypes filter. With a type filter active the baseline therefore included accounts the current
// total excluded, and ChangeAmount/ChangePercent were arbitrary — a measured +50% was reported as
// +150%.
//
// Issue #1537 round 10 fixed the universe half, which round 9 broke. Round 9 applied the CURRENT
// account map to the baseline as well, and that map comes from an active-only read
// (GetByUserID: `status = 'active'`; GetForHouseholdScopeActiveNow: `a.status = 'active'`, regardless of its
// asOf) while the snapshot reads filter on user_id/household membership alone. A snapshot whose
// account has since gone 'replaced' or 'expired' with no active chain successor therefore had no map
// entry and was dropped: a $1,000 account disconnected 30 days ago produced baseline $0 (or, with
// another account still active, an equal current and baseline), and the comparison reported "no
// change" for what was really a large decrease.
//
// THE TWO SIDES USE DIFFERENT ACCOUNT UNIVERSES ON PURPOSE. Net worth history includes historical
// balances, because a snapshot is a fact about a point in time; the CURRENT total includes only
// linked/active accounts. That asymmetry is what makes a disconnection surface as a real decrease,
// and it is why the field is PreviousBalance rather than "previous balance of the accounts that
// happen to still be active". historicalAccounts supplies the since-inactive accounts — used ONLY to
// learn an account's type so the AccountTypes filter can still be applied to it; the balance itself
// always comes from the snapshot row.
//
// activeAccounts is consulted first — pinned by
// TestBaselineBalanceForComparison_ActiveAccountMapWinsOverHistorical, because swapping the two
// maps used to leave the whole internal/services package green. Note what that precedence actually
// buys: it is PER-KEY, not
// per-chain. A snapshot carrying a user_account_id hits the active member through that key, as
// intended. A snapshot with a NIL user_account_id keys on its raw (possibly already-replaced)
// account_id, which the active-only map does not contain — so it falls through and resolves to the
// INACTIVE chain member instead. For account.Type that is harmless, because chain members share a
// type; and it is now load-bearing for DEDUP, because the inactive member still carries the chain's
// user_account_id, which is the only way a legacy NULL-user_account_id snapshot can be recognised as
// the same logical account as its post-relink sibling (see accountChainKey). Whichever member
// resolves, the canonical key is the same. The fallback fingerprint is literally
// institution_id|mask|type|subtype (domain.Account.FallbackFingerprintBlindIdx), so a differing type
// would be a different fingerprint and hence a different chain; the persistent_account_id path
// identifies one physical account at one institution. The earlier wording — "so a re-linked chain
// keeps resolving to its active member" — overstated this, and is corrected rather than deleted
// because anyone adding a NAME, ID, or status-dependent read to this lookup would be relying on the
// overstatement and would be wrong.
//
// dedupChains reflects a real difference between the two callers, not an oversight: the personal path
// can receive several snapshots per logical account on a re-link day (it sorts DESC first and keeps
// the latest per chain — pinned by TestGetBalanceSummary_PeriodComparison_DedupKeepsLatestInChain,
// since the older dedup test gave both rows the same balance and a zero SnapshotAt and so could not
// see which row won), while the household path is already one row per account: its snapshot read
// JOINs on user_account_id, so it never sees the legacy NULL-key rows accountChainKey exists for.
//
// ⚠ The comment that used to live here asserted the single-map approach as a deliberate "KNOWN
// APPROXIMATION". It was wrong. Stated plainly because this exact claim has now been wrong twice in
// opposite directions — first "the baseline needs no map at all", then "one now-shaped map serves
// both sides" — and each time the wrong version was written down as a justification rather than a
// question. Two universes are needed; anything that collapses them re-breaks one half.
func baselineBalanceForComparison(
	snapshots []domain.BalanceSnapshot,
	activeAccounts map[string]domain.Account,
	historicalAccounts map[string]domain.Account,
	accountTypes []string,
	dedupChains bool,
) float64 {
	var total float64
	seenAccounts := make(map[string]bool)
	superseded := supersededSnapshotIndexes(snapshots, activeAccounts, historicalAccounts)
	for i, snapshot := range snapshots {
		account, exists := snapshotAccount(snapshot, activeAccounts, historicalAccounts)
		if !exists {
			continue
		}
		if dedupChains && superseded[i] {
			continue
		}
		// Dedup AFTER resolution, on the resolved account's OWN id.
		//
		// ⚠ #1569 T10.2 CHANGED THIS KEY FROM accountChainKey, and the direction of the old error
		// is the opposite of the one it was written to fix. Keying on the CHAIN collapsed two
		// maskless accounts — which share a user_account_id by schema (migration 000100) — into one
		// baseline row while the current-period loop above counted both. Current 500 against
		// baseline 400 is not a halving; it is a FABRICATED +25% on a balance that did not move.
		//
		// What the chain key used to buy is now bought upstream and better: the repository read
		// returns one row per ACCOUNT and removes a predecessor only when a RECORDED succession
		// pointer names its successor (accountChainSupersessionCTE), which a maskless pair can
		// never satisfy and a genuine re-link always does. This seen-set is the remaining safety
		// net against a duplicate row for one account; the caller sorted DESC, so the survivor is
		// the latest.
		if dedupChains {
			if seenAccounts[account.ID] {
				continue
			}
			seenAccounts[account.ID] = true
		}
		if !accountTypeMatchesFilter(account.Type, accountTypes) {
			continue
		}
		if snapshot.CurrentBalance != nil {
			total += *snapshot.CurrentBalance
		}
	}
	return total
}

// accountTypeMatchesFilter reports whether an account passes a DashboardFilters.AccountTypes filter.
// An empty or nil filter matches every type, which is what "no filter requested" means at every
// call site.
//
// This exists as one named predicate because the filter axis has been missed once per copy. A
// mechanical audit of this file's four period-comparison baselines found the current-period loop
// applied the filter in 4/4 cases while the baseline applied it in only 2/4: round 9 fixed the two
// balance-summary baselines by threading accountTypes into baselineBalanceForComparison, and both
// net-worth baselines were left summing every type against a filtered current total — a real +25%
// change reported as +150%. Inlining the predicate a third and fourth time is how that happens
// again, so the net-worth baselines call this instead.
func accountTypeMatchesFilter(accountType string, accountTypes []string) bool {
	if len(accountTypes) == 0 {
		return true
	}
	for _, filterType := range accountTypes {
		if accountType == filterType {
			return true
		}
	}
	return false
}

// hasSnapshotOutsideAccountMap reports whether any snapshot's OWN accounts row is absent from
// accountMap. It gates the extra all-statuses account read behind the cases that need it.
//
// ⚠ #1569 T10-FIX WIDENED THIS FROM THE CHAIN KEY TO THE OWN ROW, and the difference is a maskless
// pair. Asking about the chain key answered "present" for a CLOSED sibling purely because its LIVE
// twin occupies that key — so the as-of read never ran, the closed sibling never entered the
// baseline universe, and the baseline lost its contribution while the current total kept it under
// the twin. Both legs then agreed on a number neither of them should have produced. Asking about
// the snapshot's own row asks the question the caller actually has: can this snapshot be resolved
// to the account it was written for?
//
// The cost is one extra account query for a re-linked chain, whose predecessor row is likewise not
// in the active-only map. That is the correct trade: the predecessor's TYPE is what the baseline's
// AccountTypes filter is applied to, and inferring it from the successor is the same guess this
// whole family of defects is made of.
func hasSnapshotOutsideAccountMap(snapshots []domain.BalanceSnapshot, accountMap map[string]domain.Account) bool {
	for _, snapshot := range snapshots {
		if _, exists := snapshotOwnAccount(snapshot, accountMap); !exists {
			return true
		}
	}
	return false
}

// asOfIsHistorical reports whether asOfDate names a day strictly BEFORE today (UTC).
//
// This is the seam of the whole #1569 task-21 C2 fix, and it cuts in both directions. For a past
// date the account universe must be resolved from recorded liveness at that date, or a
// since-disconnected account's snapshot is dropped from the figure the response is labelled with.
// For today it must NOT be: `GetForHouseholdScopeLiveAsOf`'s doc comment states the reason
// directly — passing asOf=now readmits an account removed moments ago — and the product rule
// (task 20) is that CURRENT totals include only linked/active accounts.
//
// asOfDate has already been Truncate(24*time.Hour)'d by every caller, so this is a day comparison,
// not an instant one. A FUTURE date is deliberately not historical: it is a current-figure question
// wearing a different label.
func asOfIsHistorical(asOfDate time.Time) bool {
	return asOfDate.Before(domain.CalendarDayUTC(time.Now()))
}

// mergeMissingAccounts copies entries from src into dst for keys dst does not already hold.
//
// ADD-ONLY. The as-of universe AUGMENTS the active-now one; it never replaces it. The only rows
// this can add are accounts that are NOT active today, WERE live at the requested date, and have a
// snapshot at or before it — exactly the set that was being dropped, and the only set the fix is
// about.
//
// ⚠ THE DIRECTION CHANGES A TOTAL, not just a label. An earlier revision of this comment claimed
// the two directions differ only in TYPE attribution and therefore could not move a number. That
// was wrong. The map gates INCLUSION as well as decoration: `filters.AccountTypes` is applied to
// `account.Type` AFTER this lookup, so for a chain that re-linked after the requested date — the
// only case where the two maps disagree, the current map holding the successor and the as-of map
// the predecessor — a differing type decides whether that chain's money is counted at all. Pinned,
// with the disconnected second chain needed to reach the case, by
// TestDashboard_PastAsOfDate_MergeDirection_DecidesInclusionUnderATypeFilter: 1300.00 add-only
// versus 1000.00 overwriting.
//
// Add-only is chosen because it is the precedence the baseline path already applies (active map
// first, historical as fallback — baselineBalanceForComparison, and
// TestDashboard_AccountOpenedAfterTheCutoff_IsAbsentFromTheBaseline_Integration, which asserts that
// ordering explicitly). Having the current leg resolve a chain differently from the baseline leg
// of the same response is a class of bug this file has already been bitten by twice (#1369 G1,
// #1537 round 10).
//
// The cost, stated rather than left to be found: a historical figure is attributed to the
// SUCCESSOR's type, id and name. Preferring the member that was live THEN is arguably more honest
// for a point-in-time read — but it would have to move the baseline leg with it, or the two halves
// of one response would disagree about one chain, which is the exact failure both prior bugs were.
// That is a separate change and deliberately not made here.
func mergeMissingAccounts(dst, src map[string]domain.Account) {
	for key, account := range src {
		if _, exists := dst[key]; !exists {
			dst[key] = account
		}
	}
}

// ⚠ #1569 T10.2: THE CHAIN-KEY ENTRY IS A COLLISION, AND IT IS NOW RESOLVED DELIBERATELY.
//
// A user_account_id can name SEVERAL live accounts — two maskless accounts at one institution are
// FORCED to share one (migration 000100's NULLS NOT DISTINCT unique index), so this is an ordinary
// Plaid shape and not an edge case. Only one of them can occupy m[user_account_id], and until this
// task the winner was whichever the range loop wrote LAST, i.e. slice order. That is not a
// tie-break, it is an absence of one: the same two accounts in the other order gave a read a
// different NAME and a different TYPE, and TYPE decides whether an AccountTypes filter counts the
// money at all.
//
// The rule: a LIVE member beats a terminal one (the chain-key lookup exists to reach the account
// that is current), then the lowest id, which is arbitrary but stable. Pinned by
// TestBuildChainAwareAccountMap_MasklessPairEntryIsDeterministic.
//
// ⚠ The per-ID entries are untouched and are what callers should prefer — see snapshotAccount.
func buildChainAwareAccountMap(accounts []domain.Account) map[string]domain.Account {
	m := make(map[string]domain.Account, len(accounts)*2)
	for _, account := range accounts {
		m[account.ID] = account
	}
	for _, account := range accounts {
		if account.UserAccountID == nil || *account.UserAccountID == "" {
			continue
		}
		key := *account.UserAccountID
		if incumbent, exists := m[key]; exists && !chainRepresentativeBeats(account, incumbent) {
			continue
		}
		m[key] = account
	}
	return m
}

// chainRepresentativeBeats reports whether candidate should replace incumbent as the account a
// chain key resolves to. Live first, then lowest id. See buildChainAwareAccountMap.
func chainRepresentativeBeats(candidate, incumbent domain.Account) bool {
	candLive := candidate.Status == domain.AccountStatusActive
	incLive := incumbent.Status == domain.AccountStatusActive
	if candLive != incLive {
		return candLive
	}
	return candidate.ID < incumbent.ID
}

// supersededSnapshotIndexes returns the SLICE INDEXES of SNAPSHOTS that must NOT contribute to a total because
// the money they carry is already on the books under a live account.
//
// ⚠ IT IS KEYED BY THE SNAPSHOT'S POSITION IN THE SLICE, NOT BY ACCOUNT, and not by snapshot id
// either. Not by account because both rules below concern a snapshot that snapshotAccount resolves
// to an account OTHER than the one it was written for, so an account-keyed answer named the wrong
// row — the successor — and the callers, which skipped on the RESOLVED account's id, would have
// dropped the successor's own snapshot along with it or (as shipped) dropped neither. Not by
// snapshot id because an empty or duplicated id would collapse several rows onto one key and
// suppress every one of them; the index cannot collide. Both rules below concern a
// snapshot that snapshotAccount resolves to an account OTHER than the one it was written for, so an
// account-keyed answer named the wrong row — the successor — and the callers, which skipped on the
// RESOLVED account's id, would have dropped the successor's own snapshot along with it or (as
// shipped) dropped neither.
//
// ⚠ WHY A SECOND RULE EXISTS AT ALL, when the repository already applies the succession one. They
// answer different questions with different evidence, and neither subsumes the other:
//
//   - The repository rule reads RECORDED SUCCESSION (linked_items.replaced_by_id / accounts
//     .replaced_by_id) and deliberately ignores current status, because a per-DAY history read must
//     not re-judge a past day by what the accounts look like today.
//   - This rule reads CURRENT STATUS, and can only be applied where the answer is about a single
//     instant and the caller already holds the accounts. It closes the repository rule's known
//     fail-open window: a re-link whose pointer write failed leaves the predecessor 'replaced' with
//     no successor pointer anywhere, and nothing downstream can see the succession — but the
//     PREDECESSOR'S OWN STATUS still says it was retired, and 'replaced' has exactly one meaning.
//
// It is what keeps TestGetBalanceSummary_PeriodComparison_LegacyNullUserAccountIDDoesNotDoubleCount
// green after #1569 T10.2 replaced the chain-key seen-set, and it is precisely NOT a chain-key
// collapse: two maskless accounts are BOTH 'active', so neither is superseded by the other and both
// count. That is the whole distinction the task turns on, expressed in the one piece of evidence
// available at this layer.
//
// ⚠ #1569 T10-FIX narrowed the first rule from "not active" to "'replaced'", and added the second.
// "Not active" was wrong in a way that MOVED MONEY: it treated a maskless sibling the user simply
// CLOSED as a retired predecessor. Round 10 never observed it because snapshotAccount resolved that
// closed sibling to its live twin before this function saw it, so the sibling never presented as
// terminal here at all — and its balance was added a second time under the twin's id and TYPE
// instead. 'replaced' is written by the re-link and the CreateBatch backstop and by nothing else;
// 'removed'/'expired' mean closed, and a closed account is nobody's predecessor.
//
// THE SECOND RULE — the stand-in withdrawal — is what actually stops that double count, and it is
// stated in terms of the SET rather than of a status because the closed sibling has no accounts row
// in the CURRENT universe at all (GetByUserID is active-only), so there is no status to read. A
// snapshot with no own row is stood in for by the chain's live member (#1109, so a re-linked chain
// whose successor has not synced yet keeps reporting its last known balance); the stand-in is
// withdrawn the moment that live member contributes a snapshot of its own, because then it is
// already counted as itself. Pinned by
// TestGetBalanceSummary_MasklessPair_ClosedSiblingIsNotCountedThroughTheLiveTwin (which measures
// the total) and TestGetNetWorth_MasklessPair_ClosedCreditSiblingIsNotAnAsset (which measures the
// type — a closed CREDIT sibling was being reported inside TotalAssets).
//
// ⚠ RESIDUAL. This is still per-instant evidence, not a recorded pointer. A closed sibling whose
// live twin has NO snapshot in the same set is still stood in for, and is then counted under the
// twin. That shape needs the twin to be silent at the very cutoff being read, and the honest fix
// remains a recorded succession pointer rather than a better guess.
func supersededSnapshotIndexes(snapshots []domain.BalanceSnapshot, maps ...map[string]domain.Account) map[int]bool {
	liveChains := map[string]bool{}
	claimed := map[string]bool{}
	owners := make(map[int]domain.Account, len(snapshots))
	for i, snapshot := range snapshots {
		account, exists := snapshotOwnAccount(snapshot, maps...)
		if !exists {
			continue
		}
		owners[i] = account
		claimed[account.ID] = true
		if account.Status == domain.AccountStatusActive && hasChainKey(account) {
			liveChains[accountChainKey(account)] = true
		}
	}
	out := map[int]bool{}
	for i, snapshot := range snapshots {
		own, hasOwn := owners[i]
		if hasOwn {
			if own.Status == domain.AccountStatusReplaced && hasChainKey(own) &&
				liveChains[accountChainKey(own)] {
				out[i] = true
			}
			continue
		}
		// No accounts row for this snapshot in this universe, so snapshotAccount stands the chain's
		// live member in for it. That is only sound while the live member has no snapshot of its
		// own here; when it does, it is already counted as itself and the stand-in would add a
		// SECOND balance — and a second account's TYPE — under one identity.
		if representative, ok := liveChainRepresentative(snapshot, maps...); ok && claimed[representative.ID] {
			out[i] = true
		}
	}
	return out
}

func hasChainKey(a domain.Account) bool {
	return a.UserAccountID != nil && *a.UserAccountID != ""
}

// snapshotAccount resolves a snapshot to the account whose identity should be reported beside it —
// its name, its id, and (decisively) its TYPE, which an AccountTypes filter uses to decide whether
// the money counts at all.
//
// ⚠ TWO RULES MEET HERE AND THEY PULL IN OPPOSITE DIRECTIONS. Getting the precedence wrong breaks
// one of two shipped rulings, and this function exists so the choice is written down once.
//
//  1. THE SNAPSHOT BELONGS TO ITS OWN ACCOUNT. Two accounts at one institution with no mask are
//     forced to share a user_accounts row (migration 000100), so resolving through the chain key
//     made a two-account breakdown two copies of whichever account won buildChainAwareAccountMap's
//     chain-key slot (#1569 T10.2).
//
//  2. ...UNLESS THAT ACCOUNT IS 'replaced', in which case it is a retired PREDECESSOR and the
//     snapshot is interpreted by the live SUCCESSOR. That is the add-only merge direction
//     mergeMissingAccounts already applies and which
//     TestDashboard_PastAsOfDate_MergeDirection_DecidesInclusionUnderATypeFilter pins as a TOTAL,
//     not a label: resolving to the predecessor drops $300 under a type filter. Having the current
//     leg and the baseline leg of one response resolve one chain differently is a class of bug this
//     file has been bitten by twice (#1369 G1, #1537 round 10).
//
// ⚠ 'replaced' — NOT "terminal" — IS THE DISCRIMINATOR, and #1569 T10-FIX corrected that. Round 10
// wrote rule 2 as "a live chain representative wins over a terminal predecessor, unless the
// snapshot's own account is ALSO live". A maskless sibling that the user simply CLOSED is terminal
// and is not a predecessor, so that form resolved a closed CREDIT card to its live DEPOSITORY twin
// and reported a liability inside TotalAssets. 'replaced' is written by the re-link and the
// CreateBatch backstop and by nothing else; 'removed'/'expired' mean closed.
//
// ⚠ AND THE OWN-ACCOUNT LOOKUP IS ONLY AS GOOD AS THE UNIVERSE IT SEARCHES. A closed sibling is
// absent from the active-only current map entirely, so there is no status to read and the chain-key
// stand-in below fires anyway. supersededSnapshotIndexes withdraws that stand-in when the live member
// already contributes a snapshot of its own; the two functions are one rule in two halves and
// neither is sufficient alone.
//
// maps are consulted in caller order (active-now first, historical/as-of second) at each step.
func snapshotAccount(snapshot domain.BalanceSnapshot, maps ...map[string]domain.Account) (domain.Account, bool) {
	if own, ok := snapshotOwnAccount(snapshot, maps...); ok {
		if own.Status != domain.AccountStatusReplaced {
			return own, true
		}
		if representative, repOK := liveChainRepresentative(snapshot, maps...); repOK {
			return representative, true
		}
		return own, true
	}
	// The snapshot's own accounts row is not in this universe at all. Standing in for it with the
	// chain's live member is the #1109 rule and it keeps real money on the books
	// (TestDashboardService_GetNetWorth_ResolvesByUserAccountID: a re-linked chain whose successor
	// has not synced yet still reports its last known balance). It is also how a CLOSED maskless
	// sibling gets counted a second time under its live twin — which is why the stand-in is
	// withdrawn by supersededSnapshotIndexes whenever that live member already has a snapshot of its
	// own in the same set. Both halves are needed; neither is sufficient.
	if representative, ok := liveChainRepresentative(snapshot, maps...); ok {
		return representative, true
	}
	return lookupInAccountMaps(snapshotChainKey(snapshot), maps...)
}

// snapshotOwnAccount resolves a snapshot to the accounts row it was actually written for: its own
// account_id, or — only when balance_snapshots.account_id has been NULLed by its ON DELETE SET NULL
// FK — the chain key, which is then the only identity the row still carries.
func snapshotOwnAccount(snapshot domain.BalanceSnapshot, maps ...map[string]domain.Account) (domain.Account, bool) {
	if snapshot.AccountID != "" {
		return lookupInAccountMaps(snapshot.AccountID, maps...)
	}
	return lookupInAccountMaps(snapshotChainKey(snapshot), maps...)
}

func liveChainRepresentative(snapshot domain.BalanceSnapshot, maps ...map[string]domain.Account) (domain.Account, bool) {
	for _, m := range maps {
		if account, ok := m[snapshotChainKey(snapshot)]; ok && account.Status == domain.AccountStatusActive {
			return account, true
		}
	}
	return domain.Account{}, false
}

func lookupInAccountMaps(key string, maps ...map[string]domain.Account) (domain.Account, bool) {
	if key == "" {
		return domain.Account{}, false
	}
	for _, m := range maps {
		if account, ok := m[key]; ok {
			return account, true
		}
	}
	return domain.Account{}, false
}

// DashboardService provides balance dashboard functionality
// Issue #353: Balance Tracking Dashboard
// Issue #127: Net Worth Tracking & Visualization
type DashboardService interface {
	// GetBalanceSummary returns current balance overview with breakdown by account type
	GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error)

	// GetBalanceHistory returns time series balance data for charts
	GetBalanceHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceHistoryResponse, error)

	// GetAccountTypes returns available account types for the user
	GetAccountTypes(ctx context.Context, userID string) (*domain.AccountTypesResponse, error)

	// GetNetWorth returns current net worth (assets - liabilities) with breakdown
	// Issue #127: Net Worth Tracking & Visualization
	GetNetWorth(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.NetWorthResponse, error)

	// GetNetWorthHistory returns time series net worth data for charts
	// Issue #127: Net Worth Tracking & Visualization
	GetNetWorthHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.NetWorthHistoryResponse, error)
}

type dashboardService struct {
	balanceSnapshotRepo repository.BalanceSnapshotRepository
	accountRepo         repository.AccountRepository

	// sf coalesces the repository reads GetBalanceSummary/GetNetWorth (and their household
	// variants) duplicate — see dashboardSFDo's doc comment. Zero value is ready to
	// use; deliberately not constructor-injected, since it holds no state a test would ever need to
	// seed.
	sf singleflight.Group
}

// NewDashboardService creates a new DashboardService instance
func NewDashboardService(
	balanceSnapshotRepo repository.BalanceSnapshotRepository,
	accountRepo repository.AccountRepository,
) DashboardService {
	return &dashboardService{
		balanceSnapshotRepo: balanceSnapshotRepo,
		accountRepo:         accountRepo,
	}
}

// sfSnapshotsAsOfForUser coalesces concurrent personal-scope snapshot reads for the same
// (userID, cutoff) pair — GetBalanceSummary and GetNetWorth both call this with byte-identical
// arguments for both the current cutoff and the previous-period cutoff (#1569 round 11 item 1).
//
// Returns a fresh copy of the singleflight-shared slice, never the shared backing array itself:
// both callers sort.Slice their previousSnapshots result IN PLACE, and two goroutines sorting the
// same backing array concurrently is a data race, not merely a correctness question.
func (s *dashboardService) sfSnapshotsAsOfForUser(ctx context.Context, userID string, cutoff time.Time) ([]domain.BalanceSnapshot, error) {
	key := fmt.Sprintf("snap:u:%s:%d", userID, cutoff.UnixNano())
	shared, err := dashboardSFDo(&s.sf, ctx, key, func(c context.Context) ([]domain.BalanceSnapshot, error) {
		return s.balanceSnapshotRepo.GetSnapshotsAsOfForUser(c, userID, cutoff)
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.BalanceSnapshot, len(shared))
	copy(out, shared)
	return out, nil
}

// sfAccountsByUserID coalesces concurrent personal-scope "current" account reads — both
// GetBalanceSummary and GetNetWorth call accountRepo.GetByUserID(userID) with no other arguments.
func (s *dashboardService) sfAccountsByUserID(ctx context.Context, userID string) ([]domain.Account, error) {
	key := "acct:u:" + userID
	shared, err := dashboardSFDo(&s.sf, ctx, key, func(c context.Context) ([]domain.Account, error) {
		return s.accountRepo.GetByUserID(c, userID)
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Account, len(shared))
	copy(out, shared)
	return out, nil
}

// sfAccountsAsOf coalesces concurrent personal-scope as-of-live account reads keyed by (userID,
// asOf) — historicalAccountMap's fallback query, which both GetBalanceSummary's and GetNetWorth's
// baseline legs (and, via augmentWithAsOfUniverse, their current legs for a historical
// as_of_date) can call with an identical cutoff.
func (s *dashboardService) sfAccountsAsOf(ctx context.Context, userID string, asOf time.Time) ([]domain.Account, error) {
	key := fmt.Sprintf("acct:u:asof:%s:%d", userID, asOf.UnixNano())
	shared, err := dashboardSFDo(&s.sf, ctx, key, func(c context.Context) ([]domain.Account, error) {
		return s.accountRepo.GetAccountsWithInstitutionFiltered(c, userID, repository.AccountFilterOptions{
			AsOf:          &asOf,
			IncludeHidden: true,
		})
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Account, len(shared))
	copy(out, shared)
	return out, nil
}

// sfSnapshotsAsOfForHousehold is sfSnapshotsAsOfForUser for household scope — both
// getBalanceSummaryHousehold and getNetWorthHousehold call GetSnapshotsAsOfForHousehold with the
// same (requestingUserID, scope, cutoff) for both the current and previous cutoffs.
func (s *dashboardService) sfSnapshotsAsOfForHousehold(ctx context.Context, userID string, scope *domain.HouseholdScope, cutoff time.Time) ([]domain.BalanceSnapshot, error) {
	key := fmt.Sprintf("snap:hh:%s:%s:%d", userID, householdScopeKeyFragment(scope), cutoff.UnixNano())
	shared, err := dashboardSFDo(&s.sf, ctx, key, func(c context.Context) ([]domain.BalanceSnapshot, error) {
		return s.balanceSnapshotRepo.GetSnapshotsAsOfForHousehold(c, userID, scope, cutoff)
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.BalanceSnapshot, len(shared))
	copy(out, shared)
	return out, nil
}

// sfAccountsHouseholdActiveNow is sfAccountsByUserID for household scope — both
// getBalanceSummaryHousehold and getNetWorthHousehold call GetForHouseholdScopeActiveNow with the
// current cutoff; getNetWorthHousehold additionally calls it at the previous cutoff (a call the
// balance-summary side does not make, so that particular call pays no dedup benefit but is
// harmless to route through the same coalescing path).
func (s *dashboardService) sfAccountsHouseholdActiveNow(ctx context.Context, userID string, scope *domain.HouseholdScope, cutoff time.Time) ([]domain.Account, error) {
	key := fmt.Sprintf("acct:hh:active:%s:%s:%d", userID, householdScopeKeyFragment(scope), cutoff.UnixNano())
	shared, err := dashboardSFDo(&s.sf, ctx, key, func(c context.Context) ([]domain.Account, error) {
		return s.accountRepo.GetForHouseholdScopeActiveNow(c, userID, scope, cutoff)
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Account, len(shared))
	copy(out, shared)
	return out, nil
}

// sfAccountsHouseholdLiveAsOf is the household as-of-live fallback used by both
// augmentHouseholdWithAsOfUniverse and both household baseline legs' historical fallback — all of
// which can call GetForHouseholdScopeLiveAsOf with the identical (userID, scope, cutoff).
func (s *dashboardService) sfAccountsHouseholdLiveAsOf(ctx context.Context, userID string, scope *domain.HouseholdScope, cutoff time.Time) ([]domain.Account, error) {
	key := fmt.Sprintf("acct:hh:asof:%s:%s:%d", userID, householdScopeKeyFragment(scope), cutoff.UnixNano())
	shared, err := dashboardSFDo(&s.sf, ctx, key, func(c context.Context) ([]domain.Account, error) {
		return s.accountRepo.GetForHouseholdScopeLiveAsOf(c, userID, scope, cutoff)
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Account, len(shared))
	copy(out, shared)
	return out, nil
}

// GetBalanceSummary returns current balance overview with breakdown by account type
func (s *dashboardService) GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error) {
	if filters != nil && filters.HouseholdScope != nil {
		return s.getBalanceSummaryHousehold(ctx, userID, filters)
	}
	// Determine the date for balance snapshot
	asOfDate := domain.CalendarDayUTC(time.Now())
	if filters != nil && filters.AsOfDate != nil {
		asOfDate = domain.CalendarDayUTC(*filters.AsOfDate)
	}

	// Both sides of this response are anchored on asOfDate: the current figures are the latest
	// snapshot per chain AT OR BEFORE the end of that day, and the baseline (below) is the same
	// read 30 days earlier. Before #1537 the current figures came from the unbounded
	// GetLatestByUserID while only the baseline moved with as_of_date, so a request for a past
	// date returned TODAY's total labelled with that date and compared it against a 30-days-before
	// -that-date baseline — a change over an interval nobody asked for.
	// #1569 round 11 item 1: routed through sfSnapshotsAsOfForUser/sfAccountsByUserID rather than
	// the repo directly — GetNetWorth issues this identical read for the identical (userID, cutoff),
	// and a single dashboard page load fires both endpoints together. See dashboardSFDo's doc
	// comment for the mechanism and why it introduces no cache.
	snapshots, err := s.sfSnapshotsAsOfForUser(ctx, userID, endOfDay(asOfDate))
	if err != nil {
		return nil, fmt.Errorf("failed to get balance snapshots: %w", err)
	}

	// Get accounts to map account IDs to account details (GetByUserID filters to active only)
	accounts, err := s.sfAccountsByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	accountMap := buildChainAwareAccountMap(accounts)
	// #1569 task 21 (C2): the snapshots above were read AS OF asOfDate; the map that INTERPRETS
	// them must be resolved at the same instant, or a since-disconnected account's snapshot is
	// silently dropped from the figure this response is labelled with. The baseline half of this
	// very response already does this (historicalAccountMap, below); the current half did not.
	if err := s.augmentWithAsOfUniverse(ctx, userID, asOfDate, snapshots, accountMap); err != nil {
		return nil, err
	}

	// Group snapshots by account type
	byType := make(map[string]*domain.AccountTypeBalance)
	var totalBalance float64

	// #1569 T10.2: applied to the CURRENT leg as well as the baseline leg, deliberately. The two
	// legs of one response disagreeing about one chain is a class of bug this file has been bitten
	// by twice (#1369 G1, #1537 round 10). It fires only when BOTH chain members contributed a row
	// at this instant — the overlap case — so a past as-of date whose only live member is the
	// now-terminal predecessor is untouched.
	supersededNow := supersededSnapshotIndexes(snapshots, accountMap)
	for i, snapshot := range snapshots {
		account, exists := snapshotAccount(snapshot, accountMap)
		if !exists {
			continue // Skip if account not found
		}
		if supersededNow[i] {
			continue
		}

		// Apply account type filter if specified
		if filters != nil && len(filters.AccountTypes) > 0 {
			typeMatch := false
			for _, filterType := range filters.AccountTypes {
				if account.Type == filterType {
					typeMatch = true
					break
				}
			}
			if !typeMatch {
				continue // Skip if account type doesn't match filter
			}
		}

		// Get current balance from snapshot
		balance := 0.0
		if snapshot.CurrentBalance != nil {
			balance = *snapshot.CurrentBalance
		}

		// Initialize type balance if not exists
		if _, exists := byType[account.Type]; !exists {
			byType[account.Type] = &domain.AccountTypeBalance{
				Balance:      0,
				AccountCount: 0,
				Accounts:     []domain.AccountBalanceDetail{},
			}
		}

		// Add to type aggregation
		typeBalance := byType[account.Type]
		typeBalance.Balance += balance
		typeBalance.AccountCount++
		typeBalance.Accounts = append(typeBalance.Accounts, domain.AccountBalanceDetail{
			ID:      account.ID,
			Name:    account.Name,
			Balance: balance,
		})

		totalBalance += balance
	}

	// Calculate period comparison (vs 30 days ago)
	var periodComparison *domain.PeriodComparisonData
	previousDate := asOfDate.AddDate(0, 0, -30)

	// A point-in-time read, NOT a date range. GetByUserIDAndDateRange(previousDate, previousDate)
	// was a zero-width window — `snapshot_at >= $2 AND snapshot_at <= $3` with both bounds at
	// midnight UTC — and production stores wall-clock instants (transaction_sync_service.go writes
	// `time.Now().UTC()`), so it matched nothing and this whole block never ran for a real user.
	// endOfDay so a snapshot taken any time on previousDate counts, and "latest at or before the
	// cutoff" so a user who was not synced that exact day still gets their most recent prior
	// balance rather than no comparison at all. Issue #1537.
	// Hoisted to a variable because the baseline account universe must be resolved at exactly
	// this instant too — see historicalAccountMap. Reading the snapshots at one cutoff and the
	// accounts at another is a class of bug that has already been fixed twice in the household
	// paths (#1369 G1, #1537 round 10); sharing the value makes it unrepresentable here.
	previousCutoff := endOfDay(previousDate)
	// #1569 round 11 item 1: GetNetWorth's baseline leg reads this identical (userID, previousCutoff)
	// pair — see sfSnapshotsAsOfForUser.
	previousSnapshots, err := s.sfSnapshotsAsOfForUser(ctx, userID, previousCutoff)
	if err == nil && len(previousSnapshots) > 0 {
		// Sort descending so the dedup below keeps the LATEST snapshot per sharing chain.
		// ⚠ #1569 T10-FIX item 9: this said "the repo already returns one row per chain", which
		// #1569 T10.2 made false — GetSnapshotsAsOfForUser returns one row per PHYSICAL ACCOUNT
		// (snapshotIdentityKey), precisely so a maskless pair is not halved. A chain can therefore
		// surface twice for two reasons now: two live siblings sharing a user_account (which MUST
		// both count, and which supersededSnapshotIndexes deliberately does not collapse), and a legacy
		// NULL-user_account_id row keying on its raw account_id beside its post-relink sibling. The
		// sort exists for the second, whose two rows carry different instants.
		sort.Slice(previousSnapshots, func(i, j int) bool {
			return previousSnapshots[i].SnapshotAt.After(previousSnapshots[j].SnapshotAt)
		})
		var accountTypes []string
		if filters != nil {
			accountTypes = filters.AccountTypes
		}
		historicalMap, histErr := s.historicalAccountMap(ctx, userID, previousCutoff, previousSnapshots, accountMap)
		if histErr != nil {
			// Omit the comparison rather than report one computed over the wrong universe. This
			// matches how the surrounding code already degrades (a failed previous-snapshot read
			// leaves periodComparison nil), and reporting "no change" for a disconnection is
			// exactly the user-visible lie #1537 round 10 exists to remove.
			return &domain.BalanceSummaryResponse{
				AsOfDate:        asOfDate,
				AsOfDateDisplay: domain.FormatCalendarDayUTC(asOfDate),
				TotalBalance:    totalBalance,
				ByType:          byType,
			}, nil
		}
		previousBalance := baselineBalanceForComparison(previousSnapshots, accountMap, historicalMap, accountTypes, true)

		changeAmount := totalBalance - previousBalance
		changePercent := 0.0
		if previousBalance != 0 {
			changePercent = (changeAmount / previousBalance) * 100
		}

		periodComparison = &domain.PeriodComparisonData{
			PreviousBalance: previousBalance,
			ChangeAmount:    changeAmount,
			ChangePercent:   changePercent,
		}
	}

	return &domain.BalanceSummaryResponse{
		AsOfDate:         asOfDate,
		AsOfDateDisplay:  domain.FormatCalendarDayUTC(asOfDate),
		TotalBalance:     totalBalance,
		ByType:           byType,
		PeriodComparison: periodComparison,
	}, nil
}

// historicalAccountMap returns the chain-aware account map for the personal-scope comparison
// baseline: the accounts that account_status_history records as having been LIVE at asOf, so a
// snapshot belonging to a chain that has since been fully replaced, expired or removed can still
// be attributed to an account type. It returns an empty (safe to read) map without querying when
// every baseline snapshot already resolves in activeMap, which is the common case — a merely
// re-linked chain resolves there through its user_account_id key. That short-circuit avoids an
// EXTRA account query on top of this call's caller's own read, but it does NOT bound the request
// to one account query overall.
//
// ⚠ #1569 round 11 item 4: this used to say "the ordinary dashboard request still issues exactly
// one account query" — false, in two ways this function's own callers now demonstrate. (1) This
// function has TWO call sites per personal request that can each independently miss the
// short-circuit: augmentWithAsOfUniverse (the CURRENT leg, for a historical as_of_date) and the
// baseline leg below, each contributing its own extra query when its own snapshots don't already
// resolve — a historical request can pay for both. (2) The short-circuit's own trigger,
// "a merely re-linked chain," is exactly a disconnected-or-relinked account, i.e. NOT the common
// case for the users this fallback exists to serve — so "ordinary" overstated how often it fires.
// Say only what the gate's own condition guarantees: zero extra queries when it holds, one extra
// query per call site when it does not.
//
// asOf MUST be the same cutoff the baseline snapshot read used. The two together are one question
// ("what was the world holding on that day"); resolving the balances at one instant and the
// universe that interprets them at another is the #1369 G1 defect in personal clothing.
//
// AsOf, not AllStatuses (#1569, replacing #1537's approximation). AllStatuses removed the status
// predicate entirely, which answers "every account that exists in any status TODAY". That
// over-includes — an account opened AFTER asOf is in it too — and it can only ever say an account
// exists, never that it was live then; the baseline was correct only because a
// not-yet-opened account has no snapshot before the cutoff to contribute. account_status_history
// records the real intervals, so the universe is now recorded truth rather than a guess that
// happens to arrive at the right number. (IncludeReplaced was never a candidate: it produces
// `status IN ('active','replaced')` and so misses 'expired', which is what
// ExpireAccountsByFingerprint writes for a fully-disconnected chain.)
//
// One consequence of that switch is deliberate rather than overlooked (#1569 Task 18): an account
// with no recorded 'active' interval AT ALL is outside this universe at every cutoff, where
// AllStatuses would have admitted it. 000152's backfill produces that shape for a pre-existing
// terminal-status row whose transition timestamp is not strictly after created_at — the row was
// never observably live for a nonzero duration, so claiming it was would be inventing history.
// It is also the only way to reach that shape: the NULL-timestamp variant is refused by three
// validated CHECK constraints plus updated_at's NOT NULL, and every account created after 000152
// gets its 'active' interval from the AFTER INSERT trigger. Both halves are pinned by
// repository/account_status_history_degenerate_backfill_integration_test.go, which asserts the
// as-of universe through this very call (GetAccountsWithInstitutionFiltered + AsOf).
//
// IncludeHidden is true to match GetByUserID, which applies no is_hidden predicate at all —
// a baseline source that defaults to hiding hidden accounts would silently change which accounts
// count toward it.
// augmentWithAsOfUniverse adds, to the CURRENT-leg account map of a personal-scope read, the
// accounts that were live at asOfDate but are not active today. It mutates accountMap in place and
// is a no-op unless asOfDate is genuinely in the past.
//
// It reuses historicalAccountMap rather than issuing its own query, which buys three things: the
// same IncludeHidden treatment (GetByUserID applies no is_hidden predicate, so a source that hides
// them would change which accounts count), the same recorded-liveness universe, and the same
// hasSnapshotOutsideAccountMap short-circuit — so the common case, where every snapshot already
// resolves, costs no extra round trip at all.
//
// An error here is FATAL rather than degrading to the un-augmented map. The un-augmented map is
// precisely the defect: it produces a confidently-labelled figure that is too small, and this
// endpoint is reachable by an agent. Both callers already fail the request when the account read
// fails; this is the same failure for the same reason.
func (s *dashboardService) augmentWithAsOfUniverse(
	ctx context.Context,
	userID string,
	asOfDate time.Time,
	snapshots []domain.BalanceSnapshot,
	accountMap map[string]domain.Account,
) error {
	if !asOfIsHistorical(asOfDate) {
		return nil
	}
	asOfMap, err := s.historicalAccountMap(ctx, userID, endOfDay(asOfDate), snapshots, accountMap)
	if err != nil {
		return fmt.Errorf("failed to resolve the as-of account universe for %s: %w",
			asOfDate.Format("2006-01-02"), err)
	}
	mergeMissingAccounts(accountMap, asOfMap)
	return nil
}

// augmentHouseholdWithAsOfUniverse is augmentWithAsOfUniverse for household scope. The household
// pair is split across two repository methods rather than one option struct
// (GetForHouseholdScopeActiveNow / GetForHouseholdScopeLiveAsOf), so the shape differs; the rule,
// the seam and the add-only merge are identical.
func (s *dashboardService) augmentHouseholdWithAsOfUniverse(
	ctx context.Context,
	userID string,
	scope *domain.HouseholdScope,
	asOfDate time.Time,
	cutoff time.Time,
	snapshots []domain.BalanceSnapshot,
	accountMap map[string]domain.Account,
) error {
	if !asOfIsHistorical(asOfDate) || !hasSnapshotOutsideAccountMap(snapshots, accountMap) {
		return nil
	}
	// #1569 round 11 item 1: both endpoints' current legs (and both baseline legs' fallback, below)
	// can call GetForHouseholdScopeLiveAsOf with an identical (userID, scope, cutoff) triple.
	asOfAccounts, err := s.sfAccountsHouseholdLiveAsOf(ctx, userID, scope, cutoff)
	if err != nil {
		return fmt.Errorf("failed to resolve the as-of household account universe for %s: %w",
			asOfDate.Format("2006-01-02"), err)
	}
	mergeMissingAccounts(accountMap, buildChainAwareAccountMap(asOfAccounts))
	return nil
}

func (s *dashboardService) historicalAccountMap(
	ctx context.Context,
	userID string,
	asOf time.Time,
	baselineSnapshots []domain.BalanceSnapshot,
	activeMap map[string]domain.Account,
) (map[string]domain.Account, error) {
	if !hasSnapshotOutsideAccountMap(baselineSnapshots, activeMap) {
		return map[string]domain.Account{}, nil
	}
	// #1569 round 11 item 1: this single function is called from BOTH GetBalanceSummary's and
	// GetNetWorth's baseline legs (and, via augmentWithAsOfUniverse, both endpoints' current legs
	// for a historical as_of_date) with an identical (userID, asOf) pair — sfAccountsAsOf coalesces
	// the concurrent overlap.
	accounts, err := s.sfAccountsAsOf(ctx, userID, asOf)
	if err != nil {
		return nil, fmt.Errorf("failed to get as-of-live accounts for comparison baseline: %w", err)
	}
	return buildChainAwareAccountMap(accounts), nil
}

// getBalanceSummaryHousehold is the household-scope variant of GetBalanceSummary. It MUST
// NOT call GetLatestByUserID/GetByUserID — those are single-user and would silently drop
// every non-requester member's accounts from the aggregate (Codex adversarial review,
// P0-2). GetSnapshotsAsOfForHousehold/GetForHouseholdScopeActiveNow already dedup joint accounts
// (freshest-synced copy) and resolve re-link chains via the stable user_account_id, so —
// unlike the personal path — no further chain-key dedup loop is needed here.
//
// The "current" snapshot fetch below is anchored on asOfDate, not time.Now(). It used to use
// time.Now() to mirror the personal path, whose current read (GetLatestByUserID) had no date
// parameter at all — so filters.AsOfDate shaped the response LABEL and the baseline but never the
// current figures, and a request for a past date returned today's total labelled with that date.
// #1537 anchored both scopes on asOfDate instead; the personal read is now
// GetSnapshotsAsOfForUser(endOfDay(asOfDate)) and this one matches it.
func (s *dashboardService) getBalanceSummaryHousehold(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error) {
	scope := filters.HouseholdScope
	asOfDate := domain.CalendarDayUTC(time.Now())
	if filters.AsOfDate != nil {
		asOfDate = domain.CalendarDayUTC(*filters.AsOfDate)
	}

	// currentCutoff is derived from asOfDate, so the snapshot query and the account-map query
	// below necessarily agree on which member's copy wins a joint account's fingerprint. Two
	// independent time.Now().UTC() calls — what this used to be — could straddle a sync landing in
	// between and resolve that winner differently (the same class of bug fixed for the
	// previous-period pair in #1369 gap-closure G1; flagged by Codex adversarial review on the G1
	// fix itself). Deriving it from asOfDate makes the agreement structural rather than a
	// convention one edit could break.
	currentCutoff := endOfDay(asOfDate)
	// #1569 round 11 item 1: getNetWorthHousehold reads this identical (userID, scope, currentCutoff)
	// triple — see sfSnapshotsAsOfForHousehold.
	snapshots, err := s.sfSnapshotsAsOfForHousehold(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household balance snapshots: %w", err)
	}
	// This "now"-asOf map is the CURRENT universe: active accounts only, per the product rule that
	// current totals include only linked/active accounts.
	//
	// ⚠ This comment has been wrong twice, in opposite directions, so the history is recorded rather
	// than quietly overwritten a third time. It first said the comparison "sums snapshots directly
	// without any accountMap lookup" — true, and a bug: the baseline then included accounts the
	// filtered current total excluded, making ChangePercent arbitrary under a type filter (#1537
	// round 9). Round 9's replacement then said this one map "serves both the current total and the
	// previous-period baseline" — also a bug, because GetForHouseholdScopeActiveNow filters
	// `a.status = 'active'` regardless of its asOf, so a household account disconnected since the
	// cutoff vanished from the baseline and a real decrease was reported as "no change" (#1537
	// round 10). The baseline needs its OWN map, resolved at the previous cutoff and over all
	// statuses — like getNetWorthHousehold, this path needs a map per point in time after all.
	// #1569 round 11 item 1: getNetWorthHousehold reads this identical (userID, scope, currentCutoff)
	// triple — see sfAccountsHouseholdActiveNow.
	accounts, err := s.sfAccountsHouseholdActiveNow(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household accounts: %w", err)
	}
	accountMap := buildChainAwareAccountMap(accounts)
	// #1569 task 21 (C2): same defect as the personal path, same fix — for a PAST as_of_date the
	// interpreting map must come from recorded liveness at that date, because
	// GetForHouseholdScopeActiveNow filters `a.status = 'active'` regardless of its cutoff. The
	// baseline below already reaches for GetForHouseholdScopeLiveAsOf for exactly this reason.
	if err := s.augmentHouseholdWithAsOfUniverse(ctx, userID, scope, asOfDate, currentCutoff, snapshots, accountMap); err != nil {
		return nil, err
	}

	byType := make(map[string]*domain.AccountTypeBalance)
	var totalBalance float64
	// #1569 T10.2: applied to the CURRENT leg as well as the baseline leg, deliberately. The two
	// legs of one response disagreeing about one chain is a class of bug this file has been bitten
	// by twice (#1369 G1, #1537 round 10). It fires only when BOTH chain members contributed a row
	// at this instant — the overlap case — so a past as-of date whose only live member is the
	// now-terminal predecessor is untouched.
	supersededNow := supersededSnapshotIndexes(snapshots, accountMap)
	for i, snapshot := range snapshots {
		account, exists := snapshotAccount(snapshot, accountMap)
		if !exists {
			continue
		}
		if supersededNow[i] {
			continue
		}
		if len(filters.AccountTypes) > 0 {
			typeMatch := false
			for _, filterType := range filters.AccountTypes {
				if account.Type == filterType {
					typeMatch = true
					break
				}
			}
			if !typeMatch {
				continue
			}
		}

		balance := 0.0
		if snapshot.CurrentBalance != nil {
			balance = *snapshot.CurrentBalance
		}

		if _, exists := byType[account.Type]; !exists {
			byType[account.Type] = &domain.AccountTypeBalance{
				Balance:      0,
				AccountCount: 0,
				Accounts:     []domain.AccountBalanceDetail{},
			}
		}
		typeBalance := byType[account.Type]
		typeBalance.Balance += balance
		typeBalance.AccountCount++
		typeBalance.Accounts = append(typeBalance.Accounts, domain.AccountBalanceDetail{
			ID:      account.ID,
			Name:    account.Name,
			Balance: balance,
		})
		totalBalance += balance
	}

	var periodComparison *domain.PeriodComparisonData
	previousDate := asOfDate.AddDate(0, 0, -30)
	// previousCutoff bounds the baseline snapshot read, and — only when the historical fallback
	// fires — the GetForHouseholdScopeLiveAsOf read below.
	//
	// It is NOT shared with a prev-cutoff PRIMARY account map: unlike getNetWorthHousehold, which
	// builds one (see the eight lines there explaining why), this path's primary lookup is the
	// CURRENT-cutoff accountMap built above, and previousCutoff reaches only the fallback. That is
	// a real difference between the two household paths, stated plainly because the comment here
	// previously claimed the sharing that getNetWorthHousehold has and this path does not — so a
	// reader looking for the #1369 G1 joint-account-winner guarantee would have believed it was
	// already present here. It is not; the fallback agrees with the snapshot read, the primary
	// lookup does not.
	//
	// endOfDay, not previousDate: `snapshot_at <= previousDate` would demand a snapshot before
	// midnight and skip every one taken during the baseline day itself.
	previousCutoff := endOfDay(previousDate)
	// #1569 round 11 item 1: getNetWorthHousehold's baseline leg reads this identical
	// (userID, scope, previousCutoff) triple — see sfSnapshotsAsOfForHousehold.
	previousSnapshots, err := s.sfSnapshotsAsOfForHousehold(ctx, userID, scope, previousCutoff)
	if err == nil && len(previousSnapshots) > 0 {
		historicalMap := map[string]domain.Account{}
		if hasSnapshotOutsideAccountMap(previousSnapshots, accountMap) {
			// #1569 round 11 item 1: getNetWorthHousehold's fallback can call
			// GetForHouseholdScopeLiveAsOf with this identical triple too — see
			// sfAccountsHouseholdLiveAsOf.
			historicalAccounts, histErr := s.sfAccountsHouseholdLiveAsOf(ctx, userID, scope, previousCutoff)
			if histErr != nil {
				// Omit the comparison rather than compute it over the wrong universe — see the
				// personal path's identical guard for why silence beats a false "no change".
				return &domain.BalanceSummaryResponse{
					AsOfDate:        asOfDate,
					AsOfDateDisplay: domain.FormatCalendarDayUTC(asOfDate),
					TotalBalance:    totalBalance,
					ByType:          byType,
				}, nil
			}
			historicalMap = buildChainAwareAccountMap(historicalAccounts)
		}
		previousBalance := baselineBalanceForComparison(previousSnapshots, accountMap, historicalMap, filters.AccountTypes, false)
		changeAmount := totalBalance - previousBalance
		changePercent := 0.0
		if previousBalance != 0 {
			changePercent = (changeAmount / previousBalance) * 100
		}
		periodComparison = &domain.PeriodComparisonData{
			PreviousBalance: previousBalance,
			ChangeAmount:    changeAmount,
			ChangePercent:   changePercent,
		}
	}

	return &domain.BalanceSummaryResponse{
		AsOfDate:         asOfDate,
		AsOfDateDisplay:  domain.FormatCalendarDayUTC(asOfDate),
		TotalBalance:     totalBalance,
		ByType:           byType,
		PeriodComparison: periodComparison,
	}, nil
}

// ⚠ ASSUMES d IS UTC. time.Truncate operates on absolute time since the zero instant and is
// LOCATION-BLIND, so this always returns a UTC-day boundary regardless of d's location. Measured: an
// IST-zoned midnight yields 2026-08-19 05:29:59.999999999 +0530, i.e. 2026-08-18 23:59:59.999 UTC —
// the WRONG DAY. Safe today only because filters.AsOfDate has exactly one producer,
// dateutil.ParseOptionalDate's time.Parse("2006-01-02", s), which yields UTC. Pass a zoned time here
// and the cutoff is off by a day (#1537 round 13).
// endOfDay returns the last representable instant of d's calendar day (23:59:59.999999999),
// so an "as of this date" snapshot lookup (`snapshot_at <= asOf`) includes every snapshot
// taken that day, not just ones before midnight.
func endOfDay(d time.Time) time.Time {
	return d.Truncate(24 * time.Hour).Add(24*time.Hour - time.Nanosecond)
}

// GetBalanceHistory returns time series balance data for charts
func (s *dashboardService) GetBalanceHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceHistoryResponse, error) {
	// Default to last 30 days if not specified
	endDate := domain.CalendarDayUTC(time.Now())
	startDate := endDate.AddDate(0, 0, -30)

	if filters != nil {
		if filters.StartDate != nil {
			startDate = domain.CalendarDayUTC(*filters.StartDate)
		}
		if filters.EndDate != nil {
			endDate = domain.CalendarDayUTC(*filters.EndDate)
		}
	}

	// Validate date range
	if startDate.After(endDate) {
		return nil, fmt.Errorf("start_date must be before or equal to end_date")
	}

	// Get account types to filter (if specified)
	var accountTypes []string
	if filters != nil && len(filters.AccountTypes) > 0 {
		accountTypes = filters.AccountTypes
	}

	// Get aggregated balance history from repository. Household mode fans out across every
	// member's shared accounts (GetAggregatedHistoryForHousehold) instead of the personal,
	// single-user GetAggregatedHistory.
	var history []domain.BalanceHistoryPoint
	var err error
	if filters != nil && filters.HouseholdScope != nil {
		history, err = s.balanceSnapshotRepo.GetAggregatedHistoryForHousehold(ctx, userID, filters.HouseholdScope, startDate, endDate, accountTypes)
	} else {
		history, err = s.balanceSnapshotRepo.GetAggregatedHistory(ctx, userID, startDate, endDate, accountTypes)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get balance history: %w", err)
	}

	// Convert to response format
	snapshots := make([]domain.BalanceHistorySnapshot, len(history))
	for i, point := range history {
		snapshots[i] = domain.BalanceHistorySnapshot{
			Date:        point.Date,
			DateDisplay: point.Date.Format(time.DateOnly),
			Balance:     point.TotalBalance,
			ByType:      point.ByType,
		}
	}

	granularity := "daily"
	if filters != nil && filters.Granularity != "" {
		granularity = filters.Granularity
	}

	return &domain.BalanceHistoryResponse{
		StartDate:        startDate,
		StartDateDisplay: domain.FormatCalendarDayUTC(startDate),
		EndDate:          endDate,
		EndDateDisplay:   domain.FormatCalendarDayUTC(endDate),
		Granularity:      granularity,
		Snapshots:        snapshots,
	}, nil
}

// GetAccountTypes returns available account types for the user
func (s *dashboardService) GetAccountTypes(ctx context.Context, userID string) (*domain.AccountTypesResponse, error) {
	// Get latest snapshots for each account
	snapshots, err := s.balanceSnapshotRepo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance snapshots: %w", err)
	}

	// Get accounts to map account IDs to types (GetByUserID filters to active only)
	accounts, err := s.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	accountMap := buildChainAwareAccountMap(accounts)

	// Aggregate by account type
	typeMap := make(map[string]*domain.AccountTypeInfo)

	// #1569 T10.2: applied to the CURRENT leg as well as the baseline leg, deliberately. The two
	// legs of one response disagreeing about one chain is a class of bug this file has been bitten
	// by twice (#1369 G1, #1537 round 10). It fires only when BOTH chain members contributed a row
	// at this instant — the overlap case — so a past as-of date whose only live member is the
	// now-terminal predecessor is untouched.
	supersededNow := supersededSnapshotIndexes(snapshots, accountMap)
	for i, snapshot := range snapshots {
		account, exists := snapshotAccount(snapshot, accountMap)
		if !exists {
			continue
		}
		if supersededNow[i] {
			continue
		}

		// Initialize type info if not exists
		if _, exists := typeMap[account.Type]; !exists {
			typeMap[account.Type] = &domain.AccountTypeInfo{
				Key:          account.Type,
				Label:        formatAccountTypeLabel(account.Type),
				Count:        0,
				TotalBalance: 0,
			}
		}

		typeInfo := typeMap[account.Type]
		typeInfo.Count++

		if snapshot.CurrentBalance != nil {
			typeInfo.TotalBalance += *snapshot.CurrentBalance
		}
	}

	// Convert map to slice
	types := make([]domain.AccountTypeInfo, 0, len(typeMap))
	for _, typeInfo := range typeMap {
		types = append(types, *typeInfo)
	}

	return &domain.AccountTypesResponse{
		Types: types,
	}, nil
}

// formatAccountTypeLabel converts account type keys to user-friendly labels
func formatAccountTypeLabel(accountType string) string {
	labels := map[string]string{
		"investment": "Investments",
		"depository": "Checking & Savings",
		"credit":     "Credit Cards",
		"loan":       "Loans",
		"other":      "Other",
	}

	if label, exists := labels[accountType]; exists {
		return label
	}
	return accountType
}

// GetNetWorth returns current net worth (assets - liabilities) with breakdown
func (s *dashboardService) GetNetWorth(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.NetWorthResponse, error) {
	if filters != nil && filters.HouseholdScope != nil {
		return s.getNetWorthHousehold(ctx, userID, filters)
	}
	// Determine the date for net worth calculation
	asOfDate := domain.CalendarDayUTC(time.Now())
	if filters != nil && filters.AsOfDate != nil {
		asOfDate = domain.CalendarDayUTC(*filters.AsOfDate)
	}

	// as_of_date anchors BOTH sides here too — see GetBalanceSummary's identical read.
	// #1569 round 11 item 1: GetBalanceSummary reads this identical (userID, cutoff) pair — see
	// sfSnapshotsAsOfForUser/sfAccountsByUserID.
	snapshots, err := s.sfSnapshotsAsOfForUser(ctx, userID, endOfDay(asOfDate))
	if err != nil {
		return nil, fmt.Errorf("failed to get balance snapshots: %w", err)
	}

	// Get accounts to map account IDs to account details
	accounts, err := s.sfAccountsByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	accountMap := buildChainAwareAccountMap(accounts)
	// #1569 task 21 (C2) — see GetBalanceSummary's identical augmentation. This endpoint is an MCP
	// `read` capability, so "what was my net worth on <past date>" reaching an agent as 0.00 was
	// the sharpest form of the defect.
	if err := s.augmentWithAsOfUniverse(ctx, userID, asOfDate, snapshots, accountMap); err != nil {
		return nil, err
	}

	// Initialize category structure
	byCategory := make(map[string]*domain.NetWorthCategoryBalance)
	var totalAssets, totalLiabilities float64

	// #1569 T10.2: applied to the CURRENT leg as well as the baseline leg, deliberately. The two
	// legs of one response disagreeing about one chain is a class of bug this file has been bitten
	// by twice (#1369 G1, #1537 round 10). It fires only when BOTH chain members contributed a row
	// at this instant — the overlap case — so a past as-of date whose only live member is the
	// now-terminal predecessor is untouched.
	supersededNow := supersededSnapshotIndexes(snapshots, accountMap)
	for i, snapshot := range snapshots {
		account, exists := snapshotAccount(snapshot, accountMap)
		if !exists {
			continue // Skip if account not found
		}
		if supersededNow[i] {
			continue
		}

		// Apply account type filter if specified
		if filters != nil && len(filters.AccountTypes) > 0 {
			typeMatch := false
			for _, filterType := range filters.AccountTypes {
				if account.Type == filterType {
					typeMatch = true
					break
				}
			}
			if !typeMatch {
				continue // Skip if account type doesn't match filter
			}
		}

		// Get current balance from snapshot
		balance := 0.0
		if snapshot.CurrentBalance != nil {
			balance = *snapshot.CurrentBalance
		}

		// Determine category (assets or liabilities)
		category := domain.AccountTypeCategory(account.Type)

		// Initialize category balance if not exists
		if _, exists := byCategory[category]; !exists {
			byCategory[category] = &domain.NetWorthCategoryBalance{
				Category:     category,
				Total:        0,
				AccountCount: 0,
				ByType:       make(map[string]*domain.AccountTypeBalance),
			}
		}

		// Initialize type balance within category if not exists
		categoryBalance := byCategory[category]
		if _, exists := categoryBalance.ByType[account.Type]; !exists {
			categoryBalance.ByType[account.Type] = &domain.AccountTypeBalance{
				Balance:      0,
				AccountCount: 0,
				Accounts:     []domain.AccountBalanceDetail{},
			}
		}

		// For liabilities, we store the absolute value but the balance comes in negative
		absBalance := balance
		if category == "liabilities" {
			if balance < 0 {
				absBalance = -balance // Convert negative to positive for liabilities
			}
		}

		// Add to type aggregation within category
		typeBalance := categoryBalance.ByType[account.Type]
		typeBalance.Balance += absBalance
		typeBalance.AccountCount++
		typeBalance.Accounts = append(typeBalance.Accounts, domain.AccountBalanceDetail{
			ID:      account.ID,
			Name:    account.Name,
			Balance: absBalance,
		})

		// Update category totals
		categoryBalance.Total += absBalance
		categoryBalance.AccountCount++

		// Update overall totals
		if category == "assets" {
			totalAssets += absBalance
		} else {
			totalLiabilities += absBalance
		}
	}

	// Calculate net worth
	netWorth := totalAssets - totalLiabilities

	// Calculate period comparison (vs 30 days ago)
	var periodComparison *domain.NetWorthPeriodComparison
	previousDate := asOfDate.AddDate(0, 0, -30)

	// Point-in-time, not a zero-width date range — see GetBalanceSummary's baseline read for why
	// the range form matched nothing in production (#1537).
	// Hoisted to a variable because the baseline account universe must be resolved at exactly
	// this instant too — see historicalAccountMap. Reading the snapshots at one cutoff and the
	// accounts at another is a class of bug that has already been fixed twice in the household
	// paths (#1369 G1, #1537 round 10); sharing the value makes it unrepresentable here.
	previousCutoff := endOfDay(previousDate)
	// #1569 round 11 item 1: GetBalanceSummary's baseline leg reads this identical (userID,
	// previousCutoff) pair — see sfSnapshotsAsOfForUser.
	previousSnapshots, err := s.sfSnapshotsAsOfForUser(ctx, userID, previousCutoff)
	if err == nil && len(previousSnapshots) > 0 {
		// Sort descending so the dedup below keeps the LATEST snapshot per chain key.
		sort.Slice(previousSnapshots, func(i, j int) bool {
			return previousSnapshots[i].SnapshotAt.After(previousSnapshots[j].SnapshotAt)
		})
		// The baseline uses a DELIBERATELY DIFFERENT account universe from the current total, for
		// the same reason baselineBalanceForComparison does (#1537 round 10) — this path was left
		// unfixed by that round even though both of its instruments already existed. accountMap
		// here is the active-only "now" map (GetByUserID: `status = 'active'`), while the snapshot
		// read filters on user_id alone, so a chain with no active member left had no entry and was
		// dropped: a $1,000 account disconnected 30 days ago produced PreviousAssets $0 and the
		// divide-by-zero guard reported net_worth_change_percent = 0 — "no change" — for a real
		// decline.
		//
		// That is user-visible, not merely internally inconsistent: GetAggregatedHistory
		// (balance_snapshot_repository.go) LEFT JOINs accounts with NO status predicate, so the
		// net-worth history CHART already counts the since-expired account. Unfixed, the chart draws
		// the drop while the KPI tile above it reads 0.00% — two widgets on one dashboard
		// disagreeing about the same 30 days.
		//
		// historicalMap supplies the since-inactive account's TYPE only, which is what decides
		// assets vs liabilities; the BALANCE always comes from the snapshot row. It is empty unless
		// some baseline snapshot actually resolves nowhere in accountMap, so the common path keeps
		// its single account query.
		// The baseline must apply the SAME AccountTypes filter the current total applied — see
		// accountTypeMatchesFilter for why this was missing here while the two balance-summary
		// baselines already had it.
		var accountTypes []string
		if filters != nil {
			accountTypes = filters.AccountTypes
		}
		historicalMap, histErr := s.historicalAccountMap(ctx, userID, previousCutoff, previousSnapshots, accountMap)
		if histErr != nil {
			// Omit the comparison rather than report one computed over the wrong universe — the
			// same degradation GetBalanceSummary applies, and for the same reason: reporting "no
			// change" for a disconnection is exactly the lie this fix removes.
			return &domain.NetWorthResponse{
				AsOfDate:         asOfDate,
				AsOfDateDisplay:  domain.FormatCalendarDayUTC(asOfDate),
				NetWorth:         netWorth,
				TotalAssets:      totalAssets,
				TotalLiabilities: totalLiabilities,
				ByCategory:       byCategory,
			}, nil
		}

		var prevAssets, prevLiabilities float64
		seenAccounts := make(map[string]bool)
		superseded := supersededSnapshotIndexes(previousSnapshots, accountMap, historicalMap)

		for i, snapshot := range previousSnapshots {
			// Active map first, historical map as fallback — the same precedence
			// baselineBalanceForComparison uses, through the same resolver.
			account, exists := snapshotAccount(snapshot, accountMap, historicalMap)
			if !exists {
				continue
			}
			if superseded[i] {
				continue
			}
			// Dedup on the resolved account's OWN id — see baselineBalanceForComparison for why
			// #1569 T10.2 moved this off accountChainKey and what the chain key was silently
			// costing a maskless pair. previousSnapshots is sorted DESC above, so the latest wins.
			if seenAccounts[account.ID] {
				continue
			}
			seenAccounts[account.ID] = true
			// The same AccountTypes filter the current-period loop above applies.
			if !accountTypeMatchesFilter(account.Type, accountTypes) {
				continue
			}

			balance := 0.0
			if snapshot.CurrentBalance != nil {
				balance = *snapshot.CurrentBalance
			}

			category := domain.AccountTypeCategory(account.Type)
			absBalance := balance
			if category == "liabilities" && balance < 0 {
				absBalance = -balance
			}

			if category == "assets" {
				prevAssets += absBalance
			} else {
				prevLiabilities += absBalance
			}
		}

		prevNetWorth := prevAssets - prevLiabilities
		changeAmount := netWorth - prevNetWorth
		changePercent := 0.0
		if prevNetWorth != 0 {
			changePercent = (changeAmount / prevNetWorth) * 100
		}

		periodComparison = &domain.NetWorthPeriodComparison{
			PreviousNetWorth:      prevNetWorth,
			PreviousAssets:        prevAssets,
			PreviousLiabilities:   prevLiabilities,
			NetWorthChange:        changeAmount,
			NetWorthChangePercent: changePercent,
		}
	}

	return &domain.NetWorthResponse{
		AsOfDate:         asOfDate,
		AsOfDateDisplay:  domain.FormatCalendarDayUTC(asOfDate),
		NetWorth:         netWorth,
		TotalAssets:      totalAssets,
		TotalLiabilities: totalLiabilities,
		ByCategory:       byCategory,
		PeriodComparison: periodComparison,
	}, nil
}

// getNetWorthHousehold is the household-scope variant of GetNetWorth. As with
// getBalanceSummaryHousehold, it uses GetSnapshotsAsOfForHousehold/GetForHouseholdScopeActiveNow
// (never the single-user GetLatestByUserID/GetByUserID — Codex adversarial review, P0-2)
// and needs no chain-key dedup loop since those methods already collapse joint accounts
// and re-link chains to one row each. The "current" fetch is anchored on asOfDate, matching the
// personal path — see getBalanceSummaryHousehold's comment for the behavior this replaced.
func (s *dashboardService) getNetWorthHousehold(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.NetWorthResponse, error) {
	scope := filters.HouseholdScope
	asOfDate := domain.CalendarDayUTC(time.Now())
	if filters.AsOfDate != nil {
		asOfDate = domain.CalendarDayUTC(*filters.AsOfDate)
	}

	// currentCutoff is derived from asOfDate and reused for both calls below — see the identical
	// comment in getBalanceSummaryHousehold for why two independent time.Now().UTC() calls
	// would be unsafe here (Codex adversarial review on the G1 fix).
	currentCutoff := endOfDay(asOfDate)
	// #1569 round 11 item 1: getBalanceSummaryHousehold reads this identical
	// (userID, scope, currentCutoff) triple — see sfSnapshotsAsOfForHousehold.
	snapshots, err := s.sfSnapshotsAsOfForHousehold(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household balance snapshots: %w", err)
	}
	// #1569 round 11 item 1: getBalanceSummaryHousehold reads this identical
	// (userID, scope, currentCutoff) triple — see sfAccountsHouseholdActiveNow.
	currentAccounts, err := s.sfAccountsHouseholdActiveNow(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household accounts: %w", err)
	}
	accountMap := buildChainAwareAccountMap(currentAccounts)
	// #1569 task 21 (C2) — see getBalanceSummaryHousehold's identical augmentation.
	if err := s.augmentHouseholdWithAsOfUniverse(ctx, userID, scope, asOfDate, currentCutoff, snapshots, accountMap); err != nil {
		return nil, err
	}

	byCategory := make(map[string]*domain.NetWorthCategoryBalance)
	var totalAssets, totalLiabilities float64

	// #1569 T10.2: applied to the CURRENT leg as well as the baseline leg, deliberately. The two
	// legs of one response disagreeing about one chain is a class of bug this file has been bitten
	// by twice (#1369 G1, #1537 round 10). It fires only when BOTH chain members contributed a row
	// at this instant — the overlap case — so a past as-of date whose only live member is the
	// now-terminal predecessor is untouched.
	supersededNow := supersededSnapshotIndexes(snapshots, accountMap)
	for i, snapshot := range snapshots {
		account, exists := snapshotAccount(snapshot, accountMap)
		if !exists {
			continue
		}
		if supersededNow[i] {
			continue
		}
		if len(filters.AccountTypes) > 0 {
			typeMatch := false
			for _, filterType := range filters.AccountTypes {
				if account.Type == filterType {
					typeMatch = true
					break
				}
			}
			if !typeMatch {
				continue
			}
		}

		balance := 0.0
		if snapshot.CurrentBalance != nil {
			balance = *snapshot.CurrentBalance
		}

		category := domain.AccountTypeCategory(account.Type)
		if _, exists := byCategory[category]; !exists {
			byCategory[category] = &domain.NetWorthCategoryBalance{
				Category:     category,
				Total:        0,
				AccountCount: 0,
				ByType:       make(map[string]*domain.AccountTypeBalance),
			}
		}
		categoryBalance := byCategory[category]
		if _, exists := categoryBalance.ByType[account.Type]; !exists {
			categoryBalance.ByType[account.Type] = &domain.AccountTypeBalance{
				Balance:      0,
				AccountCount: 0,
				Accounts:     []domain.AccountBalanceDetail{},
			}
		}

		absBalance := balance
		if category == "liabilities" && balance < 0 {
			absBalance = -balance
		}

		typeBalance := categoryBalance.ByType[account.Type]
		typeBalance.Balance += absBalance
		typeBalance.AccountCount++
		typeBalance.Accounts = append(typeBalance.Accounts, domain.AccountBalanceDetail{
			ID:      account.ID,
			Name:    account.Name,
			Balance: absBalance,
		})

		categoryBalance.Total += absBalance
		categoryBalance.AccountCount++

		if category == "assets" {
			totalAssets += absBalance
		} else {
			totalLiabilities += absBalance
		}
	}

	netWorth := totalAssets - totalLiabilities

	var periodComparison *domain.NetWorthPeriodComparison
	previousDate := asOfDate.AddDate(0, 0, -30)
	previousCutoff := endOfDay(previousDate)
	// #1569 round 11 item 1: getBalanceSummaryHousehold's baseline leg reads this identical
	// (userID, scope, previousCutoff) triple — see sfSnapshotsAsOfForHousehold.
	previousSnapshots, err := s.sfSnapshotsAsOfForHousehold(ctx, userID, scope, previousCutoff)
	if err == nil && len(previousSnapshots) > 0 {
		// #1369 gap-closure G1: the previous-period account map MUST be built with the SAME
		// asOf cutoff as previousSnapshots above, not reuse the "now"-asOf accountMap from
		// the current-period loop. GetForHouseholdScopeActiveNow's joint-account fingerprint winner
		// depends on asOf — if a joint account's globally-freshest copy has no snapshot
		// before previousCutoff, the "now" accountMap only knows about that (wrong-for-this-
		// period) copy's user_account_id, and the lookup below would silently drop the
		// account instead of resolving to the other member's genuinely earlier copy.
		//
		// The right point in time is still not enough: GetForHouseholdScopeActiveNow filters
		// `a.status = 'active'` REGARDLESS of its asOf, so a member's account disconnected since
		// the cutoff is absent from previousAccountMap and was dropped from the baseline — the
		// #1537 round 10 defect, in the path that round did not reach. GetForHouseholdScopeLiveAsOf
		// (#1569, superseding that round's since-removed all-statuses household read) supplies those accounts'
		// TYPE, which is what decides assets vs liabilities; the BALANCE always comes from the
		// snapshot row. It resolves the universe from account_status_history's recorded intervals,
		// so — unlike all-statuses — it also excludes accounts that did not exist yet at the cutoff.
		//
		// Why it is user-visible: GetAggregatedHistory LEFT JOINs accounts with NO status predicate,
		// so the net-worth history CHART already counts the since-expired account. Unfixed, the chart
		// draws the drop while the KPI tile above it reads 0.00%.
		// Not duplicated by getBalanceSummaryHousehold's baseline leg (it reuses the CURRENT-cutoff
		// accountMap here instead — see this function's own doc comment) so this particular call
		// gets no cross-endpoint dedup benefit; routed through the same helper for consistency.
		previousAccounts, acctErr := s.sfAccountsHouseholdActiveNow(ctx, userID, scope, previousCutoff)
		if acctErr == nil {
			previousAccountMap := buildChainAwareAccountMap(previousAccounts)
			// Gated on the PREVIOUS-cutoff map, not the "now" map. ⚠ #1569 T10-FIX: the gate now
			// asks whether each snapshot's OWN accounts row is present, so a re-linked chain and a
			// closed maskless sibling both trigger the read — see hasSnapshotOutsideAccountMap for
			// why resolving the chain key was answering a question nobody asked.
			historicalMap := map[string]domain.Account{}
			if hasSnapshotOutsideAccountMap(previousSnapshots, previousAccountMap) {
				// #1569 round 11 item 1: getBalanceSummaryHousehold's fallback can call
				// GetForHouseholdScopeLiveAsOf with this identical triple too — see
				// sfAccountsHouseholdLiveAsOf.
				historicalAccounts, histErr := s.sfAccountsHouseholdLiveAsOf(ctx, userID, scope, previousCutoff)
				if histErr != nil {
					// Omit the comparison rather than compute it over the wrong universe — see the
					// personal path's identical guard.
					return &domain.NetWorthResponse{
						AsOfDate:         asOfDate,
						AsOfDateDisplay:  domain.FormatCalendarDayUTC(asOfDate),
						NetWorth:         netWorth,
						TotalAssets:      totalAssets,
						TotalLiabilities: totalLiabilities,
						ByCategory:       byCategory,
					}, nil
				}
				historicalMap = buildChainAwareAccountMap(historicalAccounts)
			}
			var prevAssets, prevLiabilities float64
			// #1569 T10-FIX item 3: resolved per PHYSICAL account, through the same resolver every
			// other leg uses. This loop was the last raw `map[snapshotChainKey(snapshot)]` lookup
			// in the file, and once T10.2 made GetSnapshotsAsOfForHousehold return physical rows it
			// became a classification bug with a plausible-looking total: a maskless depository and
			// a maskless credit card share a user_accounts row, so both were categorised as
			// whichever of the two occupied the chain-key slot — $100 of assets and $50 of
			// liabilities reported as $150 of one of them. Pinned by
			// TestGetNetWorthHousehold_Baseline_MasklessPairKeepsEachAccountsOwnCategory.
			superseded := supersededSnapshotIndexes(previousSnapshots, previousAccountMap, historicalMap)
			for i, snapshot := range previousSnapshots {
				account, exists := snapshotAccount(snapshot, previousAccountMap, historicalMap)
				if !exists {
					continue
				}
				if superseded[i] {
					continue
				}
				// The same AccountTypes filter the current-period loop above applies.
				if !accountTypeMatchesFilter(account.Type, filters.AccountTypes) {
					continue
				}
				balance := 0.0
				if snapshot.CurrentBalance != nil {
					balance = *snapshot.CurrentBalance
				}
				category := domain.AccountTypeCategory(account.Type)
				absBalance := balance
				if category == "liabilities" && balance < 0 {
					absBalance = -balance
				}
				if category == "assets" {
					prevAssets += absBalance
				} else {
					prevLiabilities += absBalance
				}
			}

			prevNetWorth := prevAssets - prevLiabilities
			changeAmount := netWorth - prevNetWorth
			changePercent := 0.0
			if prevNetWorth != 0 {
				changePercent = (changeAmount / prevNetWorth) * 100
			}
			periodComparison = &domain.NetWorthPeriodComparison{
				PreviousNetWorth:      prevNetWorth,
				PreviousAssets:        prevAssets,
				PreviousLiabilities:   prevLiabilities,
				NetWorthChange:        changeAmount,
				NetWorthChangePercent: changePercent,
			}
		}
	}

	return &domain.NetWorthResponse{
		AsOfDate:         asOfDate,
		AsOfDateDisplay:  domain.FormatCalendarDayUTC(asOfDate),
		NetWorth:         netWorth,
		TotalAssets:      totalAssets,
		TotalLiabilities: totalLiabilities,
		ByCategory:       byCategory,
		PeriodComparison: periodComparison,
	}, nil
}

// GetNetWorthHistory returns time series net worth data for charts
func (s *dashboardService) GetNetWorthHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.NetWorthHistoryResponse, error) {
	// Default to last 30 days if not specified
	endDate := domain.CalendarDayUTC(time.Now())
	startDate := endDate.AddDate(0, 0, -30)

	if filters != nil {
		if filters.StartDate != nil {
			startDate = domain.CalendarDayUTC(*filters.StartDate)
		}
		if filters.EndDate != nil {
			endDate = domain.CalendarDayUTC(*filters.EndDate)
		}
	}

	// Validate date range
	if startDate.After(endDate) {
		return nil, fmt.Errorf("start_date must be before or equal to end_date")
	}

	// Get account types to filter (if specified)
	var accountTypes []string
	if filters != nil && len(filters.AccountTypes) > 0 {
		accountTypes = filters.AccountTypes
	}

	// Get aggregated balance history from repository. Household mode fans out across every
	// member's shared accounts instead of the personal, single-user GetAggregatedHistory.
	var history []domain.BalanceHistoryPoint
	var err error
	if filters != nil && filters.HouseholdScope != nil {
		history, err = s.balanceSnapshotRepo.GetAggregatedHistoryForHousehold(ctx, userID, filters.HouseholdScope, startDate, endDate, accountTypes)
	} else {
		history, err = s.balanceSnapshotRepo.GetAggregatedHistory(ctx, userID, startDate, endDate, accountTypes)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get balance history: %w", err)
	}

	// Convert to net worth history format
	snapshots := make([]domain.NetWorthHistorySnapshot, len(history))
	for i, point := range history {
		var totalAssets, totalLiabilities float64

		// Calculate assets and liabilities from ByType breakdown
		for accountType, balance := range point.ByType {
			category := domain.AccountTypeCategory(accountType)
			absBalance := balance
			if category == "liabilities" && balance < 0 {
				absBalance = -balance
			}

			if category == "assets" {
				totalAssets += absBalance
			} else {
				totalLiabilities += absBalance
			}
		}

		// #1626 Phase 3 item 5: THE UNTYPED REMAINDER, which used to be discarded.
		//
		// This reconstruction is driven ENTIRELY by ByType, and an ORPHANED balance snapshot —
		// accounts row hard-deleted, balance_snapshots.account_id SET NULL — has no account row to
		// take a type from. GetAggregatedHistory counted it toward TotalBalance and omitted it from
		// ByType, so a day whose only rows were orphans reported net worth of EXACTLY ZERO beside a
		// correct balance. That is one of the two ways the user's chart reached a flat zero.
		//
		// The repository now recovers the type from the snapshot's own chain row
		// (user_accounts.identity_type) wherever one survives, which is both the common shape and
		// the only way to get a credit card's SIGN right. What is left here is the residue that
		// recovery cannot reach: a snapshot whose chain row is gone too.
		//
		// SIGN, NOT A DEFAULT TYPE. Routing the remainder through domain.AccountTypeCategory("")
		// would silently call it an asset (its default branch), so an untyped DEBT would IMPROVE
		// net worth. The balance's own sign is the only information left about what it is, so a
		// positive remainder is an asset and a negative one a liability, both at face value.
		//
		if point.UntypedBalance > 0 {
			totalAssets += point.UntypedBalance
		} else if point.UntypedBalance < 0 {
			totalLiabilities += -point.UntypedBalance
		}

		netWorth := totalAssets - totalLiabilities

		snapshots[i] = domain.NetWorthHistorySnapshot{
			Date:             point.Date,
			DateDisplay:      point.Date.Format(time.DateOnly),
			NetWorth:         netWorth,
			TotalAssets:      totalAssets,
			TotalLiabilities: totalLiabilities,
		}
	}

	granularity := "daily"
	if filters != nil && filters.Granularity != "" {
		granularity = filters.Granularity
	}

	return &domain.NetWorthHistoryResponse{
		StartDate:        startDate,
		StartDateDisplay: domain.FormatCalendarDayUTC(startDate),
		EndDate:          endDate,
		EndDateDisplay:   domain.FormatCalendarDayUTC(endDate),
		Granularity:      granularity,
		Snapshots:        snapshots,
	}, nil
}
