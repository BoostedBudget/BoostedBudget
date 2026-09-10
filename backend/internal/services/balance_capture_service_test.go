package services

// Tests for the periodic balance-capture service (#1626 Phase 3, Defect 3 / Task G) — the
// gap-filler that guarantees every active account has a balance snapshot for today.
//
// WHAT THIS SERVICE IS FOR. balance_snapshots is what the net-worth and balance-history charts read,
// and until this change it was written ONLY as a side effect of a transaction sync — which runs on
// initial link and on an inbound Plaid TRANSACTIONS webhook, and nowhere else. A day with no webhook
// was a day with no row, permanently, because no backfill exists and none is being built.
//
// THE COST PROPERTY IS A TESTED PROPERTY, NOT A CLAIM. The design's whole justification is that it
// makes ZERO incremental Plaid calls for an item a webhook has already covered today. Every test
// below counts real HTTP requests against an httptest server rather than trusting a mock's
// expectations, because "the mock was not called" and "no request left the process" are different
// assertions and only the second one is the one the cost argument needs.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
)

// countingAccountsGetServer serves one /accounts/get response and counts how many requests it
// actually received.
//
// ⚠ The COUNTER is the point, and it is why this exists beside plaidAccountsGetServer rather than
// reusing it. T3's assertion is "no Plaid call was made", and the only honest form of that is a
// server that would have observed one.
func countingAccountsGetServer(t *testing.T, accounts []map[string]any) (*plaid.APIClient, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/get", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts":   accounts,
			"item":       map[string]any{"item_id": "item-1", "institution_id": "ins_test", "webhook": "", "available_products": []string{}, "billed_products": []string{}, "update_type": "background"},
			"request_id": "req-daily-capture-test",
		})
	})
	// ⚠ Any OTHER Plaid path is a hard failure. /accounts/balance/get in particular must never be
	// reached: it forces an institution read and carries per-call Balance-product billing, and it
	// appears nowhere in this tree. A 404 would have let a wrong endpoint look like a network error.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the daily capture called an unexpected Plaid endpoint %q — only /accounts/get is "+
			"permitted, and /accounts/balance/get is billed per call", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cfg := plaid.NewConfiguration()
	cfg.UseEnvironment(plaid.Environment(server.URL))
	return plaid.NewAPIClient(cfg), &calls
}

// captureAccountJSON is plaidAccountJSON with the balances spelled out, so a test can express
// current-without-available and NULL cases the shared helper cannot.
func captureAccountJSON(plaidAccountID string, current, available any) map[string]any {
	return map[string]any{
		"account_id":    plaidAccountID,
		"name":          plaidAccountID,
		"type":          "depository",
		"subtype":       "checking",
		"mask":          "0000",
		"official_name": plaidAccountID,
		"balances": map[string]any{
			"current":                  current,
			"available":                available,
			"iso_currency_code":        "USD",
			"unofficial_currency_code": nil,
			"limit":                    nil,
		},
	}
}

type captureHarness struct {
	svc        BalanceCaptureService
	items      *MockLinkedItemRepository
	accounts   *MockAccountRepository
	snapshots  *MockBalanceSnapshotRepository
	plaid      *MockPlaidService
	plaidCalls *atomic.Int64
}

// newCaptureHarness wires the service against mocks and a counting Plaid server.
func newCaptureHarness(t *testing.T, item *domain.LinkedItem, plaidAccounts []map[string]any) *captureHarness {
	t.Helper()
	h := &captureHarness{
		items:     &MockLinkedItemRepository{},
		accounts:  &MockAccountRepository{},
		snapshots: &MockBalanceSnapshotRepository{},
		plaid:     &MockPlaidService{},
	}
	client, calls := countingAccountsGetServer(t, plaidAccounts)
	h.plaidCalls = calls
	// .Maybe() throughout: a test whose whole point is that NOTHING was called must not be forced
	// to declare the calls it forbids.
	h.plaid.On("GetClient").Return(client).Maybe()
	h.items.On("GetByID", mock.Anything, item.ID).Return(item, nil).Maybe()

	h.svc = NewBalanceCaptureService(BalanceCaptureServiceDeps{
		LinkedItemRepo:      h.items,
		AccountRepo:         h.accounts,
		BalanceSnapshotRepo: h.snapshots,
		PlaidService:        h.plaid,
		Logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return h
}

func activeAccount(id, plaidID string) domain.Account {
	return domain.Account{
		ID: id, LinkedItemID: "item-1", UserID: "user-1",
		PlaidAccountID: plaidID, Status: domain.AccountStatusActive, Type: "depository",
	}
}

func liveItem() *domain.LinkedItem {
	return &domain.LinkedItem{ID: "item-1", UserID: "user-1", Status: domain.LinkedItemStatusActive}
}

// ── T3: the cost property ─────────────────────────────────────────────────────────────────────

// TestCaptureItemBalances_FullCohortSkipsWithZeroPlaidCalls is T3, and it is the test the entire
// cadence decision rests on.
//
// The design is a GAP FILLER, not an unconditional daily writer: an item whose full account cohort
// already has today's row (from a webhook sync) is skipped outright. That is what makes the daily
// cadence cost nothing for actively-syncing users, who are most of them. If this test can be made to
// pass while a request still leaves the process, the cost argument is fiction — hence the HTTP
// counter as well as the mock assertions.
func TestCaptureItemBalances_FullCohortSkipsWithZeroPlaidCalls(t *testing.T) {
	h := newCaptureHarness(t, liveItem(), []map[string]any{captureAccountJSON("plaid-a", 100.0, 100.0)})
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{}, nil).Once()

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err)

	assert.True(t, out.CohortComplete, "a full cohort must report as already covered")
	assert.False(t, out.PlaidCalled)
	assert.Zero(t, out.Written)
	assert.Equal(t, int64(0), h.plaidCalls.Load(),
		"a full cohort must make NO Plaid request — this is the cost claim, not a nicety")
	mockassert.NoCallTo(t, &h.plaid.Mock, "GetClient")
	mockassert.NoCallTo(t, &h.snapshots.Mock, "CreateDailyCapture")
	// And it must not even read the item: an item whose cohort is covered needs no access token,
	// so the decrypting read is skipped too.
	mockassert.NoCallTo(t, &h.items.Mock, "GetByID")
}

// ── T4: a partial cohort is not done ──────────────────────────────────────────────────────────

// TestCaptureItemBalances_PartialCohortIsStillProcessed is T4 — the incomplete-cohort defect itself.
//
// A webhook sync writes snapshots per ITEM but can leave part of that item's roster uncovered, and
// the day's aggregate used to be the sum of only the accounts that received a row. So "does this
// item have ANY row today" is the WRONG predicate: it would skip exactly the days that are broken.
//
// The fixture is 3-of-5 rather than, say, 4-of-5 for a reason: it is the shape actually reported, and
// it puts two accounts on each side of the question so a fixture that accidentally matched the code's
// own boundary could not pass this by coincidence.
func TestCaptureItemBalances_PartialCohortIsStillProcessed(t *testing.T) {
	var plaidAccounts []map[string]any
	var internal []domain.Account
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		plaidAccounts = append(plaidAccounts, captureAccountJSON("plaid-"+id, float64(100+i), nil))
		internal = append(internal, activeAccount("acct-"+id, "plaid-"+id))
	}

	h := newCaptureHarness(t, liveItem(), plaidAccounts)
	// Two of five uncovered — the item is NOT done.
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-d", "acct-e"}, nil).Once()
	h.accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").Return(internal, nil).Once()
	h.snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)
	h.snapshots.On("CreateDailyCapture", mock.Anything, mock.Anything).Return(true, nil)

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err)

	assert.False(t, out.CohortComplete)
	assert.True(t, out.PlaidCalled)
	assert.Equal(t, int64(1), h.plaidCalls.Load(), "exactly one /accounts/get per processed item")
	// The WHOLE cohort is captured, not just the two gaps. The already-covered three either dedup or
	// lose the day's uniqueness race, and both are no-ops — whereas capturing only the named gaps
	// would need the enumeration to still be true minutes later, which nothing guarantees.
	assert.Equal(t, 5, out.Written)
}

// ── T5: a removed account gets no row ─────────────────────────────────────────────────────────

// TestCaptureItemBalances_RemovedAccountGetsNoRow is T5.
//
// ⚠ THE MOCK RETURNS THE REMOVED ACCOUNT, AND THAT IS THE WHOLE TEST. An earlier specification of
// this test would have passed vacuously: if the account read returns only active rows, "the removed
// account got no row" is true no matter what the service does. So the service reads the
// retained-INCLUSIVE roster and filters it itself, and this fixture hands it a removed account and a
// Plaid response that still reports it — which is exactly what a bank that still sees an account the
// user disconnected from US does.
//
// The rule being protected: a removed account must accrue NO balance snapshots, which is what stops
// a user-disconnected account quietly resuming data collection because the provider still reports it.
func TestCaptureItemBalances_RemovedAccountGetsNoRow(t *testing.T) {
	reason := string(domain.RemovalReasonUserDisconnect)
	gone := activeAccount("acct-gone", "plaid-gone")
	gone.Status = domain.AccountStatusRemoved
	gone.RemovedReason = &reason

	h := newCaptureHarness(t, liveItem(), []map[string]any{
		captureAccountJSON("plaid-live", 100.0, 100.0),
		captureAccountJSON("plaid-gone", 250.0, 250.0),
	})
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-live"}, nil).Once()
	h.accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").
		Return([]domain.Account{activeAccount("acct-live", "plaid-live"), gone}, nil).Once()
	h.snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)

	var written []string
	h.snapshots.On("CreateDailyCapture", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			written = append(written, args.Get(1).(*domain.BalanceSnapshot).AccountID)
		}).Return(true, nil)

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err)

	assert.Equal(t, []string{"acct-live"}, written,
		"a removed account must accrue no snapshot even when the provider still reports it")
	assert.Equal(t, 1, out.Written)
	// The provider account with no live internal row is counted, not silently dropped — an
	// unmatched account is either a removal we already made or a roster we have not linked yet, and
	// an operator needs to be able to see the number.
	assert.Equal(t, 1, out.Unmatched)
}

// ── T6: a NULL current balance ────────────────────────────────────────────────────────────────

// TestCaptureItemBalances_NullCurrentWithAvailablePresentIsStillWritten is the service half of T6.
// The repository half — that the NULL survives a real Postgres round trip, and that the day still
// renders via the carry-forward — is in
// repository/balance_snapshot_daily_capture_integration_test.go, because pgxmock scans a nil cell
// into a plain Go string without error and could not have told us anything here.
//
// The decision this pins is Task E's, restated so it cannot drift: available_balance is NOT
// coalesced into current_balance. On a credit card the available balance is the remaining credit
// LINE, so substituting it would report a large positive asset where a debt belongs. The row is
// written as-is; the aggregated read excludes it from the value cohort and carries the account's last
// known real balance.
func TestCaptureItemBalances_NullCurrentWithAvailablePresentIsStillWritten(t *testing.T) {
	h := newCaptureHarness(t, liveItem(), []map[string]any{captureAccountJSON("plaid-a", nil, 4500.0)})
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-a"}, nil).Once()
	h.accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").
		Return([]domain.Account{activeAccount("acct-a", "plaid-a")}, nil).Once()
	h.snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)

	var got *domain.BalanceSnapshot
	h.snapshots.On("CreateDailyCapture", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { got = args.Get(1).(*domain.BalanceSnapshot) }).Return(true, nil)

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err)
	assert.Equal(t, 1, out.Written)

	require.NotNil(t, got)
	assert.Nil(t, got.CurrentBalance, "current_balance must stay NULL, not be filled from available")
	require.NotNil(t, got.AvailableBalance)
	assert.InDelta(t, 4500.0, *got.AvailableBalance, 0.01)
	assert.Equal(t, domain.SnapshotSourceDailyCapture, got.Source,
		"the row must carry the gap-filler's own provenance — it is the idempotency key's scope")
}

// TestCaptureItemBalances_BothBalancesNullProducesNoRow is the boundary on the other side, and it
// is the case the EARLIER draft of T6 wrongly targeted: the writer already skips it. It is kept
// because without it, a "write whatever Plaid said" implementation would satisfy the test above and
// would insert a row carrying no balance at all — which Validate() rejects and no read can use.
func TestCaptureItemBalances_BothBalancesNullProducesNoRow(t *testing.T) {
	h := newCaptureHarness(t, liveItem(), []map[string]any{captureAccountJSON("plaid-a", nil, nil)})
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-a"}, nil).Once()
	h.accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").
		Return([]domain.Account{activeAccount("acct-a", "plaid-a")}, nil).Once()
	h.snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil).Maybe()

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err)

	assert.Zero(t, out.Written)
	assert.Equal(t, 1, out.NoBalance)
	mockassert.NoCallTo(t, &h.snapshots.Mock, "CreateDailyCapture")
}

// ── T8: the capture path fails loudly ─────────────────────────────────────────────────────────

// TestCaptureItemBalances_PartialWriteFailsTheJob is T8, and it is the single most important
// behavioural change in this task.
//
// The writer this service was extracted from LOGGED AND SWALLOWED individual insert failures while
// returning success. So a partial cohort could commit while River recorded the item as succeeded —
// which is precisely the incomplete-cohort shape that caused the user's reported bug. A job that
// exists to guarantee a row for every account must not be able to report success having written some
// of them.
//
// ⚠ The successful write is asserted too. An implementation that abandoned the cohort at the first
// error would also return an error and would pass a test that only checked the error — while leaving
// accounts uncaptured that it could have captured, and that River's retry might not reach before the
// UTC day rolls over.
func TestCaptureItemBalances_PartialWriteFailsTheJob(t *testing.T) {
	h := newCaptureHarness(t, liveItem(), []map[string]any{
		captureAccountJSON("plaid-a", 100.0, nil),
		captureAccountJSON("plaid-b", 200.0, nil),
		captureAccountJSON("plaid-c", 300.0, nil),
	})
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-a", "acct-b", "acct-c"}, nil).Once()
	h.accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").Return([]domain.Account{
		activeAccount("acct-a", "plaid-a"),
		activeAccount("acct-b", "plaid-b"),
		activeAccount("acct-c", "plaid-c"),
	}, nil).Once()
	h.snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)

	boom := errors.New("connection reset by peer")
	h.snapshots.On("CreateDailyCapture", mock.Anything, mock.MatchedBy(func(s *domain.BalanceSnapshot) bool {
		return s.AccountID == "acct-b"
	})).Return(false, boom)
	h.snapshots.On("CreateDailyCapture", mock.Anything, mock.Anything).Return(true, nil)

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")

	require.Error(t, err, "a partial capture must FAIL the job, never report success")
	assert.ErrorIs(t, err, boom, "the underlying failure must survive to the job's error, not be flattened")
	assert.Contains(t, err.Error(), "acct-b", "the error must name the account that failed")
	assert.Equal(t, 2, out.Written,
		"the other accounts are still captured — abandoning the cohort at the first error would lose "+
			"rows this run could have written before the UTC day rolls over")
}

// TestCaptureItemBalances_AProviderFailureIsReturned keeps the loudness honest one layer up: a failed
// /accounts/get is River's problem, and the honest outcome is a retry and — if it keeps failing — a
// gap. There is deliberately no fallback to accounts.current_balance, which is written only at link
// time and would beat a genuine earlier webhook row on every read.
func TestCaptureItemBalances_AProviderFailureIsReturned(t *testing.T) {
	// A server that fails /accounts/get. Built inline rather than through the harness because the
	// harness's catch-all handler treats an unexpected path as a test failure.
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/get", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error_code":"INTERNAL_SERVER_ERROR","error_type":"API_ERROR"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	cfg := plaid.NewConfiguration()
	cfg.UseEnvironment(plaid.Environment(server.URL))

	items := &MockLinkedItemRepository{}
	snapshots := &MockBalanceSnapshotRepository{}
	plaidSvc := &MockPlaidService{}
	items.On("GetByID", mock.Anything, "item-1").Return(liveItem(), nil).Once()
	snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-a"}, nil).Once()
	plaidSvc.On("GetClient").Return(plaid.NewAPIClient(cfg)).Once()

	svc := NewBalanceCaptureService(BalanceCaptureServiceDeps{
		LinkedItemRepo:      items,
		AccountRepo:         &MockAccountRepository{},
		BalanceSnapshotRepo: snapshots,
		PlaidService:        plaidSvc,
		Logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	_, err := svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.Error(t, err, "a provider failure must be returned so River retries and the day is not silently lost")
	mockassert.NoCallTo(t, &snapshots.Mock, "CreateDailyCapture")
}

// ── T11 (second line of defence) and ownership ────────────────────────────────────────────────

// TestCaptureItemBalances_NeedsReauthItemIsRefusedWithoutAPlaidCall re-checks the item's status at
// EXECUTION time, not just at enumeration time.
//
// The dispatcher already filters on status, so this is a second line of defence — and it is not
// redundant: minutes pass between enqueue and run, River retries a capture job for up to its whole
// retry budget, and a dead-letter replay can resurrect one much later. An ITEM_LOGIN_REQUIRED item
// does not heal on retry; it heals when the user re-authenticates.
func TestCaptureItemBalances_NeedsReauthItemIsRefusedWithoutAPlaidCall(t *testing.T) {
	for _, status := range []domain.LinkedItemStatus{
		domain.LinkedItemStatusLoginRequired,
		domain.LinkedItemStatusError,
		domain.LinkedItemStatusPendingExpiration,
		domain.LinkedItemStatusRevoked,
		domain.LinkedItemStatusReplaced,
		domain.LinkedItemStatusDisconnected,
	} {
		t.Run(string(status), func(t *testing.T) {
			item := liveItem()
			item.Status = status
			h := newCaptureHarness(t, item, []map[string]any{captureAccountJSON("plaid-a", 100.0, nil)})
			h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
				Return([]string{"acct-a"}, nil).Once()

			out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
			require.NoError(t, err, "an ineligible item is not a job failure — retrying it daily would "+
				"burn the retry budget on something only the user can fix")
			assert.True(t, out.Ineligible)
			assert.False(t, out.PlaidCalled)
			assert.Equal(t, int64(0), h.plaidCalls.Load())
			mockassert.NoCallTo(t, &h.snapshots.Mock, "CreateDailyCapture")
		})
	}
}

// TestCaptureItemBalances_OwnershipMismatchIsRefused. The job's args carry a user id enumerated
// alongside the item id, so a mismatch means the item changed hands or the args were tampered with
// through a dead-letter replay. Either way this must not read one user's balances under another
// user's job.
func TestCaptureItemBalances_OwnershipMismatchIsRefused(t *testing.T) {
	h := newCaptureHarness(t, liveItem(), []map[string]any{captureAccountJSON("plaid-a", 100.0, nil)})
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-a"}, nil).Once()

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "somebody-else")
	require.NoError(t, err, "a mismatch is not retryable — retrying cannot make the item change owner")
	assert.True(t, out.Ineligible)
	assert.Equal(t, int64(0), h.plaidCalls.Load())
	mockassert.NoCallTo(t, &h.snapshots.Mock, "CreateDailyCapture")
}

// ── The UTC day ───────────────────────────────────────────────────────────────────────────────

// TestCaptureItemBalances_AsksTheCohortQuestionAboutTheUTCDay pins the day the service reasons about.
//
// The job runs at a fixed UTC hour, which lands at a different LOCAL time for every user — a UTC-11
// user's 21:00-UTC row is stamped with a date that is still "tomorrow" for them. That is accepted for
// a daily balance series; a silently ambiguous day boundary is not. Both the cohort question and the
// snapshot instant must therefore be UTC, and this asserts the instant handed to the repository is
// inside the current UTC day rather than a local-clock one.
func TestCaptureItemBalances_AsksTheCohortQuestionAboutTheUTCDay(t *testing.T) {
	h := newCaptureHarness(t, liveItem(), []map[string]any{captureAccountJSON("plaid-a", 100.0, nil)})

	var askedAbout time.Time
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Run(func(args mock.Arguments) { askedAbout = args.Get(2).(time.Time) }).
		Return([]string{}, nil).Once()

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err)

	wantDay := time.Now().UTC().Format("2006-01-02")
	assert.Equal(t, wantDay, askedAbout.UTC().Format("2006-01-02"),
		"the cohort question must be about the current UTC calendar day")
	assert.Equal(t, time.UTC, askedAbout.Location(),
		"and it must be carried as a UTC instant, so no downstream conversion can shift it")
	assert.Equal(t, wantDay, out.Day.UTC().Format("2006-01-02"),
		"the outcome must report the same day it asked about")
}

// TestCaptureItemBalances_SendsTheDecryptedAccessToken exists because the wrong field compiles.
//
// linked_items carries the credential twice: AccessTokenEncrypted (what is stored) and AccessToken
// (the plaintext the repository fills in by decrypting it on every read). Sending the ciphertext is
// a one-character mistake that no mock-based test notices — the mock returns whatever the fixture
// put in the struct — and its production symptom is INVALID_ACCESS_TOKEN from Plaid, i.e. a
// permanent-looking provider error for a purely local bug, on a job that would then reproduce it
// every day.
//
// So this reads the request BODY the Plaid client actually sent.
func TestCaptureItemBalances_SendsTheDecryptedAccessToken(t *testing.T) {
	var sentToken string
	mux := http.NewServeMux()
	mux.HandleFunc("/accounts/get", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AccessToken string `json:"access_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sentToken = body.AccessToken
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts":   []map[string]any{captureAccountJSON("plaid-a", 100.0, nil)},
			"item":       map[string]any{"item_id": "item-1", "institution_id": "ins_test", "webhook": "", "available_products": []string{}, "billed_products": []string{}, "update_type": "background"},
			"request_id": "req-token-test",
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	cfg := plaid.NewConfiguration()
	cfg.UseEnvironment(plaid.Environment(server.URL))

	item := liveItem()
	item.AccessToken = "access-sandbox-plaintext"
	item.AccessTokenEncrypted = "Y2lwaGVydGV4dA==-not-a-token"

	items := &MockLinkedItemRepository{}
	accounts := &MockAccountRepository{}
	snapshots := &MockBalanceSnapshotRepository{}
	plaidSvc := &MockPlaidService{}
	items.On("GetByID", mock.Anything, "item-1").Return(item, nil).Once()
	snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).Return([]string{"acct-a"}, nil).Once()
	accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").
		Return([]domain.Account{activeAccount("acct-a", "plaid-a")}, nil).Once()
	snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)
	snapshots.On("CreateDailyCapture", mock.Anything, mock.Anything).Return(true, nil)
	plaidSvc.On("GetClient").Return(plaid.NewAPIClient(cfg)).Once()

	svc := NewBalanceCaptureService(BalanceCaptureServiceDeps{
		LinkedItemRepo:      items,
		AccountRepo:         accounts,
		BalanceSnapshotRepo: snapshots,
		PlaidService:        plaidSvc,
		Logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	_, err := svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err)
	assert.Equal(t, "access-sandbox-plaintext", sentToken,
		"the DECRYPTED access token must reach Plaid — the encrypted column would fail with "+
			"INVALID_ACCESS_TOKEN and be retried daily forever")
}

// ── G8: an active account the provider did not report ─────────────────────────────────────────

// TestCaptureItemBalances_ActiveAccountAbsentFromTheRosterFailsTheItem is #1626 gap G8.
//
// THE DEFECT. writeBalanceCohort walks the PROVIDER's roster and looks up internal accounts, so an
// account that exists internally, is ACTIVE, and is simply not named in /accounts/get is never
// visited at all. It accrued no snapshot, contributed to no counter, and the run returned success —
// the same permanent-absence shape as G13, one layer down. The only trace was the difference between
// the `gaps_found` and `written` fields of an INFO line, which nothing compares.
//
// THE DECISION, and it is a decision rather than an obvious fix. Three options were on the table:
//
//   - CARRY FORWARD the last known balance. REJECTED, and not narrowly: this file's own header rules
//     out manufacturing an observation, and the accounts.current_balance fallback was removed for
//     exactly this reason. Every read takes the latest snapshot_at, so a fabricated same-day row
//     would BEAT a genuine earlier webhook row. A gap is recoverable information; an invented
//     observation that outranks a real one is not.
//   - COUNT IT AND RETURN SUCCESS. Rejected as insufficient on its own. It makes the condition
//     visible to someone reading logs and leaves the job reporting success — which is precisely the
//     "healthy job, absent data" shape this whole phase exists to remove.
//   - COUNT IT AND FAIL THE ITEM. Chosen. It is the SAME policy this service already applies to a
//     partially-written cohort, for the same stated reason: "a job whose entire remit is 'every
//     account has a row for today' must not report success having written some of them". An account
//     the provider did not report IS a partial cohort; the only difference is where the gap came
//     from. Nothing is fabricated, the rows that DID write stay written (each insert commits
//     individually), River retries within the day in case the omission was transient, and an
//     omission that is permanent dead-letters loudly instead of accruing silence.
//
// ⚠ WHY THIS DOES NOT MAKE THE SYNC PATH FAIL TOO, even though both paths share
// writeBalanceCohort: the check lives in CaptureItemBalances, not in the shared writer.
// createBalanceSnapshots runs reconcileRemovedAccounts BEFORE its cohort write, which soft-removes
// exactly the accounts a roster proves absent, so it has already ACTED on the condition by the time
// the writer runs. The gap-filler performs no reconciliation, deliberately (see this service's
// header), so it is the path that needs the check.
//
// ⚠ AND IT IS SCOPED TO THE ACCOUNTS THE COHORT CHECK ASKED ABOUT. An absent account that ALREADY
// has today's row is not a gap and must not fail the item — otherwise a bank that stops reporting a
// closed account would fail this job every day for the rest of time even though the day's data is
// complete. TestCaptureItemBalances_AbsentAccountThatAlreadyHasTodaysRowIsNotAFailure pins that.
func TestCaptureItemBalances_ActiveAccountAbsentFromTheRosterFailsTheItem(t *testing.T) {
	// Plaid reports ONE of the item's two active accounts. `acct-b` is live per our own records and
	// is one of the two the cohort check just named as missing today's row.
	h := newCaptureHarness(t, liveItem(), []map[string]any{
		captureAccountJSON("plaid-a", 100.0, 100.0),
	})
	internal := []domain.Account{
		activeAccount("acct-a", "plaid-a"),
		activeAccount("acct-b", "plaid-b"),
	}
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-a", "acct-b"}, nil).Once()
	h.accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").Return(internal, nil).Once()
	h.snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)
	h.snapshots.On("CreateDailyCapture", mock.Anything, mock.Anything).Return(true, nil)

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")

	require.Error(t, err,
		"an ACTIVE internal account that the provider did not report, and that has no row for today, "+
			"must fail the item. Returning success here is the silent permanent absence this phase "+
			"exists to remove: no row is written, no retry happens, and the day is lost with nothing "+
			"but a log line to say so")
	assert.ErrorIs(t, err, ErrBalanceCaptureAccountAbsent,
		"the failure must be identifiable, not just any error — the worker logs it and an operator "+
			"has to be able to tell it apart from a provider outage")
	assert.Contains(t, err.Error(), "acct-b",
		"the error must NAME the account, or the condition is undiagnosable from the dead letter")

	assert.Equal(t, 1, out.ProviderAbsent,
		"exactly one account was absent; the count is what makes the condition measurable in "+
			"production rather than only visible in prose")
	assert.Equal(t, 1, out.Written,
		"the account the provider DID report must still get its row — the failure must not discard "+
			"the work that succeeded, because there is no backfill to redo it")
	// Nothing is invented for the absent account.
	h.snapshots.AssertNumberOfCalls(t, "CreateDailyCapture", 1)
}

// TestCaptureItemBalances_AbsentAccountThatAlreadyHasTodaysRowIsNotAFailure is the control that
// keeps G8's fix from becoming "any roster mismatch fails forever".
//
// A bank that closes an account simply stops returning it. If a webhook already captured that
// account's row earlier today, the DAY IS COMPLETE for it — there is no gap, nothing is missing, and
// failing the item would dead-letter this job every single day until the sync path's reconciliation
// happened to run. So the check is scoped to the accounts AccountsMissingDailySnapshot actually
// named, and this fixture puts an absent account OUTSIDE that set.
func TestCaptureItemBalances_AbsentAccountThatAlreadyHasTodaysRowIsNotAFailure(t *testing.T) {
	h := newCaptureHarness(t, liveItem(), []map[string]any{
		captureAccountJSON("plaid-a", 100.0, 100.0),
	})
	internal := []domain.Account{
		activeAccount("acct-a", "plaid-a"),
		activeAccount("acct-b", "plaid-b"), // absent from the roster, but NOT in the missing set
	}
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-a"}, nil).Once()
	h.accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").Return(internal, nil).Once()
	h.snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)
	h.snapshots.On("CreateDailyCapture", mock.Anything, mock.Anything).Return(true, nil)

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err,
		"an absent account that already has today's row is not a gap. Failing here would make this "+
			"job dead-letter every day for an item whose data is actually complete")
	assert.Zero(t, out.ProviderAbsent,
		"and it must not be counted either — the counter measures MISSING data, not roster drift")
	assert.Equal(t, 1, out.Written)
}

// TestCaptureItemBalances_ARemovedAccountAbsentFromTheRosterIsNotAFailure is the second control, and
// it protects a rule this codebase enforces elsewhere at some length: a removed account must accrue
// NO balance snapshots, so its absence from the provider's roster is the EXPECTED state and cannot
// be a failure. Without this, a user disconnecting one account would make the daily job fail for
// that item indefinitely.
func TestCaptureItemBalances_ARemovedAccountAbsentFromTheRosterIsNotAFailure(t *testing.T) {
	reason := string(domain.RemovalReasonUserDisconnect)
	gone := activeAccount("acct-gone", "plaid-gone")
	gone.Status = domain.AccountStatusRemoved
	gone.RemovedReason = &reason

	h := newCaptureHarness(t, liveItem(), []map[string]any{
		captureAccountJSON("plaid-a", 100.0, 100.0),
	})
	internal := []domain.Account{activeAccount("acct-a", "plaid-a"), gone}
	// A stale cohort answer that still names the removed account: the strongest form of this case,
	// because it means the check cannot lean on the cohort query having filtered it out.
	h.snapshots.On("AccountsMissingDailySnapshot", mock.Anything, "item-1", mock.Anything).
		Return([]string{"acct-a", "acct-gone"}, nil).Once()
	h.accounts.On("GetByLinkedItemIDIncludingRetained", mock.Anything, "item-1").Return(internal, nil).Once()
	h.snapshots.On("GetLatestByAccountID", mock.Anything, mock.Anything).Return(nil, nil)
	h.snapshots.On("CreateDailyCapture", mock.Anything, mock.Anything).Return(true, nil)

	out, err := h.svc.CaptureItemBalances(context.Background(), "item-1", "user-1")
	require.NoError(t, err,
		"a REMOVED account is meant to accrue no snapshots, so its absence from the roster is the "+
			"expected state. Failing on it would break the daily job for every item a user has ever "+
			"partially disconnected")
	assert.Zero(t, out.ProviderAbsent)
	assert.Equal(t, 1, out.Written)
}
