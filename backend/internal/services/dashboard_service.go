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

// buildChainAwareAccountMap builds a map keyed by both account.ID and account.UserAccountID
// so that snapshots carrying a replaced account_id still resolve via the chain key (Issue #1109).
// baselineBalanceForComparison sums the 30-day-ago snapshots using the SAME account universe and
// type filter the current total uses, so the period comparison is apples-to-apples.
//
// Issue #1537 round 9. Both callers previously summed every snapshot they were handed, while their
// current-balance loops applied a chain-aware accountMap lookup AND the AccountTypes filter. With a
// type filter active the baseline therefore included accounts the current total excluded, and
// ChangeAmount/ChangePercent were arbitrary — a measured +50% was reported as +150%.
//
// dedupChains reflects a real difference between the two callers, not an oversight: the personal path
// can receive several snapshots per logical account on a re-link day (it sorts DESC first and keeps
// the latest per chain), while the household path is already one row per account.
//
// KNOWN APPROXIMATION, stated rather than left to be discovered: accountMap is built as of NOW, and
// is applied here to snapshots from 30 days ago. An account that has since disappeared from the map
// drops out of the baseline. That is deliberate — the current total is computed over exactly this
// universe, so using it on both sides is what makes the two comparable. The alternative (a second,
// as-of-then account map) is what the comment above getBalanceSummaryHousehold's snapshot fetch was
// avoiding when it justified the raw sum, and it would reintroduce the mismatch it was avoiding.
func baselineBalanceForComparison(
	snapshots []domain.BalanceSnapshot,
	accountMap map[string]domain.Account,
	accountTypes []string,
	dedupChains bool,
) float64 {
	var total float64
	seenChainKeys := make(map[string]bool)
	for _, snapshot := range snapshots {
		key := snapshotChainKey(snapshot)
		if dedupChains {
			if seenChainKeys[key] {
				continue
			}
			seenChainKeys[key] = true
		}
		account, exists := accountMap[key]
		if !exists {
			continue
		}
		if len(accountTypes) > 0 {
			typeMatch := false
			for _, filterType := range accountTypes {
				if account.Type == filterType {
					typeMatch = true
					break
				}
			}
			if !typeMatch {
				continue
			}
		}
		if snapshot.CurrentBalance != nil {
			total += *snapshot.CurrentBalance
		}
	}
	return total
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

	// Get latest snapshots for each account
	snapshots, err := s.balanceSnapshotRepo.GetLatestByUserID(ctx, userID)
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

	previousSnapshots, err := s.balanceSnapshotRepo.GetByUserIDAndDateRange(ctx, userID, previousDate, previousDate)
	if err == nil && len(previousSnapshots) > 0 {
		// Sort descending so the dedup below keeps the LATEST snapshot per chain key.
		// The repo returns ASC; on a re-link day both replaced and active rows may exist.
		sort.Slice(previousSnapshots, func(i, j int) bool {
			return previousSnapshots[i].SnapshotAt.After(previousSnapshots[j].SnapshotAt)
		})
		var accountTypes []string
		if filters != nil {
			accountTypes = filters.AccountTypes
		}
		previousBalance := baselineBalanceForComparison(previousSnapshots, accountMap, accountTypes, true)

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

// getBalanceSummaryHousehold is the household-scope variant of GetBalanceSummary. It MUST
// NOT call GetLatestByUserID/GetByUserID — those are single-user and would silently drop
// every non-requester member's accounts from the aggregate (Codex adversarial review,
// P0-2). GetSnapshotsAsOfForHousehold/GetForHouseholdScope already dedup joint accounts
// (freshest-synced copy) and resolve re-link chains via the stable user_account_id, so —
// unlike the personal path — no further chain-key dedup loop is needed here.
//
// The "current" snapshot fetch below intentionally uses time.Now(), not asOfDate — this
// mirrors the PERSONAL path's existing behavior exactly (GetLatestByUserID has no date
// parameter at all; filters.AsOfDate only ever affects the response label and the
// previous-period baseline, never which snapshot counts as "current"). Codex adversarial
// review flagged this as inconsistent with the AsOfDate param, but making household MORE
// precise than personal here would itself be a scope-dependent behavioral divergence —
// out of scope for this phase; see plan decision notes.
func (s *dashboardService) getBalanceSummaryHousehold(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error) {
	scope := filters.HouseholdScope
	asOfDate := time.Now().UTC().Truncate(24 * time.Hour)
	if filters.AsOfDate != nil {
		asOfDate = filters.AsOfDate.Truncate(24 * time.Hour)
	}

	// currentCutoff is captured once and reused for both calls below — two independent
	// time.Now().UTC() calls could straddle a sync landing in between, letting the snapshot
	// query and the account-map query resolve a joint account's fingerprint winner to
	// different members' copies (the same class of bug fixed for the previous-period pair
	// in #1369 gap-closure G1; flagged by Codex adversarial review on the G1 fix itself).
	currentCutoff := time.Now().UTC()
	snapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForHousehold(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household balance snapshots: %w", err)
	}
	// One "now"-asOf account map serves both the current total and the previous-period baseline
	// (unlike getNetWorthHousehold, which needs a map per point in time) — #1369 gap-closure G1.
	//
	// ⚠ Corrected in #1537 round 9. This comment used to say the comparison "sums snapshots directly
	// without any accountMap lookup", which was true and was a bug: the baseline then included
	// accounts the filtered current total excluded, making ChangePercent arbitrary under a type
	// filter. The baseline now goes through baselineBalanceForComparison with this same map, which is
	// what keeps one map sufficient AND the two sides comparable.
	accounts, err := s.accountRepo.GetForHouseholdScope(ctx, userID, scope, currentCutoff)
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
	previousSnapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForHousehold(ctx, userID, scope, endOfDay(previousDate))
	if err == nil && len(previousSnapshots) > 0 {
		previousBalance := baselineBalanceForComparison(previousSnapshots, accountMap, filters.AccountTypes, false)
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

	// Get latest snapshots for each account
	snapshots, err := s.balanceSnapshotRepo.GetLatestByUserID(ctx, userID)
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

	previousSnapshots, err := s.balanceSnapshotRepo.GetByUserIDAndDateRange(ctx, userID, previousDate, previousDate)
	if err == nil && len(previousSnapshots) > 0 {
		// Sort descending so the dedup below keeps the LATEST snapshot per chain key.
		sort.Slice(previousSnapshots, func(i, j int) bool {
			return previousSnapshots[i].SnapshotAt.After(previousSnapshots[j].SnapshotAt)
		})
		var prevAssets, prevLiabilities float64
		seenChainKeys := make(map[string]bool)

		for _, snapshot := range previousSnapshots {
			key := snapshotChainKey(snapshot)
			if seenChainKeys[key] {
				continue // Dedup: count each logical account (chain) only once
			}
			seenChainKeys[key] = true

			account, exists := accountMap[key]
			if !exists {
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
// getBalanceSummaryHousehold, it uses GetSnapshotsAsOfForHousehold/GetForHouseholdScope
// (never the single-user GetLatestByUserID/GetByUserID — Codex adversarial review, P0-2)
// and needs no chain-key dedup loop since those methods already collapse joint accounts
// and re-link chains to one row each. The "current" fetch uses time.Now(), matching the
// personal path's AsOfDate handling exactly — see getBalanceSummaryHousehold's comment.
func (s *dashboardService) getNetWorthHousehold(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.NetWorthResponse, error) {
	scope := filters.HouseholdScope
	asOfDate := time.Now().UTC().Truncate(24 * time.Hour)
	if filters.AsOfDate != nil {
		asOfDate = filters.AsOfDate.Truncate(24 * time.Hour)
	}

	// currentCutoff is captured once and reused for both calls below — see the identical
	// comment in getBalanceSummaryHousehold for why two independent time.Now().UTC() calls
	// would be unsafe here (Codex adversarial review on the G1 fix).
	currentCutoff := time.Now().UTC()
	snapshots, err := s.balanceSnapshotRepo.GetSnapshotsAsOfForHousehold(ctx, userID, scope, currentCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get household balance snapshots: %w", err)
	}
	currentAccounts, err := s.accountRepo.GetForHouseholdScope(ctx, userID, scope, currentCutoff)
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
		// the current-period loop. GetForHouseholdScope's joint-account fingerprint winner
		// depends on asOf — if a joint account's globally-freshest copy has no snapshot
		// before previousCutoff, the "now" accountMap only knows about that (wrong-for-this-
		// period) copy's user_account_id, and the lookup below would silently drop the
		// account instead of resolving to the other member's genuinely earlier copy.
		previousAccounts, acctErr := s.accountRepo.GetForHouseholdScope(ctx, userID, scope, previousCutoff)
		if acctErr == nil {
			previousAccountMap := buildChainAwareAccountMap(previousAccounts)
			var prevAssets, prevLiabilities float64
			for _, snapshot := range previousSnapshots {
				account, exists := previousAccountMap[snapshotChainKey(snapshot)]
				if !exists {
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
