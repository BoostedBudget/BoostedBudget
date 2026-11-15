# Admin Email Prefix Search (#1178) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the broken exact blind-index lookup in `GET /api/v1/admin/users?search=` with a real starts-with prefix search that works without decrypting emails at query time.

**Architecture:** Pre-compute HMAC-SHA256 blind indices for every email prefix of rune-length [3, 64] at user creation, email update, and startup backfill time, storing them in `user_email_prefix_indices`. The admin search query uses an `EXISTS` subquery against this table. Domain-separated via `BlindIndexWithLabel("email_prefix", …)` to isolate prefix indices from the exact `email_blind_idx` column.

**Tech Stack:** Go/pgx v5, PostgreSQL 17, pgxmock v4 (unit), `//go:build integration` (real-DB), React/bun:test

---

## Files Changed

| File | Change |
|------|--------|
| `backend/internal/crypto/field_encryptor.go` | Add `BlindIndexWithLabel`, `NormalizeEmailForBlindIndex`, `EmailPrefixBlindIndices` |
| `backend/internal/crypto/field_encryptor_test.go` | New tests for all three helpers |
| `backend/migrations/000076_add_user_email_prefix_indices.up.sql` | New table + covering index |
| `backend/migrations/000076_add_user_email_prefix_indices.down.sql` | Drop table |
| `backend/internal/repository/profile_repository.go` | Add `insertPrefixIndicesTx`; hook `CreateUserTx` + `UpdateUserEmailTx` |
| `backend/internal/repository/profile_repository_test.go` | New `CreateUserTx` tests; update `UpdateUserEmailTx_Success` expectations |
| `backend/internal/repository/admin_user_repository.go` | Replace exact blind-index match with EXISTS prefix subquery |
| `backend/internal/repository/admin_user_repository_integration_test.go` | New prefix search integration tests |
| `backend/internal/database/backfill.go` | Add `BackfillEmailPrefixIndices` + helpers |
| `backend/internal/database/backfill_test.go` | Unit test for nil-input no-op |
| `backend/cmd/server/main.go` | Call `BackfillEmailPrefixIndices` after `BackfillEncryption` at startup |
| `backend/internal/huma/operations/admin.go` | Update doc tag; add min-length validation guard in `ListUsers` |
| `backend/internal/huma/operations/admin_users_test.go` | New test: search < 3 chars → 400 |
| `backend/internal/domain/admin_user.go` | Update `Search` field comment |
| `apps/web/src/features/admin/hooks/useUserFilters.ts` | Gate `apiParams.search` on `length >= 3` |
| `apps/web/src/features/admin/hooks/useUserFilters.test.tsx` | New tests for search gating behaviour |
| `apps/web/src/features/admin/components/UsersPage.tsx` | Update placeholder, add `minLength`, add hint text |

---

## Task 1: Crypto helpers — tests first

**Files:**
- Modify: `backend/internal/crypto/field_encryptor.go`
- Test: `backend/internal/crypto/field_encryptor_test.go`

- [ ] **Step 1: Write failing tests for all three helpers**

Append to `backend/internal/crypto/field_encryptor_test.go`:

```go
// TestBlindIndexWithLabel_DomainSeparation verifies it differs from BlindIndex.
// NOTE: field_encryptor_test.go is package crypto_test — all exported symbols need crypto. prefix.
func TestBlindIndexWithLabel_DomainSeparation(t *testing.T) {
	enc := newTestEncryptor(t)
	without := enc.BlindIndex("test@example.com")
	withLabel := enc.BlindIndexWithLabel("email_prefix", "test@example.com")
	assert.NotEqual(t, without, withLabel,
		"BlindIndexWithLabel must differ from BlindIndex for the same value")
}

// TestBlindIndexWithLabel_Deterministic verifies same label+value always gives same hash.
func TestBlindIndexWithLabel_Deterministic(t *testing.T) {
	enc := newTestEncryptor(t)
	idx1 := enc.BlindIndexWithLabel("email_prefix", "test@example.com")
	idx2 := enc.BlindIndexWithLabel("email_prefix", "test@example.com")
	assert.Equal(t, idx1, idx2)
}

// TestNormalizeEmailForBlindIndex verifies lower+trim behaviour.
// Package is crypto_test — must use crypto. prefix for package-level functions.
func TestNormalizeEmailForBlindIndex(t *testing.T) {
	assert.Equal(t, "user@example.com", crypto.NormalizeEmailForBlindIndex("  USER@EXAMPLE.COM  "))
	assert.Equal(t, "user@example.com", crypto.NormalizeEmailForBlindIndex("user@example.com"))
	assert.Equal(t, "", crypto.NormalizeEmailForBlindIndex("   "))
}

// TestEmailPrefixBlindIndices_Count verifies correct number of indices.
func TestEmailPrefixBlindIndices_Count(t *testing.T) {
	enc := newTestEncryptor(t)
	// "test@example.com" = 16 runes → indices for lengths 3..16 = 14 indices
	indices := crypto.EmailPrefixBlindIndices(enc, "test@example.com")
	assert.Len(t, indices, 14)
}

// TestEmailPrefixBlindIndices_TooShort returns nil when email has < 3 runes.
func TestEmailPrefixBlindIndices_TooShort(t *testing.T) {
	enc := newTestEncryptor(t)
	assert.Nil(t, crypto.EmailPrefixBlindIndices(enc, "ab"))
	assert.Nil(t, crypto.EmailPrefixBlindIndices(enc, ""))
}

// TestEmailPrefixBlindIndices_CapsAt64 verifies prefix count is capped at 64.
func TestEmailPrefixBlindIndices_CapsAt64(t *testing.T) {
	enc := newTestEncryptor(t)
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@x.com" // 70+ runes
	indices := crypto.EmailPrefixBlindIndices(enc, long)
	assert.Len(t, indices, 64-3+1, "should produce indices for rune lengths 3..64")
}

// TestEmailPrefixBlindIndices_NormalizesInput verifies case/whitespace normalisation.
func TestEmailPrefixBlindIndices_NormalizesInput(t *testing.T) {
	enc := newTestEncryptor(t)
	lower := crypto.EmailPrefixBlindIndices(enc, "test@example.com")
	upper := crypto.EmailPrefixBlindIndices(enc, "TEST@EXAMPLE.COM")
	require.NotNil(t, lower)
	require.NotNil(t, upper)
	assert.Equal(t, lower, upper, "normalisation must produce identical indices regardless of input case")
}

// TestEmailPrefixBlindIndices_RuneSafe verifies multi-byte (non-ASCII) chars are not split.
func TestEmailPrefixBlindIndices_RuneSafe(t *testing.T) {
	enc := newTestEncryptor(t)
	// "ñoño" = 4 runes but 6 bytes in UTF-8 — prefix for length 3 is "ñon" (3 runes, not 4 bytes)
	indices := crypto.EmailPrefixBlindIndices(enc, "ñoño@x.com") // "ñoño@x.com" = 10 runes
	assert.Len(t, indices, 10-3+1, "must count runes, not bytes")
}
```

- [ ] **Step 2: Run tests to confirm they FAIL**

```bash
cd backend && go test ./internal/crypto/... -v -run "TestBlindIndexWithLabel|TestNormalizeEmailForBlindIndex|TestEmailPrefixBlindIndices"
```

Expected: `FAIL` — undefined: `BlindIndexWithLabel`, `NormalizeEmailForBlindIndex`, `EmailPrefixBlindIndices`

- [ ] **Step 3: Implement the three helpers**

Append to `backend/internal/crypto/field_encryptor.go` (inside the `crypto` package):

```go
// BlindIndexWithLabel produces a domain-separated HMAC-SHA256 blind index.
// The label prevents semantic overlap between exact-email and prefix blind indices.
// Use label "email_prefix" for all prefix table reads and writes.
func (fe *FieldEncryptor) BlindIndexWithLabel(label, plaintext string) string {
	mac := hmac.New(sha256.New, fe.hmacKey)
	mac.Write([]byte(label + ":" + plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

// NormalizeEmailForBlindIndex matches the email normalisation used throughout
// profile_repository.go and backfill.go (lower + trim, no NFC).
// Must be identical everywhere: creation, update, backfill, and admin search.
func NormalizeEmailForBlindIndex(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const (
	minPrefixLen = 3
	maxPrefixLen = 64
)

// EmailPrefixBlindIndices generates domain-separated blind indices for all
// rune-length prefixes in [minPrefixLen, maxPrefixLen] of the given email.
// Rune-based slicing prevents splitting multi-byte UTF-8 characters.
// Returns nil when the normalised email is shorter than minPrefixLen.
func EmailPrefixBlindIndices(enc *FieldEncryptor, email string) []string {
	normalised := NormalizeEmailForBlindIndex(email)
	runes := []rune(normalised)
	end := len(runes)
	if end > maxPrefixLen {
		end = maxPrefixLen
	}
	if end < minPrefixLen {
		return nil
	}
	indices := make([]string, 0, end-minPrefixLen+1)
	for i := minPrefixLen; i <= end; i++ {
		indices = append(indices, enc.BlindIndexWithLabel("email_prefix", string(runes[:i])))
	}
	return indices
}
```

- [ ] **Step 4: Run tests to confirm they PASS**

```bash
cd backend && go test ./internal/crypto/... -v -run "TestBlindIndexWithLabel|TestNormalizeEmailForBlindIndex|TestEmailPrefixBlindIndices"
```

Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/crypto/field_encryptor.go backend/internal/crypto/field_encryptor_test.go
git commit -m "#1178: Add BlindIndexWithLabel, NormalizeEmailForBlindIndex, EmailPrefixBlindIndices"
```

---

## Task 2: Migration 000076

**Files:**
- Create: `backend/migrations/000076_add_user_email_prefix_indices.up.sql`
- Create: `backend/migrations/000076_add_user_email_prefix_indices.down.sql`

- [ ] **Step 1: Create .up.sql**

```sql
-- backend/migrations/000076_add_user_email_prefix_indices.up.sql
-- #1178: Prefix blind indices for starts-with admin email search.
-- Stores one row per (user_id, prefix_blind_idx) pair.
-- PRIMARY KEY deduplicates; CASCADE keeps rows consistent with user deletes.

CREATE TABLE IF NOT EXISTS user_email_prefix_indices (
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    prefix_blind_idx TEXT NOT NULL,
    PRIMARY KEY (user_id, prefix_blind_idx)
);

-- Covering index: answers EXISTS(WHERE prefix_blind_idx = $1 AND user_id = ...)
-- without heap access. Both columns are needed — include both.
CREATE INDEX IF NOT EXISTS idx_uepi_prefix_blind_idx_user
    ON user_email_prefix_indices(prefix_blind_idx, user_id);
```

- [ ] **Step 2: Create .down.sql**

```sql
-- backend/migrations/000076_add_user_email_prefix_indices.down.sql
DROP TABLE IF EXISTS user_email_prefix_indices;
```

- [ ] **Step 3: Apply migration and verify**

```bash
cd backend && make migrate-up
```

Expected output: `migrate: +1 applied` (or similar). Table must exist.

- [ ] **Step 4: Run migration test suite**

```bash
./scripts/test-migrations.sh
```

Expected: all 5 suites pass (idempotency, perf, data, schema, rollback)

- [ ] **Step 5: Commit**

```bash
git add backend/migrations/000076_add_user_email_prefix_indices.up.sql \
        backend/migrations/000076_add_user_email_prefix_indices.down.sql
git commit -m "#1178: Add migration 000076 for user_email_prefix_indices table"
```

---

## Task 3: Profile repo — insertPrefixIndicesTx + CreateUserTx hook

**Files:**
- Modify: `backend/internal/repository/profile_repository.go`
- Modify: `backend/internal/repository/profile_repository_test.go`

- [ ] **Step 1: Write failing pgxmock test for CreateUserTx with prefix rows**

Append to `backend/internal/repository/profile_repository_test.go`:

```go
// TestProfileRepository_CreateUserTx_InsertsEmailPrefixRows verifies that CreateUserTx
// inserts prefix blind index rows after the user INSERT, within the same transaction.
func TestProfileRepository_CreateUserTx_InsertsEmailPrefixRows(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	user := &domain.User{
		ClerkUserID:        "clerk_test_prefix",
		Email:              "prefix@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}

	mock.ExpectBegin()
	// Expect the users INSERT (24 args match the CreateUserTx query)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (`)).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// Expect the prefix indices INSERT — match on SQL prefix only; arg count varies by email length
	mock.ExpectExec(`INSERT INTO user_email_prefix_indices`).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	err = repo.CreateUserTx(ctx, tx, user)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestProfileRepository_CreateUserTx_ShortEmail_NoPrefixInsert verifies that an email
// too short to generate prefix rows (< 3 rune chars) does NOT issue a prefix INSERT.
func TestProfileRepository_CreateUserTx_ShortEmail_NoPrefixInsert(t *testing.T) {
	mock, repo := setupMockDB(t)
	defer mock.Close()
	ctx := context.Background()

	user := &domain.User{
		ClerkUserID:        "clerk_test_short",
		Email:              "ab",  // only 2 chars — below minPrefixLen
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (`)).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// No ExpectExec for prefix indices — if the implementation incorrectly inserts, this will FAIL.

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	err = repo.CreateUserTx(ctx, tx, user)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd backend && go test ./internal/repository/... -v -run "TestProfileRepository_CreateUserTx_InsertsEmailPrefixRows|TestProfileRepository_CreateUserTx_ShortEmail_NoPrefixInsert"
```

Expected: FAIL — the new tests expect a prefix INSERT that doesn't happen yet

- [ ] **Step 3: Add `insertPrefixIndicesTx` private method to profile_repository.go**

Add before `CreateUserTx` in `backend/internal/repository/profile_repository.go`:

```go
// insertPrefixIndicesTx inserts prefix blind indices for a user in a single multi-row statement.
// ON CONFLICT DO NOTHING ensures idempotency — safe to call even if rows already exist.
// Returns immediately (no-op) when blindIndices is empty.
func (r *profileRepository) insertPrefixIndicesTx(
	ctx context.Context, tx pgx.Tx, userID string, blindIndices []string,
) error {
	if len(blindIndices) == 0 {
		return nil
	}
	// Single multi-row INSERT avoids per-row round trips and pgx.SendBatch footguns.
	// Parameterised as: INSERT INTO ... VALUES ($1, $2), ($1, $3), ... ON CONFLICT DO NOTHING
	// $1 is reused for user_id across all rows; $2..$N are the prefix blind indices.
	args := make([]any, 0, 1+len(blindIndices))
	args = append(args, userID)
	placeholders := make([]string, 0, len(blindIndices))
	for i, idx := range blindIndices {
		args = append(args, idx)
		placeholders = append(placeholders, fmt.Sprintf("($1, $%d)", i+2))
	}
	query := fmt.Sprintf(
		"INSERT INTO user_email_prefix_indices (user_id, prefix_blind_idx) VALUES %s ON CONFLICT DO NOTHING",
		strings.Join(placeholders, ", "),
	)
	_, err := tx.Exec(ctx, query, args...)
	return err
}
```

- [ ] **Step 4: Hook insertPrefixIndicesTx into CreateUserTx**

In `backend/internal/repository/profile_repository.go`, replace the final `return r.checkDuplicateError(err)` in `CreateUserTx` with:

```go
	if err := r.checkDuplicateError(err); err != nil {
		return err
	}

	// Insert prefix blind indices so admin starts-with search works (#1178)
	indices := appcrypto.EmailPrefixBlindIndices(r.enc, user.Email)
	return r.insertPrefixIndicesTx(ctx, tx, user.ID, indices)
```

> **Note:** `user.ID` is set at the top of `CreateUserTx` via `uuid.New().String()` before the INSERT, so it is available here.

- [ ] **Step 5: Run tests to confirm PASS**

```bash
cd backend && go test ./internal/repository/... -v -run "TestProfileRepository_CreateUserTx"
```

Expected: both new tests PASS; existing repository tests still pass

- [ ] **Step 6: Run full backend tests to catch regressions**

```bash
cd backend && go test ./... -count=1
```

Expected: all tests PASS

- [ ] **Step 7: Commit**

```bash
git add backend/internal/repository/profile_repository.go backend/internal/repository/profile_repository_test.go
git commit -m "#1178: Add insertPrefixIndicesTx; hook CreateUserTx to generate prefix rows"
```

---

## Task 4: Profile repo — UpdateUserEmailTx hook

**Files:**
- Modify: `backend/internal/repository/profile_repository_test.go` (update existing test FIRST)
- Modify: `backend/internal/repository/profile_repository.go`

- [ ] **Step 1: Update the existing pgxmock test to expect DELETE + INSERT — this makes it RED**

In `backend/internal/repository/profile_repository_test.go`, find `TestProfileRepository_UpdateUserEmailTx_Success` and replace the whole test body with:

```go
// TestProfileRepository_UpdateUserEmailTx_Success verifies that UpdateUserEmailTx:
//  1. Updates the email columns on the users row (existing behaviour)
//  2. Deletes the old prefix rows for the user
//  3. Inserts new prefix rows for the updated email
func TestProfileRepository_UpdateUserEmailTx_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mock, repo := setupMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	newEmail := "newemail@example.com"

	mock.ExpectBegin()
	// 1. UPDATE users (existing behaviour — must stay first for lock-ordering invariant)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET email = $1, email_blind_idx = $2, email_verified = false, updated_at = $3 WHERE id = $4`)).
		WithArgs(
			pgxmock.AnyArg(), // email (encrypted)
			pgxmock.AnyArg(), // email_blind_idx
			pgxmock.AnyArg(), // updated_at
			userID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// 2. DELETE old prefix rows
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_email_prefix_indices WHERE user_id = $1`)).
		WithArgs(userID).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	// 3. INSERT new prefix rows (arg count varies; match on SQL prefix only)
	mock.ExpectExec(`INSERT INTO user_email_prefix_indices`).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	err = repo.UpdateUserEmailTx(ctx, tx, userID, newEmail)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 2: Run to confirm the test FAILS (RED)**

```bash
cd backend && go test ./internal/repository/... -v -run "TestProfileRepository_UpdateUserEmailTx_Success"
```

Expected: FAIL — the current implementation only does the UPDATE, not DELETE+INSERT

- [ ] **Step 3: Implement the hook in UpdateUserEmailTx**

In `backend/internal/repository/profile_repository.go`, find `UpdateUserEmailTx` and replace its final `return nil` with:

```go
	// Delete stale prefix rows, then insert rows for the new email.
	// CRITICAL: This code appends AFTER the UPDATE users above, maintaining the
	// lock-ordering invariant (users row locked first → prefix rows second),
	// which prevents deadlocks with the startup backfill.
	if _, err := tx.Exec(ctx,
		"DELETE FROM user_email_prefix_indices WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("delete old prefix indices: %w", err)
	}
	indices := appcrypto.EmailPrefixBlindIndices(r.enc, plainEmail)
	return r.insertPrefixIndicesTx(ctx, tx, userID, indices)
```

- [ ] **Step 4: Run tests to confirm GREEN**

```bash
cd backend && go test ./internal/repository/... -v -run "TestProfileRepository_UpdateUserEmailTx"
```

Expected: all `UpdateUserEmailTx` tests PASS (Success, DuplicateEmail, NotFound)

- [ ] **Step 5: Run full backend tests**

```bash
cd backend && go test ./... -count=1
```

Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/repository/profile_repository.go backend/internal/repository/profile_repository_test.go
git commit -m "#1178: Hook UpdateUserEmailTx to atomically replace prefix rows on email change"
```

---

## Task 5: Admin repo integration tests (write first — RED until Task 6)

**Files:**
- Modify: `backend/internal/repository/admin_user_repository_integration_test.go`

- [ ] **Step 1: Add prefix search integration tests**

Append to `backend/internal/repository/admin_user_repository_integration_test.go`:

```go
// TestListUsersPrefix_Integration verifies that the EXISTS-based prefix subquery
// supports starts-with email search against a real PostgreSQL planner.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... -run TestListUsersPrefix_Integration -tags integration -v
func TestListUsersPrefix_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	adminRepo := NewAdminUserRepository(pool, enc)
	profileRepo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// Create a unique email so we don't collide with existing data
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	testEmail := "prefixtest-" + suffix + "@integration.example"

	// Create test user (CreateUserTx now inserts prefix rows automatically)
	user := &domain.User{
		ClerkUserID:        "clerk_prefixtest_" + suffix,
		Email:              testEmail,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		// CASCADE deletes prefix rows automatically
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	t.Run("prefix match returns the user", func(t *testing.T) {
		// "prefixtest-" is 11 chars — well above the 3-char minimum
		search := "prefixtest-" + suffix[:6] // use a unique prefix
		filters := domain.AdminUserFilters{Search: search}
		users, count, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		found := false
		for _, u := range users {
			if u.ID == user.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "user should be found via prefix search")
		assert.GreaterOrEqual(t, count, 1)
	})

	t.Run("non-prefix (contains only) does not match", func(t *testing.T) {
		// "@integration.example" is a suffix, not a prefix of the email
		filters := domain.AdminUserFilters{Search: "@integration.example"}
		users, _, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		for _, u := range users {
			assert.NotEqual(t, user.ID, u.ID, "suffix-only search must not match")
		}
	})

	t.Run("exact full email match works", func(t *testing.T) {
		filters := domain.AdminUserFilters{Search: testEmail}
		users, _, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		found := false
		for _, u := range users {
			if u.ID == user.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "exact full-email search must match")
	})

	t.Run("tier and search combination filters correctly", func(t *testing.T) {
		prefix := "prefixtest-" + suffix[:6]
		filters := domain.AdminUserFilters{Search: prefix, Tier: "household_2"}
		users, _, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		// Our test user is premium, not household_2 — it should NOT appear
		for _, u := range users {
			assert.NotEqual(t, user.ID, u.ID)
		}
	})
}
```

Also add `"fmt"` and `"time"` to the imports block (or they may already be present — check and add if missing).

- [ ] **Step 2: Run to confirm the tests FAIL (RED)**

```bash
cd backend && DATABASE_URL=postgres://localhost/boostedbudget_test \
  go test ./internal/repository/... -run TestListUsersPrefix_Integration -tags integration -v
```

Expected: FAIL — `buildFilterClause` still uses the exact `email_blind_idx` match; prefix search returns no results

- [ ] **Step 3: Commit (tests that will be fixed in Task 6)**

```bash
git add backend/internal/repository/admin_user_repository_integration_test.go
git commit -m "#1178: Write failing prefix search integration tests for admin user repo"
```

---

## Task 6: Admin repo — replace exact match with EXISTS prefix subquery

**Files:**
- Modify: `backend/internal/repository/admin_user_repository.go`

- [ ] **Step 0: Check for existing pgxmock tests that reference the old SQL**

```bash
grep -n "email_blind_idx\|buildFilterClause" backend/internal/repository/admin_user_repository*test* 2>/dev/null || echo "none found"
```

If any unit tests (non-integration) reference `email_blind_idx` or the old exact-match SQL, update their `ExpectQuery`/`ExpectExec` expectations to use the new `EXISTS (SELECT 1 FROM user_email_prefix_indices ...)` pattern before changing the production code. The existing integration test only tests sort order — there are no pgxmock search-filter expectations to update — but verify this.

- [ ] **Step 1: Replace the search clause in buildFilterClause**

In `backend/internal/repository/admin_user_repository.go`, find `buildFilterClause` and replace:

```go
	if filters.Search != "" {
		clause += fmt.Sprintf(" AND u.email_blind_idx = $%d", argIndex)
		args = append(args, r.enc.BlindIndex(strings.ToLower(strings.TrimSpace(filters.Search))))
		argIndex++
	}
```

with:

```go
	if filters.Search != "" {
		clause += fmt.Sprintf(
			" AND EXISTS (SELECT 1 FROM user_email_prefix_indices p"+
				" WHERE p.user_id = u.id AND p.prefix_blind_idx = $%d)", argIndex)
		args = append(args, r.enc.BlindIndexWithLabel("email_prefix",
			appcrypto.NormalizeEmailForBlindIndex(filters.Search)))
		argIndex++
	}
```

> **Note:** `appcrypto` is already imported as an alias in `admin_user_repository.go`. `strings` remains imported (used for `strings.ToUpper(filters.SortDir)`).

- [ ] **Step 2: Run the full repository test suite**

```bash
cd backend && go test ./internal/repository/... -count=1
```

Expected: all unit tests PASS

- [ ] **Step 3: Run the prefix search integration tests (now GREEN)**

```bash
cd backend && DATABASE_URL=postgres://localhost/boostedbudget_test \
  go test ./internal/repository/... -run TestListUsersPrefix_Integration -tags integration -v
```

Expected: all 4 integration sub-tests PASS

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/admin_user_repository.go
git commit -m "#1178: Replace exact email_blind_idx match with EXISTS prefix subquery in ListUsers"
```

---

## Task 7: Backfill function + main.go startup call

**Files:**
- Modify: `backend/internal/database/backfill.go`
- Modify: `backend/internal/database/backfill_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write nil-input unit test (will PASS immediately after implementation)**

Add to `backend/internal/database/backfill_test.go`:

```go
// TestBackfillEmailPrefixIndices_NilInputs verifies nil inputs are handled gracefully
// (local dev without ENCRYPTION_KEY should be a no-op).
func TestBackfillEmailPrefixIndices_NilInputs(t *testing.T) {
	err := BackfillEmailPrefixIndices(context.Background(), nil, nil)
	assert.NoError(t, err, "nil inputs should be a no-op")
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd backend && go test ./internal/database/... -v -run TestBackfillEmailPrefixIndices_NilInputs
```

Expected: FAIL — undefined: `BackfillEmailPrefixIndices`

- [ ] **Step 3: Implement BackfillEmailPrefixIndices in backfill.go**

Add to `backend/internal/database/backfill.go`.

First, add the new imports (insert into the existing import block):
```go
	"errors"
	"github.com/jackc/pgx/v5"
```

Then add after the last function in the file:

```go
// backfillResult tracks the outcome of processing a single user during backfill.
type backfillResult int

const (
	resultProcessed     backfillResult = iota // prefix rows were written
	resultSkippedLocked                       // row was locked by concurrent UpdateUserEmailTx — skip
	resultSkippedDecrypt                      // email could not be decrypted — skip, log, self-heal on next email change
)

// BackfillEmailPrefixIndices populates user_email_prefix_indices for all existing users
// that do not yet have any prefix rows. Called at startup after MigrateUp and BackfillEncryption.
//
// Idempotency guarantees:
//   - Re-run after crash: NOT EXISTS filter skips already-processed users (fast no-op)
//   - Race with UpdateUserEmailTx: FOR UPDATE SKIP LOCKED skips locked rows; the update itself
//     writes correct rows atomically, so no stale rows accumulate
//   - Decrypt error: log + skip; user gains prefix rows on their next email change
func BackfillEmailPrefixIndices(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor) error {
	if db == nil || enc == nil {
		return nil // encryption not configured (local dev without ENCRYPTION_KEY)
	}

	// Load IDs for users that have no prefix rows yet.
	// We do NOT load the email here — we re-read it inside a per-user transaction
	// with a FOR UPDATE SKIP LOCKED lock to prevent races with UpdateUserEmailTx.
	rows, err := db.Query(ctx, `
		SELECT u.id
		FROM users u
		WHERE u.is_deleted = FALSE
		  AND NOT EXISTS (
		      SELECT 1 FROM user_email_prefix_indices p
		      WHERE p.user_id = u.id
		  )
	`)
	if err != nil {
		return fmt.Errorf("query users without prefix indices: %w", err)
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan user id: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}
	if len(userIDs) == 0 {
		return nil // nothing to backfill — fast path on restarts after first successful run
	}

	slog.InfoContext(ctx, "#1178: backfilling email prefix blind indices", "count", len(userIDs))

	skippedLocked, skippedDecrypt := 0, 0
	for _, userID := range userIDs {
		result, err := backfillUserPrefixIndices(ctx, db, enc, userID)
		if err != nil {
			return fmt.Errorf("backfill user %s: %w", userID, err)
		}
		switch result {
		case resultSkippedLocked:
			skippedLocked++
		case resultSkippedDecrypt:
			skippedDecrypt++
		}
	}

	if skippedDecrypt > 0 {
		slog.WarnContext(ctx, "#1178: backfill complete with decrypt errors (affected users gain prefix rows on next email change)",
			"processed", len(userIDs)-skippedLocked-skippedDecrypt,
			"skipped_locked", skippedLocked, "decrypt_errors", skippedDecrypt)
	} else {
		slog.InfoContext(ctx, "#1178: email prefix index backfill complete",
			"processed", len(userIDs)-skippedLocked, "skipped_locked", skippedLocked)
	}
	return nil
}

// backfillUserPrefixIndices processes one user inside its own transaction.
// Uses FOR UPDATE SKIP LOCKED so concurrent UpdateUserEmailTx (which also writes
// prefix rows atomically) is never raced: if that transaction holds the lock, we skip
// and it will have already written correct rows.
//
// Multi-instance note: if instance B skips a user locked by instance A, and instance A
// crashes before committing, that user will lack prefix rows until the next server restart.
// The backfill runs again at the next startup (BackfillEmailPrefixIndices is called every time),
// so the NOT EXISTS filter picks up the skipped user on the next run. This is the accepted
// trade-off from the design review (Codex Round 4 APPROVE).
func backfillUserPrefixIndices(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor, userID string) (backfillResult, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return resultProcessed, err
	}
	defer tx.Rollback(ctx)

	// Lock the user row — SKIP LOCKED means we skip rather than wait if UpdateUserEmailTx holds it.
	var encEmail string
	err = tx.QueryRow(ctx,
		"SELECT email FROM users WHERE id = $1 FOR UPDATE SKIP LOCKED", userID,
	).Scan(&encEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		// Row locked by a concurrent UpdateUserEmailTx — that update writes correct rows itself.
		// Or: locked by a concurrent backfill instance — they will commit or roll back; the next
		// startup run picks up any uncommitted users via NOT EXISTS.
		return resultSkippedLocked, nil
	}
	if err != nil {
		return resultProcessed, fmt.Errorf("lock user row: %w", err)
	}

	// Decrypt — handles both ENC$ ciphertext and legacy plaintext.
	var plainEmail string
	if appcrypto.IsEncrypted(encEmail) {
		plainEmail, err = enc.DecryptString(encEmail)
		if err != nil {
			// Corrupt or unreadable email — non-fatal. Prefix rows will be written on next email change.
			slog.WarnContext(ctx, "#1178: skipping user — cannot decrypt email",
				"user_id", userID, "error", err)
			return resultSkippedDecrypt, nil
		}
	} else {
		plainEmail = encEmail // legacy pre-encryption plaintext row
	}

	indices := appcrypto.EmailPrefixBlindIndices(enc, plainEmail)
	if len(indices) > 0 {
		if err = insertPrefixIndicesTx(ctx, tx, userID, indices); err != nil {
			return resultProcessed, fmt.Errorf("insert prefix rows: %w", err)
		}
	}

	return resultProcessed, tx.Commit(ctx)
}

// insertPrefixIndicesTx is the package-level helper used by the backfill (database package).
// Profile repository has its own identical private method — same logic, different package scope.
func insertPrefixIndicesTx(ctx context.Context, tx pgx.Tx, userID string, blindIndices []string) error {
	if len(blindIndices) == 0 {
		return nil
	}
	args := make([]any, 0, 1+len(blindIndices))
	args = append(args, userID)
	placeholders := make([]string, 0, len(blindIndices))
	for i, idx := range blindIndices {
		args = append(args, idx)
		placeholders = append(placeholders, fmt.Sprintf("($1, $%d)", i+2))
	}
	query := fmt.Sprintf(
		"INSERT INTO user_email_prefix_indices (user_id, prefix_blind_idx) VALUES %s ON CONFLICT DO NOTHING",
		strings.Join(placeholders, ", "),
	)
	_, err := tx.Exec(ctx, query, args...)
	return err
}
```

- [ ] **Step 4: Run unit tests to confirm PASS**

```bash
cd backend && go test ./internal/database/... -v -run TestBackfillEmailPrefixIndices_NilInputs
```

Expected: PASS

- [ ] **Step 5: Add startup call to main.go**

In `backend/cmd/server/main.go`, find the `BackfillEncryption` call (around line 293):

```go
		if err := database.BackfillEncryption(context.Background(), db, fieldEncryptor); err != nil {
			appLogger.Error("Encryption backfill failed — exiting to prevent duplicate accounts", "error", err)
			os.Exit(1)
		}
```

Add immediately after it:

```go
		// #1178: Populate prefix blind indices for existing users at startup.
		// Idempotent — skips users that already have rows. Fast no-op on restart.
		if err := database.BackfillEmailPrefixIndices(context.Background(), db, fieldEncryptor); err != nil {
			appLogger.Error("Email prefix index backfill failed — exiting", "error", err)
			os.Exit(1)
		}
```

- [ ] **Step 6: Compile-check main.go**

```bash
cd backend && go build ./cmd/server/...
```

Expected: builds without errors

- [ ] **Step 7: Run full backend tests**

```bash
cd backend && go test ./... -count=1
```

Expected: all tests PASS

- [ ] **Step 8: Commit**

```bash
git add backend/internal/database/backfill.go backend/internal/database/backfill_test.go \
        backend/cmd/server/main.go
git commit -m "#1178: Add BackfillEmailPrefixIndices; call at startup after BackfillEncryption"
```

---

## Task 7b: Integration tests for profile repo changes and backfill

pgxmock cannot validate FK constraints, `ON CONFLICT DO NOTHING`, or actual row counts.
These integration tests ensure prefix rows are actually written and maintained correctly.

**Files:**
- Create: `backend/internal/repository/profile_prefix_integration_test.go`
- Create: `backend/internal/database/backfill_prefix_integration_test.go`

- [ ] **Step 1: Create profile repository prefix integration tests**

```go
//go:build integration

package repository

// Integration tests for prefix blind index maintenance in ProfileRepository.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... -run TestProfilePrefixIndices_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestProfilePrefixIndices_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	email1 := "profileprefix-" + suffix + "@example.com"
	email2 := "updated-" + suffix + "@example.com"

	user := &domain.User{
		ClerkUserID:        "clerk_pfx_" + suffix,
		Email:              email1,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}

	// Create user via transaction
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	t.Run("CreateUserTx inserts prefix rows", func(t *testing.T) {
		var count int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", user.ID,
		).Scan(&count))
		assert.Greater(t, count, 0, "prefix rows should exist after CreateUserTx")
	})

	t.Run("insertPrefixIndicesTx is idempotent (ON CONFLICT DO NOTHING)", func(t *testing.T) {
		var beforeCount int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", user.ID,
		).Scan(&beforeCount))

		// Re-insert via CreateUserTx on same user ID would fail FK, so test via direct re-insert
		tx2, err := pool.Begin(ctx)
		require.NoError(t, err)
		// Manually call insertPrefixIndicesTx by using a duplicate email update
		_ = tx2.Rollback(ctx) // just use UpdateUserEmailTx below

		// UpdateUserEmailTx replaces prefix rows, so row count should be for email2
		tx3, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.UpdateUserEmailTx(ctx, tx3, user.ID, email1)) // same email — re-inserts
		require.NoError(t, tx3.Commit(ctx))

		var afterCount int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", user.ID,
		).Scan(&afterCount))
		assert.Equal(t, beforeCount, afterCount, "idempotent re-insert should not change row count")
	})

	t.Run("UpdateUserEmailTx replaces all prefix rows atomically", func(t *testing.T) {
		tx4, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.UpdateUserEmailTx(ctx, tx4, user.ID, email2))
		require.NoError(t, tx4.Commit(ctx))

		// No rows for old email prefix
		enc2 := newTestFieldEncryptor(t)
		oldPrefixIdx := enc2.BlindIndexWithLabel("email_prefix",
			string([]rune(email1)[:3]))
		var oldCount int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1 AND prefix_blind_idx = $2",
			user.ID, oldPrefixIdx,
		).Scan(&oldCount))
		assert.Equal(t, 0, oldCount, "old email prefix rows should be removed")

		// Rows exist for new email prefix
		newPrefixIdx := enc2.BlindIndexWithLabel("email_prefix",
			string([]rune(email2)[:3]))
		var newCount int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1 AND prefix_blind_idx = $2",
			user.ID, newPrefixIdx,
		).Scan(&newCount))
		assert.Equal(t, 1, newCount, "new email prefix rows should exist after update")
	})

	t.Run("ON DELETE CASCADE removes prefix rows on user delete", func(t *testing.T) {
		_, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)

		var count int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", user.ID,
		).Scan(&count))
		assert.Equal(t, 0, count, "CASCADE should delete all prefix rows when user is deleted")

		// Prevent the t.Cleanup from failing on the already-deleted user
		user.ID = uuid.New().String() // point cleanup at a non-existent ID
	})
}
```

- [ ] **Step 2: Create backfill integration test**

```go
//go:build integration

package database

// Integration tests for BackfillEmailPrefixIndices.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/database/... -run TestBackfillEmailPrefixIndices_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillEmailPrefixIndices_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestEnc(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	// Insert a test user with no prefix rows
	userID := insertTestUserForBackfill(t, pool, enc, "backfill-"+suffix+"@example.com")
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) })

	t.Run("backfill writes prefix rows for user without any", func(t *testing.T) {
		err := BackfillEmailPrefixIndices(ctx, pool, enc)
		require.NoError(t, err)

		var count int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", userID,
		).Scan(&count))
		assert.Greater(t, count, 0, "backfill should have written prefix rows")
	})

	t.Run("re-run is a no-op (ON CONFLICT DO NOTHING prevents duplicates)", func(t *testing.T) {
		var countBefore int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", userID,
		).Scan(&countBefore))

		err := BackfillEmailPrefixIndices(ctx, pool, enc)
		require.NoError(t, err)

		var countAfter int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", userID,
		).Scan(&countAfter))
		assert.Equal(t, countBefore, countAfter, "re-run should not create duplicate rows")
	})
}

func connectTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func insertTestUserForBackfill(t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor, email string) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	encEmail, err := enc.EncryptString(email)
	require.NoError(t, err)
	emailBlindIdx := enc.BlindIndex(strings.ToLower(strings.TrimSpace(email)))
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, email_blind_idx, email_verified,
		    subscription_tier, subscription_status, primary_region, is_active, is_deleted,
		    created_at, updated_at)
		VALUES ($1, $2, $3, $4, false, 'premium', 'active', 'iad', true, false, NOW(), NOW())`,
		userID, "clerk_backfill_"+userID, encEmail, emailBlindIdx,
	)
	require.NoError(t, err)
	// Ensure no prefix rows exist yet (simulates a pre-backfill state)
	_, err = pool.Exec(ctx, "DELETE FROM user_email_prefix_indices WHERE user_id = $1", userID)
	require.NoError(t, err)
	return userID
}
```

> **Note:** The `connectTestDB` function in the `database` package needs to be defined here (it's not shared with the `repository` package). Also add `"os"`, `"strings"`, `"github.com/google/uuid"`, and `appcrypto` to the import block.

- [ ] **Step 3: Run integration tests (require DATABASE_URL)**

```bash
cd backend && DATABASE_URL=postgres://localhost/boostedbudget_test \
  go test ./internal/repository/... -run TestProfilePrefixIndices_Integration -tags integration -v

cd backend && DATABASE_URL=postgres://localhost/boostedbudget_test \
  go test ./internal/database/... -run TestBackfillEmailPrefixIndices_Integration -tags integration -v
```

Expected: all sub-tests PASS

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/profile_prefix_integration_test.go \
        backend/internal/database/backfill_prefix_integration_test.go
git commit -m "#1178: Add integration tests for profile prefix rows and backfill"
```

---

## Task 8: Handler validation + doc strings + domain comment

**Files:**
- Modify: `backend/internal/huma/operations/admin_users_test.go`
- Modify: `backend/internal/huma/operations/admin.go`
- Modify: `backend/internal/domain/admin_user.go`

- [ ] **Step 1: Write failing handler tests for min-length validation**

Append to `backend/internal/huma/operations/admin_users_test.go`:

```go
// TestListUsers_SearchTooShort_Returns400 verifies that a search term shorter
// than 3 characters (after trimming) is rejected with 400 Bad Request.
func TestListUsers_SearchTooShort_Returns400(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, new(mockAdminUserService), nil, nil, cfg)
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		search string
	}{
		{"one char", "a"},
		{"two chars", "ab"},
		{"two chars with spaces that trim to two", "  ab  "},
		{"one char unicode", "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := &ListUsersInput{
				AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
					ClerkUserID:        "clerk_admin_123",
				},
				PagePaginationInput: inputs.PagePaginationInput{Page: 1, PageSize: 20},
				Search:              tc.search,
			}
			_, err := handler.ListUsers(ctx, input)
			require.Error(t, err)
			// Project pattern: check err.Error() contains the expected message (see admin_users_test.go)
			assert.Contains(t, err.Error(), "search must be at least 3 characters")
		})
	}
}

// TestListUsers_SearchExactlyThreeChars_Succeeds verifies that exactly 3 characters passes.
func TestListUsers_SearchExactlyThreeChars_Succeeds(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)
	ctx := context.Background()

	mockService.On("ListUsers", ctx, domain.AdminUserFilters{Search: "abc"}, 1, 20).
		Return(&domain.AdminUserListResponse{Users: []domain.AdminUserListItem{}, TotalCount: 0, Page: 1, PageSize: 20, TotalPages: 0}, nil)

	input := &ListUsersInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		PagePaginationInput: inputs.PagePaginationInput{Page: 1, PageSize: 20},
		Search:              "abc",
	}
	_, err := handler.ListUsers(ctx, input)
	require.NoError(t, err)
	mockService.AssertExpectations(t)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd backend && go test ./internal/huma/operations/... -v -run "TestListUsers_SearchTooShort|TestListUsers_SearchExactlyThree"
```

Expected: `TestListUsers_SearchTooShort_Returns400` FAIL — no error is returned (handler passes the short search to the service, which testify flags as an unexpected call); `TestListUsers_SearchExactlyThreeChars_Succeeds` already PASS

- [ ] **Step 3: Add validation guard to ListUsers handler**

In `backend/internal/huma/operations/admin.go`, find `ListUsers` and add the validation at the top of the handler body (after the nil check for `adminUserService`):

```go
	// Validate search length: min 3 runes (matches prefix index minimum).
	// Frontend suppresses API calls for search.length < 3, but defend server-side too.
	search := strings.TrimSpace(input.Search)
	if search != "" && len([]rune(search)) < 3 {
		return nil, huma.Error400BadRequest("search must be at least 3 characters")
	}
```

Also update the `filters` assignment to use the trimmed value:

```go
	filters := domain.AdminUserFilters{
		Search:  search,
		Tier:    input.Tier,
		SortBy:  input.SortBy,
		SortDir: input.SortDir,
	}
```

- [ ] **Step 4: Update the doc string on ListUsersInput.Search**

In `backend/internal/huma/operations/admin.go`, find `ListUsersInput` and change:

```go
	Search  string `query:"search"   doc:"Search by email (partial match)"`
```

to:

```go
	Search  string `query:"search"   doc:"Search by email prefix, starts-with match (min 3 chars)"`
```

- [ ] **Step 5: Update AdminUserFilters.Search comment**

In `backend/internal/domain/admin_user.go`, find `AdminUserFilters` and update the `Search` field comment:

```go
	Search string // Prefix email match (starts-with, min 3 chars, case-insensitive)
```

- [ ] **Step 6: Run tests to confirm GREEN**

```bash
cd backend && go test ./internal/huma/operations/... -v -run "TestListUsers_SearchTooShort|TestListUsers_SearchExactlyThree"
```

Expected: both new tests PASS

- [ ] **Step 7: Run full backend tests**

```bash
cd backend && go test ./... -count=1
```

Expected: all tests PASS

- [ ] **Step 8: Commit**

```bash
git add backend/internal/huma/operations/admin.go \
        backend/internal/huma/operations/admin_users_test.go \
        backend/internal/domain/admin_user.go
git commit -m "#1178: Add min-3-char validation guard to ListUsers; update doc strings and domain comment"
```

---

## Task 9: Code generation

**Files:**
- Generated: `api/openapi.yaml`
- Generated: `packages/api-client/` (Orval TypeScript types + hooks)

- [ ] **Step 1: Regenerate OpenAPI spec**

```bash
make generate-openapi
```

Expected: `api/openapi.yaml` updated — `search` parameter description now reads `"Search by email prefix, starts-with match (min 3 chars)"`

- [ ] **Step 2: Regenerate TypeScript API client**

```bash
make generate-api-client
```

Expected: `packages/api-client/` updated without errors; no workspace lockfiles created in subdirectories

- [ ] **Step 3: Verify generated types compile**

```bash
cd apps/web && bun run typecheck
```

Expected: no TypeScript errors

- [ ] **Step 4: Commit**

```bash
git add api/openapi.yaml packages/api-client/
git commit -m "#1178: Regenerate OpenAPI spec and API client after doc string update"
```

---

## Task 10: Frontend — suppress short searches + UI updates

**Files:**
- Modify: `apps/web/src/features/admin/hooks/useUserFilters.ts`
- Modify: `apps/web/src/features/admin/hooks/useUserFilters.test.tsx`
- Modify: `apps/web/src/features/admin/components/UsersPage.tsx`

- [ ] **Step 1: Write failing frontend tests for search suppression**

Append to `apps/web/src/features/admin/hooks/useUserFilters.test.tsx`:

```tsx
describe('apiParams search gating', () => {
  it('includes search in apiParams when search.length >= 3', () => {
    window.history.replaceState({}, '', '/admin/users?search=abc')
    const { result } = renderHook(() => useUserFilters(), {
      wrapper: createWrapper(),
    })
    expect(result.current.apiParams.search).toBe('abc')
  })

  it('excludes search from apiParams when search.length === 2', () => {
    window.history.replaceState({}, '', '/admin/users?search=ab')
    const { result } = renderHook(() => useUserFilters(), {
      wrapper: createWrapper(),
    })
    expect(result.current.apiParams.search).toBeUndefined()
  })

  it('excludes search from apiParams when search.length === 1', () => {
    window.history.replaceState({}, '', '/admin/users?search=a')
    const { result } = renderHook(() => useUserFilters(), {
      wrapper: createWrapper(),
    })
    expect(result.current.apiParams.search).toBeUndefined()
  })

  it('excludes search from apiParams when search is empty', () => {
    window.history.replaceState({}, '', '/admin/users')
    const { result } = renderHook(() => useUserFilters(), {
      wrapper: createWrapper(),
    })
    expect(result.current.apiParams.search).toBeUndefined()
  })

  it('includes search in apiParams when search.length === 3 exactly', () => {
    window.history.replaceState({}, '', '/admin/users?search=xyz')
    const { result } = renderHook(() => useUserFilters(), {
      wrapper: createWrapper(),
    })
    expect(result.current.apiParams.search).toBe('xyz')
  })
})
```

- [ ] **Step 2: Run to confirm the new tests FAIL**

```bash
cd apps/web && bun test src/features/admin/hooks/useUserFilters.test.tsx
```

Expected: the "excludes search" tests FAIL because `apiParams` currently includes any non-empty search

- [ ] **Step 3: Update apiParams in useUserFilters.ts**

In `apps/web/src/features/admin/hooks/useUserFilters.ts`, find the `apiParams` useMemo and replace:

```tsx
    if (filters.search) {
      params.search = filters.search
    }
```

with:

```tsx
    // Only pass search to the API when it meets the server's min-length requirement.
    // Count Array.from() runes (not .length bytes) to match Go's len([]rune(search)) < 3 check.
    // Trim whitespace so "  ab  " (2 meaningful chars) is not mistakenly sent to the API.
    // Pass the trimmed value so the backend receives what it validated.
    const trimmedSearch = filters.search.trim()
    if (trimmedSearch && Array.from(trimmedSearch).length >= 3) {
      params.search = trimmedSearch
    }
```

- [ ] **Step 4: Run to confirm tests PASS**

```bash
cd apps/web && bun test src/features/admin/hooks/useUserFilters.test.tsx
```

Expected: all tests including new search-gating tests PASS

- [ ] **Step 5: Update UsersPage.tsx search input**

In `apps/web/src/features/admin/components/UsersPage.tsx`, find the search `<input>` element and apply all three changes:

```tsx
<input
  type="text"
  className="w-full pl-9 pr-3 py-2 border border-border rounded-md bg-background text-foreground text-sm font-[inherit] transition-colors focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/15"
  placeholder="Search by email prefix (min. 3 chars)..."
  value={filters.search}
  onChange={(e) => setSearch(e.target.value)}
  aria-label="Search users by email prefix"
  minLength={3}
/>
```

Then add a hint text element immediately after the `</div>` that wraps the search input:

```tsx
{filters.search.length > 0 && filters.search.length < 3 && (
  <p className="text-xs text-muted-foreground mt-1">
    Keep typing — searches emails that start with your query (min. 3 chars)
  </p>
)}
```

> **Note:** The hint `<p>` sits inside the outer flex container but is outside the `relative` div wrapping the input. Make the outer search div `flex-col` or restructure slightly so the hint appears below the input without breaking the layout.

Revised search section in `UsersPage.tsx`:

```tsx
<div className="relative flex-1 min-w-[200px] max-w-[360px]">
  <div className="relative">
    <Search
      size={16}
      className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none"
      aria-hidden="true"
    />
    <input
      type="text"
      className="w-full pl-9 pr-3 py-2 border border-border rounded-md bg-background text-foreground text-sm font-[inherit] transition-colors focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/15"
      placeholder="Search by email prefix (min. 3 chars)..."
      value={filters.search}
      onChange={(e) => setSearch(e.target.value)}
      aria-label="Search users by email prefix"
      minLength={3}
    />
  </div>
  {filters.search.trim().length > 0 && Array.from(filters.search.trim()).length < 3 && (
    <p className="text-xs text-muted-foreground mt-1">
      Keep typing — searches emails that start with your query (min. 3 chars)
    </p>
  )}
</div>
```

- [ ] **Step 6: Run all frontend tests**

```bash
cd apps/web && bun run test && bun run typecheck && bun run lint
```

> **Note:** For targeted test runs during red-green cycles use `bun test src/features/admin/hooks/useUserFilters.test.tsx`. The full `bun run test` runs all suites.

Expected: all tests PASS, no TypeScript errors, no lint warnings

- [ ] **Step 7: Commit**

```bash
git add apps/web/src/features/admin/hooks/useUserFilters.ts \
        apps/web/src/features/admin/hooks/useUserFilters.test.tsx \
        apps/web/src/features/admin/components/UsersPage.tsx
git commit -m "#1178: Gate API search param on length>=3; update placeholder and hint text"
```

---

## Verification

### Full test suite

```bash
# Backend unit tests
cd backend && go test ./... -count=1

# Frontend tests + lint
cd apps/web && bun run test && bun run typecheck && bun run lint

# Backend integration tests (requires DATABASE_URL)
cd backend && DATABASE_URL=postgres://localhost/boostedbudget_test \
  go test ./internal/repository/... -run "TestListUsersPrefix_Integration|TestListUsersSort_Integration|TestProfilePrefixIndices_Integration" -tags integration -v

cd backend && DATABASE_URL=postgres://localhost/boostedbudget_test \
  go test ./internal/database/... -run "TestBackfillEmailPrefixIndices_Integration" -tags integration -v
```

### Manual smoke test

1. Start dev stack: `make dev`
2. Log in as admin
3. Navigate to `/admin/users`
4. Type `te` → hint text appears, no spinner, no API call in network tab
5. Type `tes` → spinner appears, results load showing users whose emails start with `tes`
6. Type `test@` → only users with emails starting `test@` appear
7. Type `test@gmail.com` → only that exact user (if exists) appears

### Migration rollback test

```bash
make migrate-down  # rolls back 000076
make migrate-up    # re-applies 000076
```

Expected: both succeed cleanly.
