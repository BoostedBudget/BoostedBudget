package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The drift guard on plaidOrphanedItemRepository.List's SINGLE-STATEMENT property, asserted without a
// database so it cannot be skipped along with the integration suite — the same reason
// deletionResidueStatsQuery is hoisted and guarded this way in job_health_repository.
//
// WHAT IS ACTUALLY AT STAKE, because "one query instead of two" reads like tidiness and is not.
// List used to run `SELECT COUNT(*)` and then the page as two separate, non-transactional statements.
// Under READ COMMITTED each takes its own snapshot, so a row inserted between them makes the total
// and the page describe different tables. domain.SummarisePlaidOrphanedItemsBounded refuses to
// publish state counts once the total is past domain.PlaidOrphanedItemHealthScanCap, precisely so a
// partial scan is never reported as a total — and a total read BEFORE the page can say exactly the
// cap while the page it is paired with was truncated out of a larger table. The refusal then does not
// fire and the surface publishes a silent UNDER-COUNT of the manual-intervention population, which is
// the one direction a compliance surface must never err in and the exact case the cap exists to
// prevent. `COUNT(*) OVER ()` is evaluated before LIMIT/OFFSET and inside the same statement, so the
// two answers cannot come from different snapshots.
func TestOrphanListQuery_TakesItsTotalFromTheSameStatementAsThePage(t *testing.T) {
	assert.Contains(t, orphanListQuery, "COUNT(*) OVER ()",
		"the total must be a window count riding out on the page's own statement — that is what makes one snapshot serve both")

	// A window count is only the total of the whole result set when it is UNPARTITIONED. `OVER
	// (PARTITION BY ...)` would still compile, still return a number, and silently report a
	// per-group count as the table's size.
	assert.NotContains(t, orphanListQuery, "PARTITION BY",
		"a partitioned window count is a per-group count, not the table total")

	// And the statement it rides on must still be the paged one, or the window would count the page.
	assert.Contains(t, orphanListQuery, "LIMIT $1 OFFSET $2",
		"the window count is computed before LIMIT/OFFSET, which is only true if the LIMIT is on this same statement")

	// The reintroduction this guards against, spelled exactly: any separate aggregate over the table
	// is a second snapshot.
	assert.False(t, strings.Contains(orphanListQuery, "SELECT COUNT(*) FROM"),
		"a separate COUNT is a second statement and therefore a second snapshot — the defect this replaced")

	// Boundedness: the page must still be bounded by the caller's limit. A window total is worthless
	// if the read that produced it was unbounded, and this is the read domain's scan cap bounds.
	require.Contains(t, orphanListQuery, "ORDER BY orphaned_at DESC, id DESC",
		"the total describes the result set the page was cut from, so the ordering that defines that cut must be on the same statement")
}
