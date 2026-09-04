package services

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// AccountBalanceHistoryService reads ONE account's retained day-by-day balances (Issue #1569).
//
// It exists because privacy policy §8.2a promises a removed linked account's "transaction,
// balance, and holdings history is retained ... so you can still view it", and the BALANCE third
// of that sentence was false: the rows sat in balance_snapshots, written by every sync, and the
// only per-account read was GetLatestByAccountID — a single last-known figure, not a history.
// getBalanceHistory takes account_types and never an account_id, so it could not answer for one
// account either. The day-by-day rows were reachable only through the GDPR export.
//
// ⚠ HISTORICAL UNIVERSE. Nothing here applies a liveness filter, and nothing here may be folded
// into a current-state read. The service is deliberately separate from DashboardService (which
// owns the aggregated, current-state-scoped series) for the same reason #1569 T2 gave a deleted
// goal's ledger its own route: the current-state path stays structurally incapable of returning a
// closed account's figures.
type AccountBalanceHistoryService interface {
	// GetAccountBalanceHistory returns the account's retained series, oldest day first. start and
	// end are inclusive UTC calendar-day bounds; either may be nil for unbounded.
	//
	// An account with nothing retained is NOT an error — it returns an empty series. "We kept
	// nothing for this account" is a real answer and the caller must be able to say it without
	// rendering a failure.
	GetAccountBalanceHistory(ctx context.Context, userID, accountID string, start, end *time.Time) (*domain.AccountBalanceHistory, error)
}

type accountBalanceHistoryService struct {
	snapshotRepo repository.BalanceSnapshotRepository
}

func NewAccountBalanceHistoryService(snapshotRepo repository.BalanceSnapshotRepository) AccountBalanceHistoryService {
	return &accountBalanceHistoryService{snapshotRepo: snapshotRepo}
}

func (s *accountBalanceHistoryService) GetAccountBalanceHistory(
	ctx context.Context, userID, accountID string, start, end *time.Time,
) (*domain.AccountBalanceHistory, error) {
	points, err := s.snapshotRepo.GetAccountBalanceHistory(ctx, userID, accountID, start, end)
	if err != nil {
		return nil, err
	}

	out := &domain.AccountBalanceHistory{
		AccountID: accountID,
		// Never nil, even when the repository hands back nil: the JSON contract is `points: []`,
		// and a null there is indistinguishable from a field the client forgot to read.
		Points: []domain.AccountBalanceHistoryPoint{},
	}
	if len(points) == 0 {
		return out, nil
	}
	out.Points = points

	// Pre-computed bounds, taken from the ENDS of a series the repository guarantees is ordered
	// oldest-first. Deliberately not min/max over the slice: if the ordering ever broke, deriving
	// the bounds independently would hide it behind a still-plausible answer, whereas taking the
	// ends surfaces it.
	first := points[0].Date
	last := points[len(points)-1].Date
	out.FirstDate = &first
	out.LastDate = &last
	return out, nil
}
