package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

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
// Dedup keys must come from here, not from snapshotChainKey, once a snapshot has been resolved to an
// account. balance_snapshots.user_account_id is NULLABLE, its backfill was one-shot
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
	seenChainKeys := make(map[string]bool)
	for _, snapshot := range snapshots {
		key := snapshotChainKey(snapshot)
		account, exists := activeAccounts[key]
		if !exists {
			account, exists = historicalAccounts[key]
		}
		if !exists {
			continue
		}
		// Dedup AFTER resolution, on the resolved account's chain key — see accountChainKey for why
		// the snapshot's own key double-counts a chain with a legacy NULL user_account_id row. The
		// caller sorted DESC, so the row that survives is the chain's LATEST.
		if dedupChains {
			canonicalKey := accountChainKey(account)
			if seenChainKeys[canonicalKey] {
				continue
			}
			seenChainKeys[canonicalKey] = true
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

// hasSnapshotOutsideAccountMap reports whether any snapshot's chain key is absent from accountMap.
// It gates the extra all-statuses account read behind the only case that needs it: a chain with no
// active member left. A chain that was merely re-linked still resolves through the chain-aware map's
// user_account_id key, so the common path keeps its single account query and its single round trip.
func hasSnapshotOutsideAccountMap(snapshots []domain.BalanceSnapshot, accountMap map[string]domain.Account) bool {
	for _, snapshot := range snapshots {
		if _, exists := accountMap[snapshotChainKey(snapshot)]; !exists {
			return true
		}
	}
	return false
}

func buildChainAwareAccountMap(accounts []domain.Account) map[string]domain.Account {
	m := make(map[string]domain.Account, len(accounts)*2)
	for _, account := range accounts {
		m[account.ID] = account
		if account.UserAccountID != nil && *account.UserAccountID != "" {
			m[*account.UserAccountID] = account
		}
	}
	return m
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

// GetBalanceSummary returns current balance overview with breakdown by account type
func (s *dashboardService) GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error) {
	if filters != nil && filters.HouseholdScope != nil {
		return s.getBalanceSummaryHousehold(ctx, userID, filters)
	}
	// Determine the date for balance snapshot
	asOfDate := time.Now().UTC().Truncate(24 * time.Hour)
	if filters != nil && filters.AsOfDate != nil {
		asOfDate = filters.AsOfDate.Truncate(24 * time.Hour)
	}

	// Both sides of this response are anchored on asOfDate: the current figures are the latest
	// snapshot per chain AT OR BEFORE the end of that day, and the baseline (below) is the same
	// read 30 days earlier. Before #1537 the current figures came from the unbounded
	// GetLatestByUserID while only the baseline moved with as_of_date, so a request for a past
	// date returned TODAY's total labelled with that date and compared it against a 30-days-before
	// -that-date baseline — a change over an interval nobody asked for.
	snapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForUser(ctx, userID, endOfDay(asOfDate))
	if err != nil {
		return nil, fmt.Errorf("failed to get balance snapshots: %w", err)
	}

	// Get accounts to map account IDs to account details (GetByUserID filters to active only)
	accounts, err := s.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	accountMap := buildChainAwareAccountMap(accounts)

	// Group snapshots by account type
	byType := make(map[string]*domain.AccountTypeBalance)
	var totalBalance float64

	for _, snapshot := range snapshots {
		account, exists := accountMap[snapshotChainKey(snapshot)]
		if !exists {
			continue // Skip if account not found
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
	previousSnapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForUser(ctx, userID, previousCutoff)
	if err == nil && len(previousSnapshots) > 0 {
		// Sort descending so the dedup below keeps the LATEST snapshot per chain key.
		// The repo already returns one row per chain, but a re-link chain can still surface twice
		// here when a legacy NULL-user_account_id row keys on its raw account_id (see
		// baselineBalanceForComparison), and those two rows carry different instants.
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
				AsOfDate:     asOfDate,
				TotalBalance: totalBalance,
				ByType:       byType,
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
// re-linked chain resolves there through its user_account_id key. That short-circuit is why the
// ordinary dashboard request still issues exactly one account query.
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
// IncludeHidden is true to match GetByUserID, which applies no is_hidden predicate at all —
// a baseline source that defaults to hiding hidden accounts would silently change which accounts
// count toward it.
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
	accounts, err := s.accountRepo.GetAccountsWithInstitutionFiltered(ctx, userID, repository.AccountFilterOptions{
		AsOf:          &asOf,
		IncludeHidden: true,
	})
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
	asOfDate := time.Now().UTC().Truncate(24 * time.Hour)
	if filters.AsOfDate != nil {
		asOfDate = filters.AsOfDate.Truncate(24 * time.Hour)
	}

	// currentCutoff is derived from asOfDate, so the snapshot query and the account-map query
	// below necessarily agree on which member's copy wins a joint account's fingerprint. Two
	// independent time.Now().UTC() calls — what this used to be — could straddle a sync landing in
	// between and resolve that winner differently (the same class of bug fixed for the
	// previous-period pair in #1369 gap-closure G1; flagged by Codex adversarial review on the G1
	// fix itself). Deriving it from asOfDate makes the agreement structural rather than a
	// convention one edit could break.
	currentCutoff := endOfDay(asOfDate)
	snapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForHousehold(ctx, userID, scope, currentCutoff)
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
	accounts, err := s.accountRepo.GetForHouseholdScopeActiveNow(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household accounts: %w", err)
	}
	accountMap := buildChainAwareAccountMap(accounts)

	byType := make(map[string]*domain.AccountTypeBalance)
	var totalBalance float64
	for _, snapshot := range snapshots {
		account, exists := accountMap[snapshotChainKey(snapshot)]
		if !exists {
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
	previousSnapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForHousehold(ctx, userID, scope, previousCutoff)
	if err == nil && len(previousSnapshots) > 0 {
		historicalMap := map[string]domain.Account{}
		if hasSnapshotOutsideAccountMap(previousSnapshots, accountMap) {
			historicalAccounts, histErr := s.accountRepo.GetForHouseholdScopeLiveAsOf(ctx, userID, scope, previousCutoff)
			if histErr != nil {
				// Omit the comparison rather than compute it over the wrong universe — see the
				// personal path's identical guard for why silence beats a false "no change".
				return &domain.BalanceSummaryResponse{
					AsOfDate:     asOfDate,
					TotalBalance: totalBalance,
					ByType:       byType,
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
	endDate := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := endDate.AddDate(0, 0, -30)

	if filters != nil {
		if filters.StartDate != nil {
			startDate = filters.StartDate.Truncate(24 * time.Hour)
		}
		if filters.EndDate != nil {
			endDate = filters.EndDate.Truncate(24 * time.Hour)
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
			Date:    point.Date,
			Balance: point.TotalBalance,
			ByType:  point.ByType,
		}
	}

	granularity := "daily"
	if filters != nil && filters.Granularity != "" {
		granularity = filters.Granularity
	}

	return &domain.BalanceHistoryResponse{
		StartDate:   startDate,
		EndDate:     endDate,
		Granularity: granularity,
		Snapshots:   snapshots,
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

	for _, snapshot := range snapshots {
		account, exists := accountMap[snapshotChainKey(snapshot)]
		if !exists {
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
	asOfDate := time.Now().UTC().Truncate(24 * time.Hour)
	if filters != nil && filters.AsOfDate != nil {
		asOfDate = filters.AsOfDate.Truncate(24 * time.Hour)
	}

	// as_of_date anchors BOTH sides here too — see GetBalanceSummary's identical read.
	snapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForUser(ctx, userID, endOfDay(asOfDate))
	if err != nil {
		return nil, fmt.Errorf("failed to get balance snapshots: %w", err)
	}

	// Get accounts to map account IDs to account details
	accounts, err := s.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts: %w", err)
	}

	accountMap := buildChainAwareAccountMap(accounts)

	// Initialize category structure
	byCategory := make(map[string]*domain.NetWorthCategoryBalance)
	var totalAssets, totalLiabilities float64

	for _, snapshot := range snapshots {
		account, exists := accountMap[snapshotChainKey(snapshot)]
		if !exists {
			continue // Skip if account not found
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
	previousSnapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForUser(ctx, userID, previousCutoff)
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
				NetWorth:         netWorth,
				TotalAssets:      totalAssets,
				TotalLiabilities: totalLiabilities,
				ByCategory:       byCategory,
			}, nil
		}

		var prevAssets, prevLiabilities float64
		seenChainKeys := make(map[string]bool)

		for _, snapshot := range previousSnapshots {
			key := snapshotChainKey(snapshot)
			// Active map first, historical map as fallback — the same precedence
			// baselineBalanceForComparison uses.
			account, exists := accountMap[key]
			if !exists {
				account, exists = historicalMap[key]
			}
			if !exists {
				continue
			}
			// Dedup: count each logical account (chain) only once, keyed on the RESOLVED account so
			// that a legacy NULL-user_account_id row and its post-relink sibling collapse — see
			// accountChainKey. previousSnapshots is sorted DESC above, so the latest row wins.
			canonicalKey := accountChainKey(account)
			if seenChainKeys[canonicalKey] {
				continue
			}
			seenChainKeys[canonicalKey] = true
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
	asOfDate := time.Now().UTC().Truncate(24 * time.Hour)
	if filters.AsOfDate != nil {
		asOfDate = filters.AsOfDate.Truncate(24 * time.Hour)
	}

	// currentCutoff is derived from asOfDate and reused for both calls below — see the identical
	// comment in getBalanceSummaryHousehold for why two independent time.Now().UTC() calls
	// would be unsafe here (Codex adversarial review on the G1 fix).
	currentCutoff := endOfDay(asOfDate)
	snapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForHousehold(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household balance snapshots: %w", err)
	}
	currentAccounts, err := s.accountRepo.GetForHouseholdScopeActiveNow(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household accounts: %w", err)
	}
	accountMap := buildChainAwareAccountMap(currentAccounts)

	byCategory := make(map[string]*domain.NetWorthCategoryBalance)
	var totalAssets, totalLiabilities float64

	for _, snapshot := range snapshots {
		account, exists := accountMap[snapshotChainKey(snapshot)]
		if !exists {
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
	previousSnapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForHousehold(ctx, userID, scope, previousCutoff)
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
		previousAccounts, acctErr := s.accountRepo.GetForHouseholdScopeActiveNow(ctx, userID, scope, previousCutoff)
		if acctErr == nil {
			previousAccountMap := buildChainAwareAccountMap(previousAccounts)
			// Gated on the PREVIOUS-cutoff map, not the "now" map: a chain that merely re-linked
			// resolves there already, so the extra query stays conditional on the only case that
			// needs it — a chain with no active member left.
			historicalMap := map[string]domain.Account{}
			if hasSnapshotOutsideAccountMap(previousSnapshots, previousAccountMap) {
				historicalAccounts, histErr := s.accountRepo.GetForHouseholdScopeLiveAsOf(ctx, userID, scope, previousCutoff)
				if histErr != nil {
					// Omit the comparison rather than compute it over the wrong universe — see the
					// personal path's identical guard.
					return &domain.NetWorthResponse{
						AsOfDate:         asOfDate,
						NetWorth:         netWorth,
						TotalAssets:      totalAssets,
						TotalLiabilities: totalLiabilities,
						ByCategory:       byCategory,
					}, nil
				}
				historicalMap = buildChainAwareAccountMap(historicalAccounts)
			}
			var prevAssets, prevLiabilities float64
			for _, snapshot := range previousSnapshots {
				key := snapshotChainKey(snapshot)
				account, exists := previousAccountMap[key]
				if !exists {
					account, exists = historicalMap[key]
				}
				if !exists {
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
	endDate := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := endDate.AddDate(0, 0, -30)

	if filters != nil {
		if filters.StartDate != nil {
			startDate = filters.StartDate.Truncate(24 * time.Hour)
		}
		if filters.EndDate != nil {
			endDate = filters.EndDate.Truncate(24 * time.Hour)
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

		netWorth := totalAssets - totalLiabilities

		snapshots[i] = domain.NetWorthHistorySnapshot{
			Date:             point.Date,
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
		StartDate:   startDate,
		EndDate:     endDate,
		Granularity: granularity,
		Snapshots:   snapshots,
	}, nil
}
