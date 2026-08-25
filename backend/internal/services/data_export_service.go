package services

import (
	"context"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// DataExportService produces the GDPR Article 20 / Article 15 export for one user (#1587).
//
// WHY THIS IS SYNCHRONOUS, AND WHAT WOULD CHANGE THAT.
//
// The export is a single request that reads ~49 user-scoped tables and returns one JSON document.
// That is a deliberate choice over the obvious alternative (enqueue a River job, email a download
// link), taken on a reasoned size bound rather than a measurement — there is no production traffic
// to measure yet, and saying so is more useful than a number from a synthetic fixture:
//
//   - The dominant sections are transactions, balance_snapshots and holding_snapshots. Plaid returns
//     at most 24 months of transactions, and the tier limits cap linked accounts. A heavy Premium
//     user at ~5 accounts x ~150 transactions/month x 24 months is ~18,000 transaction rows; daily
//     balance snapshots over the same window are ~3,650 per account. Serialised as jsonb rows at a
//     few hundred bytes each, the whole document lands in the low tens of megabytes.
//   - Everything else is bounded by the tier limits directly (budgets, goals, boosts, rules) or by
//     the number of accounts (liabilities, holdings), and is negligible beside those three.
//
// A response in the tens of megabytes is servable synchronously; it is not comfortable, and it is
// the reason RowCount is part of the payload rather than hidden. ⚠ MOVE THIS TO A RIVER JOB WHEN
// RowCount ROUTINELY EXCEEDS ~250,000, or when p99 latency for this endpoint passes a few seconds —
// whichever happens first. Both are observable from the analytics event this service fires and from
// the endpoint's own latency metric, so the trigger does not depend on anyone remembering to
// re-derive the arithmetic above. The async design is not speculative: internal/queue already runs
// River, and account_deletion.go is the worked example of a user-scoped job.
//
// The synchronous design also avoids the thing the async one would have to solve: a download link is
// a bearer URL to a complete copy of a user's financial life, which needs an expiring token, an
// object store with a lifecycle policy, and an erasure story of its own. Not building that yet is
// part of the value of not building it yet.
type DataExportService interface {
	// ExportUserData returns the authenticated user's own data.
	ExportUserData(ctx context.Context, userID string) (*domain.DataExport, error)

	// SetAnalytics injects the PostHog client (setter injection, per the project convention).
	SetAnalytics(analytics phclient.Analytics)
}

// dataExportService implements DataExportService.
type dataExportService struct {
	repo      repository.DataExportRepository
	analytics phclient.Analytics
}

// NewDataExportService creates a DataExportService.
func NewDataExportService(repo repository.DataExportRepository) DataExportService {
	return &dataExportService{repo: repo}
}

// SetAnalytics injects the analytics client. Nil is fine — phclient is no-op safe.
func (s *dataExportService) SetAnalytics(analytics phclient.Analytics) {
	s.analytics = analytics
}

// ExportUserData builds the export for one user.
//
// The userID is the one the Clerk middleware resolved, never a value from the request body or path:
// there is no way to ask this endpoint for somebody else's export, because it takes no parameter
// that could name somebody else.
func (s *dataExportService) ExportUserData(ctx context.Context, userID string) (*domain.DataExport, error) {
	if userID == "" {
		return nil, fmt.Errorf("ExportUserData: empty user id")
	}

	export, err := s.repo.ExportUserData(ctx, userID)
	if err != nil {
		return nil, err
	}

	// An export is a GDPR request being fulfilled, so it is worth counting: the row count is what
	// tells us when the synchronous design above has run out of road.
	if s.analytics != nil {
		s.analytics.Capture(userID, phclient.EventDataExportGenerated, map[string]interface{}{
			"row_count":      export.RowCount,
			"section_count":  len(export.Sections),
			"export_version": export.ExportVersion,
		})
	}

	return export, nil
}
