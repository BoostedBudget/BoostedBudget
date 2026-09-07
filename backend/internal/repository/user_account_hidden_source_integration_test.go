//go:build integration

package repository

// Issue #1609, Item 3 (task 3c, Part 2) — what the two hide statements actually compute.
//
// The service decides WHETHER a hide may be lifted (only a 'system' cause is lifted
// automatically); these are the statements that carry it out, and the one property that cannot be
// asserted anywhere else is that SetHiddenBySystem NEVER DOWNGRADES a cause already recorded. That
// is a CASE expression evaluated by Postgres against the row's current value — pgxmock asserts the
// SQL string and then returns rows the test author invented, so it can express neither the current
// value nor the result. Hence integration.
//
// Why the downgrade matters: a 'user' cause relabelled 'system' is silently destroyed by the next
// re-link, which is precisely the defect this batch exists to close. The guard has to be race-free
// too, which is why it is one statement rather than a read-then-write in Go.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hiddenSourceFixture struct {
	pool   *pgxpool.Pool
	userID uuid.UUID
	repo   UserAccountRepository
	prefix string
}

func newHiddenSourceFixture(t *testing.T) (*hiddenSourceFixture, context.Context) {
	t.Helper()
	pool := connectTestDB(t)
	ctx := context.Background()

	userID := uuid.New()
	prefix := "hidsrc-" + userID.String()

	// context.Background() rather than t.Context(): a cancelled context makes the DELETEs silently
	// no-op and leaks fixture rows into the shared database, where a global-scan test in another
	// package picks them up and fails on a mismatched entity.
	t.Cleanup(func() {
		cctx := context.Background()
		for _, stmt := range []string{
			`DELETE FROM user_accounts WHERE user_id = $1`,
			`DELETE FROM users WHERE id = $1`,
		} {
			if _, err := pool.Exec(cctx, stmt, userID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix, prefix+"@example.com")
	require.NoError(t, err)

	return &hiddenSourceFixture{
		pool:   pool,
		userID: userID,
		repo:   NewUserAccountRepository(pool, newTestFieldEncryptor(t)),
		prefix: prefix,
	}, ctx
}

// newChain inserts a chain row in a given hidden state. hiddenSource is written raw so the fixture
// can express states the application no longer produces (a hidden row with no recorded cause).
func (f *hiddenSourceFixture) newChain(t *testing.T, ctx context.Context, hidden bool, hiddenSource *string) string {
	t.Helper()
	id := uuid.New()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type, is_hidden, hidden_source)
		VALUES ($1, $2, $3, 'depository', $4, $5)`,
		id, f.userID, f.prefix+"-inst-"+id.String()[:8], hidden, hiddenSource)
	require.NoError(t, err)
	return id.String()
}

func (f *hiddenSourceFixture) read(t *testing.T, ctx context.Context, id string) (bool, *string) {
	t.Helper()
	var hidden bool
	var source *string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT is_hidden, hidden_source FROM user_accounts WHERE id = $1`, id).Scan(&hidden, &source))
	return hidden, source
}

// TestUserAccountRepository_SetHiddenBySystem_NeverDowngradesAUserCause is the one that matters.
// A user hide relabelled 'system' would be lifted by the next re-link, undetectably.
func TestUserAccountRepository_SetHiddenBySystem_NeverDowngradesAUserCause(t *testing.T) {
	f, ctx := newHiddenSourceFixture(t)

	user := "user"
	id := f.newChain(t, ctx, true, &user)

	require.NoError(t, f.repo.SetHiddenBySystem(ctx, id, f.userID.String()))

	hidden, source := f.read(t, ctx, id)
	assert.True(t, hidden)
	require.NotNil(t, source)
	assert.Equal(t, "user", *source, "a system hide must never relabel a hide the user set")
}

// TestUserAccountRepository_SetHiddenBySystem_NeverDowngradesAnUnknownCause — 'unknown' means the
// cause could NOT be established, and a system hide arriving on an already-hidden chain
// establishes nothing about why it was hidden before. Rewriting it to 'system' would make it
// eligible for the automatic un-hide, which is exactly what 'unknown' exists to prevent.
func TestUserAccountRepository_SetHiddenBySystem_NeverDowngradesAnUnknownCause(t *testing.T) {
	f, ctx := newHiddenSourceFixture(t)

	unknown := "unknown"
	id := f.newChain(t, ctx, true, &unknown)

	require.NoError(t, f.repo.SetHiddenBySystem(ctx, id, f.userID.String()))

	hidden, source := f.read(t, ctx, id)
	assert.True(t, hidden)
	require.NotNil(t, source)
	assert.Equal(t, "unknown", *source)
}

// TestUserAccountRepository_SetHiddenBySystem_RecordsSystemOnAVisibleChain is the ordinary case:
// the chain lost its last active account and is being hidden now.
func TestUserAccountRepository_SetHiddenBySystem_RecordsSystemOnAVisibleChain(t *testing.T) {
	f, ctx := newHiddenSourceFixture(t)

	id := f.newChain(t, ctx, false, nil)

	require.NoError(t, f.repo.SetHiddenBySystem(ctx, id, f.userID.String()))

	hidden, source := f.read(t, ctx, id)
	assert.True(t, hidden)
	require.NotNil(t, source)
	assert.Equal(t, "system", *source)
}

// TestUserAccountRepository_SetHiddenBySystem_FillsAMissingCauseOnAHiddenChain — a hidden row with
// no recorded cause is not a state the application produces any more, but it is one migration
// 000185 could leave behind if a row were hidden between its two steps, and one a fixture or an
// operator can create. Filling the hole is the only monotone answer: it is the ELSE-leg of the
// COALESCE, not a downgrade of anything.
func TestUserAccountRepository_SetHiddenBySystem_FillsAMissingCauseOnAHiddenChain(t *testing.T) {
	f, ctx := newHiddenSourceFixture(t)

	id := f.newChain(t, ctx, true, nil)

	require.NoError(t, f.repo.SetHiddenBySystem(ctx, id, f.userID.String()))

	hidden, source := f.read(t, ctx, id)
	assert.True(t, hidden)
	require.NotNil(t, source)
	assert.Equal(t, "system", *source)
}

// TestUserAccountRepository_ClearHidden_ClearsTheCauseWithTheFlag — the two columns are one fact.
// A visible row carrying a stale cause would hand its reason to whatever hid the row next, and
// the un-hide decision reads exactly that column.
func TestUserAccountRepository_ClearHidden_ClearsTheCauseWithTheFlag_Integration(t *testing.T) {
	f, ctx := newHiddenSourceFixture(t)

	system := "system"
	id := f.newChain(t, ctx, true, &system)

	require.NoError(t, f.repo.ClearHidden(ctx, id, f.userID.String()))

	hidden, source := f.read(t, ctx, id)
	assert.False(t, hidden)
	assert.Nil(t, source, "a visible chain has no hide, so it can have no reason for one")
}

// TestUserAccountRepository_Update_CarriesTheCauseWithTheFlag pins the USER-hide write: the
// customization update (PATCH /api/v1/user-accounts/{id}) is where a user hide is recorded, and
// the cause has to round-trip through the same statement that sets the flag.
func TestUserAccountRepository_Update_CarriesTheCauseWithTheFlag_Integration(t *testing.T) {
	f, ctx := newHiddenSourceFixture(t)

	id := f.newChain(t, ctx, false, nil)

	existing, err := f.repo.GetByIDAndUser(ctx, id, f.userID.String())
	require.NoError(t, err)
	require.False(t, existing.IsHidden)
	require.Nil(t, existing.HiddenSource)

	user := "user"
	existing.IsHidden = true
	existing.HiddenSource = &user
	require.NoError(t, f.repo.Update(ctx, existing))

	hidden, source := f.read(t, ctx, id)
	assert.True(t, hidden)
	require.NotNil(t, source)
	assert.Equal(t, "user", *source)

	// And it reads back through the same repository, which is what the un-hide decision consumes.
	reread, err := f.repo.GetByIDAndUser(ctx, id, f.userID.String())
	require.NoError(t, err)
	require.NotNil(t, reread.HiddenSource)
	assert.Equal(t, "user", *reread.HiddenSource)
}
