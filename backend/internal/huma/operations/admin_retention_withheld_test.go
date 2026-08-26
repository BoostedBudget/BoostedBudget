package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// stubRetentionWithheldReader implements exactly the two reads the handler is allowed to make.
// There is no write to stub, and that absence is the point: #1584's three remedies (re-arm the
// clock, waive the notice, notify out of band) are an undecided product question, so this surface
// must stay a looking surface. A test double with no write method is the cheapest way to make a
// future handler that reaches for one fail to compile.
type stubRetentionWithheldReader struct {
	counts     repository.RetentionWithholdingCounts
	countsErr  error
	page       []repository.RetentionWithheldAccount
	pageErr    error
	gotCursor  *repository.RetentionSweepCursor
	gotLimit   int
	pageCalls  int
	countCalls int
}

func (s *stubRetentionWithheldReader) CountRetentionDeletionsWithheld(context.Context) (repository.RetentionWithholdingCounts, error) {
	s.countCalls++
	return s.counts, s.countsErr
}

func (s *stubRetentionWithheldReader) GetRetentionDeletionsWithheldPage(_ context.Context, after *repository.RetentionSweepCursor, limit int) ([]repository.RetentionWithheldAccount, error) {
	s.pageCalls++
	s.gotCursor, s.gotLimit = after, limit
	return s.page, s.pageErr
}

func ptrTime(t time.Time) *time.Time { return &t }

// TestListAdminRetentionWithheld_ReportsTheWholePopulationBesideThePage pins the relationship that
// makes this surface worth having: the number is the WHOLE backlog, the list is one page of it, and
// a reader can tell which is which. A client that had to infer the total from len(accounts) would
// report "3 accounts withheld" on page one of a hundred.
func TestListAdminRetentionWithheld_ReportsTheWholePopulationBesideThePage(t *testing.T) {
	expired := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sent30 := expired.Add(-30 * 24 * time.Hour)

	reader := &stubRetentionWithheldReader{
		counts: repository.RetentionWithholdingCounts{WithheldNoticeMissing: 137, NoticePending: 4200},
		page: []repository.RetentionWithheldAccount{
			{UserID: "u1", SubscriptionStatus: "cancelled", RetentionExpiresAt: expired, HasEmailOnFile: true},
			{UserID: "u2", SubscriptionStatus: "inactive", RetentionExpiresAt: expired, Notice30dSentAt: ptrTime(sent30), HasEmailOnFile: false},
		},
	}
	h := (&AdminHandler{}).WithRetentionWithheldReader(reader)

	out, err := h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err)

	assert.Equal(t, 137, out.Body.WithheldTotal,
		"withheld_total must be the whole population, not this page's length — an operator sizing the backlog from one page would under-report it")
	assert.Equal(t, 4200, out.Body.NoticePending,
		"the healthy population is reported beside the stuck one on purpose: 137 stuck out of 4337 reads very differently from 137 out of 137")
	require.Len(t, out.Body.Accounts, 2)
	assert.Nil(t, out.Body.NextCursor,
		"a page shorter than the limit is the end of the walk, so there must be no next cursor to follow")
}

// TestListAdminRetentionWithheld_ReportsTheAddresslessReasonPerAccount is the fourth withholding
// reason reaching a surface for the first time. retention_deletions_withheld provably cannot carry
// it (its label would need a SQL predicate over ciphertext), so if this field is dropped or
// constant-folded nothing anywhere reports it.
func TestListAdminRetentionWithheld_ReportsTheAddresslessReasonPerAccount(t *testing.T) {
	expired := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reader := &stubRetentionWithheldReader{
		page: []repository.RetentionWithheldAccount{
			{UserID: "has-address", RetentionExpiresAt: expired, HasEmailOnFile: true},
			{UserID: "no-address", RetentionExpiresAt: expired, HasEmailOnFile: false},
		},
	}
	h := (&AdminHandler{}).WithRetentionWithheldReader(reader)

	out, err := h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err)
	require.Len(t, out.Body.Accounts, 2)

	assert.True(t, out.Body.Accounts[0].HasEmailOnFile,
		"an account with an address must report true — a constant-false field reports nothing at all")
	assert.False(t, out.Body.Accounts[1].HasEmailOnFile,
		"an addressless account needs a DIFFERENT remedy from every other withheld account, and this is the only surface anywhere that can say so")
}

// TestListAdminRetentionWithheld_NullNoticeMarkersSerialiseAsNull. A marker's ABSENCE is the whole
// signal here, so it must reach the client as null. A zero time.Time formatted into the body reads
// as the date 0001-01-01, which a client renders as a notice that WAS sent — the exact inversion of
// the fact this endpoint exists to report.
func TestListAdminRetentionWithheld_NullNoticeMarkersSerialiseAsNull(t *testing.T) {
	expired := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sent7 := expired.Add(-7 * 24 * time.Hour)
	reader := &stubRetentionWithheldReader{
		page: []repository.RetentionWithheldAccount{
			{UserID: "u1", RetentionExpiresAt: expired, Notice7dSentAt: ptrTime(sent7)},
		},
	}
	h := (&AdminHandler{}).WithRetentionWithheldReader(reader)

	out, err := h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err)
	require.Len(t, out.Body.Accounts, 1)

	assert.Nil(t, out.Body.Accounts[0].Notice30dSentAt,
		"an unrecorded 30-day notice must serialise as null, never as a zero date a client would render as 'sent'")
	require.NotNil(t, out.Body.Accounts[0].Notice7dSentAt)
	assert.Equal(t, "2025-12-25T00:00:00Z", *out.Body.Accounts[0].Notice7dSentAt,
		"a recorded marker must round-trip as its real instant, or the null case above is satisfied by a field that is always null")
}

// TestListAdminRetentionWithheld_FullPageYieldsAFollowableCursor pins both directions of the keyset
// walk, because a cursor that is always nil and a cursor that is never nil both pass a one-sided
// test — one truncates the backlog silently, the other never terminates.
func TestListAdminRetentionWithheld_FullPageYieldsAFollowableCursor(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	reader := &stubRetentionWithheldReader{
		page: []repository.RetentionWithheldAccount{
			{UserID: "u1", RetentionExpiresAt: older},
			{UserID: "u2", RetentionExpiresAt: newer},
		},
	}
	h := (&AdminHandler{}).WithRetentionWithheldReader(reader)

	out, err := h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{Limit: 2})
	require.NoError(t, err)
	require.NotNil(t, out.Body.NextCursor, "a FULL page may have more behind it, so the walk must be continuable")
	assert.Equal(t, "u2", out.Body.NextCursor.UserID,
		"the cursor must be the LAST row of the page — taking any other row re-reads or skips part of the backlog")
	assert.Equal(t, "2026-02-01T00:00:00Z", out.Body.NextCursor.ExpiresAt)

	// And the cursor a client sends back is the cursor the repository receives.
	_, err = h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{
		Limit:           2,
		CursorExpiresAt: out.Body.NextCursor.ExpiresAt,
		CursorUserID:    out.Body.NextCursor.UserID,
	})
	require.NoError(t, err)
	require.NotNil(t, reader.gotCursor, "the parsed cursor must reach the repository, or every page returns page one")
	assert.Equal(t, "u2", reader.gotCursor.UserID)
	assert.True(t, reader.gotCursor.ExpiresAt.Equal(newer))
	assert.Equal(t, 2, reader.gotLimit)
}

// TestListAdminRetentionWithheld_RefusesHalfACursor. A keyset position is the PAIR; half of one is
// not a smaller request, it is a silently different page. Refusing is the only honest answer.
func TestListAdminRetentionWithheld_RefusesHalfACursor(t *testing.T) {
	reader := &stubRetentionWithheldReader{}
	h := (&AdminHandler{}).WithRetentionWithheldReader(reader)

	_, err := h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{
		Limit: 50, CursorExpiresAt: "2026-01-01T00:00:00Z",
	})
	require.Error(t, err)
	assert.Equal(t, 0, reader.pageCalls, "a half cursor must not reach the repository at all")

	_, err = h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{
		Limit: 50, CursorUserID: "1c8e1f6e-0000-4000-8000-000000000000",
	})
	require.Error(t, err)
	assert.Equal(t, 0, reader.pageCalls)

	_, err = h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{
		Limit: 50, CursorExpiresAt: "not-a-timestamp", CursorUserID: "1c8e1f6e-0000-4000-8000-000000000000",
	})
	require.Error(t, err)
	assert.Equal(t, 0, reader.pageCalls, "an unparseable cursor must be refused, not silently treated as no cursor — that would restart the walk at page one forever")
}

// TestListAdminRetentionWithheld_UnavailableAndFailingReads. An unwired dependency is 503, and a
// failed read is an error rather than an empty list: reporting zero withheld accounts on the
// strength of a query that failed is the unknown-is-not-zero mistake the retention_deletions_withheld
// gauge deliberately avoids, and it would be worse here, where a human acts on the answer.
func TestListAdminRetentionWithheld_UnavailableAndFailingReads(t *testing.T) {
	_, err := (&AdminHandler{}).ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.Error(t, err, "an unwired reader must be 503, never an empty list that reads as 'nothing is stuck'")

	countFail := &stubRetentionWithheldReader{countsErr: errors.New("boom")}
	_, err = (&AdminHandler{}).WithRetentionWithheldReader(countFail).
		ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.Error(t, err)

	pageFail := &stubRetentionWithheldReader{pageErr: errors.New("boom")}
	_, err = (&AdminHandler{}).WithRetentionWithheldReader(pageFail).
		ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.Error(t, err)
}

// TestListAdminRetentionWithheld_DaysWithheldIsTheTriageOrdering. days_withheld is computed
// server-side against ONE as_of instant so every row in a response agrees about where the boundary
// is — the coupling #1466 threaded asOf everywhere to remove.
func TestListAdminRetentionWithheld_DaysWithheldIsTheTriageOrdering(t *testing.T) {
	reader := &stubRetentionWithheldReader{
		page: []repository.RetentionWithheldAccount{
			{UserID: "recent", RetentionExpiresAt: time.Now().UTC().Add(-3 * 24 * time.Hour)},
			{UserID: "ancient", RetentionExpiresAt: time.Now().UTC().Add(-400 * 24 * time.Hour)},
		},
	}
	h := (&AdminHandler{}).WithRetentionWithheldReader(reader)

	out, err := h.ListAdminRetentionWithheld(context.Background(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err)
	require.Len(t, out.Body.Accounts, 2)

	assert.Equal(t, 3, out.Body.Accounts[0].DaysWithheld)
	assert.Equal(t, 400, out.Body.Accounts[1].DaysWithheld,
		"an account stuck for over a year must report it — a constant or a page-index would make the triage ordering meaningless")
	assert.NotEmpty(t, out.Body.AsOf, "the instant the numbers were computed for must be echoed, or a reader cannot tell how stale they are")
}
