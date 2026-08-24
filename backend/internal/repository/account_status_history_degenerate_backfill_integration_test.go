//go:build integration

package repository

// Issue #1569, Task 18 (S7). What happens to an account whose 000152 backfill produced NO 'active'
// interval at all — and can such an account still be created?
//
// THE RECORDED CONCERN, AND WHY IT IS NARROWER THAN IT READS. dashboardService.historicalAccountMap
// switched its baseline universe from AllStatuses to AsOf. AllStatuses admitted every account that
// exists today regardless of recorded history; AsOf admits only accounts that
// account_status_history records as having been 'active' at the cutoff. So an account that never
// gets an 'active' interval can never enter the personal-scope comparison baseline, at ANY cutoff.
// 000152 produces exactly that shape when a terminal row's transition timestamp is NULL or is not
// strictly after created_at: the 'active' prefix INSERT is skipped and the terminal interval opens
// at created_at instead (see the "ZERO-LENGTH INTERVAL GUARD" note in
// migrations/000152_backfill_account_status_history.up.sql).
//
// THE NULL HALF OF THAT INPUT IS UNREACHABLE, BY VALIDATED CHECK CONSTRAINT — not by convention,
// and not merely "no caller does that":
//
//	status='replaced' + replaced_at IS NULL -> accounts_replaced_at_consistency (000033)
//	status='removed'  + removed_at  IS NULL -> accounts_removed_at_consistency  (000149)
//	status='expired'  + expired_at  IS NULL -> accounts_expired_at_consistency  (000155)
//	accounts.updated_at (the column the 'expired' backfill branch actually reads) is NOT NULL
//
// All three CHECKs are convalidated=true and all three predate or accompany the backfill, so no
// such row could have existed when 000152 ran either. TestAccounts_TerminalStatusRequiresA
// TransitionTimestamp below re-derives that from the live schema rather than citing it, so a
// future migration that drops or weakens any of them fails here instead of silently reopening the
// hole.
//
// WHAT REMAINS REACHABLE is the other half of 000152's guard: a NON-NULL transition timestamp that
// is not strictly after created_at (equal, or earlier — from clock skew between the app instance
// that stamped created_at and the one that stamped the transition). The schema permits it, so this
// file pins what the backfill does with it and, crucially, what the as-of read then reports.
//
// POPULATION. Zero, everywhere it could be checked: `freshcheck` and `boostedbudget` on the local
// Postgres 17 container both report 0 accounts in ANY of these states (and 0 accounts at all), and
// production has no users yet. Going forward the state is also inert: 000152's backfill runs once,
// and every account created after it goes through record_account_status_change()'s AFTER INSERT
// branch, which opens an 'active' interval at clock_timestamp() with no reference to replaced_at /
// removed_at / updated_at whatsoever. accountRepository.CreateBatch — the only INSERT INTO accounts
// in non-test code — omits the status column entirely and takes the DEFAULT 'active'.
//
// So this file documents and pins the status quo rather than changing it: the behaviour is correct
// (an account we cannot honestly say was ever observably live is not reported as having been live),
// and the defect would be leaving it undocumented.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// degenerateBackfillFixture is one (status, offset) probe of 000152's
// `transition_ts > created_at` guard.
type degenerateBackfillFixture struct {
	name string
	// status is the terminal accounts.status.
	status string
	// offset is added to created_at to produce the transition timestamp this status's backfill
	// branch reads (replaced_at / updated_at / removed_at). The three values probe one microsecond
	// BELOW the guard's bound, exactly ON it, and one microsecond ABOVE it — Postgres timestamp
	// resolution is 1µs, so these are adjacent representable instants and a `>` that became a `>=`
	// (or vice versa) cannot hide between them.
	offset time.Duration
	// wantActivePrefix is whether the backfill should emit an 'active' interval at all.
	wantActivePrefix bool
	// terminalReason is the reason stamped on the terminal interval by 000152.
	terminalReason string
}

// TestAccountStatusHistory_Backfill_TransitionNotAfterCreation is the behavioural pin. For each
// terminal status it drives 000152's own backfill SQL (read verbatim from the migration file) over
// three adjacent transition timestamps around the guard's bound, then asks the as-of account read
// — GetAccountsWithInstitutionFiltered with AsOf set, which is precisely what
// dashboardService.historicalAccountMap calls — whether the account was ever live.
func TestAccountStatusHistory_Backfill_TransitionNotAfterCreation(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	// These rows must look like they predate 000152, so the INSERT trigger (which would open an
	// 'active' interval and make the backfill a no-op for them) is disabled while they are written.
	_, err := pool.Exec(ctx, `ALTER TABLE accounts DISABLE TRIGGER account_status_history_insert_trigger`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `ALTER TABLE accounts ENABLE TRIGGER account_status_history_insert_trigger`)
	})

	created := time.Now().Add(-100 * 24 * time.Hour).UTC().Truncate(time.Microsecond)

	fixtures := []degenerateBackfillFixture{
		{"replaced/transition_before_creation", "replaced", -time.Microsecond, false, "relink"},
		{"replaced/transition_equals_creation", "replaced", 0, false, "relink"},
		{"replaced/transition_after_creation", "replaced", time.Microsecond, true, "relink"},
		{"expired/transition_before_creation", "expired", -time.Microsecond, false, "backfill_expired_estimated"},
		{"expired/transition_equals_creation", "expired", 0, false, "backfill_expired_estimated"},
		{"expired/transition_after_creation", "expired", time.Microsecond, true, "backfill_expired_estimated"},
		{"removed/transition_before_creation", "removed", -time.Microsecond, false, "user_disconnect"},
		{"removed/transition_equals_creation", "removed", 0, false, "user_disconnect"},
		{"removed/transition_after_creation", "removed", time.Microsecond, true, "user_disconnect"},
	}

	ids := make(map[string]string, len(fixtures))
	for _, f := range fixtures {
		transition := created.Add(f.offset)
		// Only the column this status's backfill branch reads carries `transition`; the others stay
		// NULL (or, for updated_at, at created_at) so each fixture probes exactly one branch.
		var replacedAt, removedAt, expiredAt *time.Time
		updatedAt := created
		switch f.status {
		case "replaced":
			replacedAt = &transition
		case "expired":
			updatedAt = transition
			// accounts_expired_at_consistency (000155) demands a non-NULL expired_at for this
			// status. 000155's own backfill set expired_at := updated_at for pre-existing rows, so
			// a row that predates both migrations looks like this once fully migrated.
			expiredAt = &transition
		case "removed":
			removedAt = &transition
		}

		var id string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status,
			                      created_at, updated_at, replaced_at, removed_at, expired_at)
			VALUES ($1, $2, $3, 'Degenerate', 'depository', $4, $5, $6, $7, $8, $9)
			RETURNING id`,
			item.ID, userID, "plaid-t18-"+suffix+"-"+f.name, f.status,
			created, updatedAt, replacedAt, removedAt, expiredAt,
		).Scan(&id), "fixture %s: the schema must ACCEPT a transition timestamp at %v relative to "+
			"created_at — if it no longer does, this whole class became unreachable and this test "+
			"should be re-derived, not deleted", f.name, f.offset)
		ids[f.name] = id
		require.Empty(t, ashIntervalsFor(t, ctx, pool, id),
			"fixture %s: disabling the INSERT trigger must leave the account with no history", f.name)
	}

	runAccountStatusHistoryBackfillSQL(t, ctx, pool)

	for _, f := range fixtures {
		f := f
		t.Run(f.name, func(t *testing.T) {
			id := ids[f.name]
			transition := created.Add(f.offset)
			intervals := ashIntervalsFor(t, ctx, pool, id)

			if f.wantActivePrefix {
				require.Len(t, intervals, 2,
					"a transition strictly after created_at leaves a nonzero-length observable "+
						"'active' life, so the backfill must record it")
				assert.Equal(t, "active", intervals[0].Status)
				assert.True(t, intervals[0].ValidFrom.Equal(created),
					"active prefix starts at created_at: got %v want %v", intervals[0].ValidFrom, created)
				require.NotNil(t, intervals[0].ValidTo)
				assert.True(t, intervals[0].ValidTo.Equal(transition),
					"active prefix ends at the transition instant: got %v want %v", *intervals[0].ValidTo, transition)
				assert.Equal(t, f.status, intervals[1].Status)
				assert.True(t, intervals[1].ValidFrom.Equal(transition),
					"terminal interval opens at the transition instant: got %v want %v", intervals[1].ValidFrom, transition)
				assert.Nil(t, intervals[1].ValidTo)
			} else {
				require.Len(t, intervals, 1,
					"a transition at or before created_at gives no honest nonzero-length 'active' "+
						"window, so 000152 emits ONLY the terminal interval")
				assert.Equal(t, f.status, intervals[0].Status,
					"the single interval is the terminal status, not 'active'")
				assert.True(t, intervals[0].ValidFrom.Equal(created),
					"the terminal interval opens at created_at (000152's CASE fallback), NOT at the "+
						"unusable transition instant: got %v want %v", intervals[0].ValidFrom, created)
				assert.Nil(t, intervals[0].ValidTo, "the terminal interval is the open one")
			}
			require.NotNil(t, intervals[len(intervals)-1].Reason)
			assert.Equal(t, f.terminalReason, *intervals[len(intervals)-1].Reason)

			// The consequence the concern was really about: what the as-of universe reports. Probes
			// sit on and either side of every bound this account has.
			probes := []struct {
				label string
				at    time.Time
				want  bool
			}{
				{"one hour before creation", created.Add(-time.Hour), false},
				{"exactly at created_at", created, f.wantActivePrefix},
				{"exactly at the transition instant", transition, false},
				{"one microsecond after the transition", transition.Add(time.Microsecond), false},
				{"fifty days later", created.Add(50 * 24 * time.Hour), false},
				{"now", time.Now().UTC(), false},
			}
			for _, p := range probes {
				at := p.at
				got, err := accountRepo.GetAccountsWithInstitutionFiltered(ctx, userID,
					AccountFilterOptions{AsOf: &at, IncludeHidden: true})
				require.NoError(t, err)
				present := false
				for _, a := range got {
					if a.ID == id {
						present = true
					}
				}
				assert.Equalf(t, p.want, present,
					"as-of live universe at %s (%v): want present=%v. An account with no recorded "+
						"'active' interval can never enter dashboardService.historicalAccountMap's "+
						"baseline, at any cutoff; one WITH a prefix is live at valid_from and not at "+
						"valid_to (half-open).", p.label, at, p.want)
			}
		})
	}
}

// TestAccounts_TerminalStatusRequiresATransitionTimestamp is the unreachability pin: it is the test
// that fails if a future migration makes the NULL-transition-timestamp state writable again.
//
// It asserts against the LIVE schema (a real INSERT and a real UPDATE, rolled back), not against
// the migration text, because the question is what Postgres will actually accept today.
func TestAccounts_TerminalStatusRequiresATransitionTimestamp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	cases := []struct {
		status string
		// nullColumn is the transition-timestamp column 000152's branch for this status reads, left
		// NULL here.
		nullColumn string
		wantErr    string
	}{
		{"replaced", "replaced_at", "accounts_replaced_at_consistency"},
		{"removed", "removed_at", "accounts_removed_at_consistency"},
		// 'expired' has two independent guards: the column paired with the status (000155) and the
		// column the backfill actually reads. Both are asserted — the backfill reads updated_at, so
		// accounts_expired_at_consistency alone would not be enough to make the branch's input safe.
		{"expired", "expired_at", "accounts_expired_at_consistency"},
		{"expired", "updated_at", "not-null constraint"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.status+"/"+c.nullColumn, func(t *testing.T) {
			// Every other timestamp is populated so the ONLY reason the write can fail is the
			// column under test being NULL.
			set := map[string]any{
				"replaced_at": nil, "removed_at": nil, "expired_at": nil,
				"updated_at": time.Now().UTC(),
			}
			switch c.status {
			case "replaced":
				set["replaced_at"] = time.Now().UTC()
			case "removed":
				set["removed_at"] = time.Now().UTC()
			case "expired":
				set["expired_at"] = time.Now().UTC()
			}
			set[c.nullColumn] = nil

			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(context.Background()) //nolint:errcheck // probe transaction is always discarded

			_, err = tx.Exec(ctx, `
				INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status,
				                      updated_at, replaced_at, removed_at, expired_at)
				VALUES ($1, $2, $3, 'NullTransition', 'depository', $4, $5, $6, $7, $8)`,
				item.ID, userID, "plaid-t18-null-"+suffix+"-"+c.status+"-"+c.nullColumn, c.status,
				set["updated_at"], set["replaced_at"], set["removed_at"], set["expired_at"])
			require.Errorf(t, err,
				"the schema ACCEPTED status=%q with %s IS NULL. 000152's backfill would then skip "+
					"the 'active' prefix for such a row, and it could never appear in an as-of "+
					"baseline. If this is now intentional, 000152's ZERO-LENGTH INTERVAL GUARD and "+
					"TestAccountStatusHistory_Backfill_TransitionNotAfterCreation need re-deriving.",
				c.status, c.nullColumn)
			assert.Containsf(t, err.Error(), c.wantErr,
				"rejected, but not by the guard this test is pinning — got %v", err)
		})
	}
}
