# Admin User Email Prefix Search — Design Spec
**Issue:** #1178  
**Date:** 2026-05-24  
**Status:** Approved (revised after two Codex adversarial reviews)

---

## Problem

`GET /api/v1/admin/users?search=` is documented as "Search by email (partial match)" but the repository performs an exact HMAC-SHA256 blind-index equality lookup (`WHERE email_blind_idx = $1`). Searching for `test@` silently returns no results instead of matching `test@example.com`. The mislabel appears in four places: the Go `doc:` tag, the `AdminUserFilters.Search` domain comment, the generated TypeScript type comment, and the frontend placeholder text.

## Goal

Support true starts-with prefix search on admin email lookup without storing plaintext or decrypting emails at query time. Minimum 3-char search enforced at handler and frontend. Indexed prefixes capped at 64 chars.

---

## Chosen Approach: Prefix Blind Indices (separate table)

Store HMAC-SHA256 blind indices for every email prefix of rune-length `[3, min(len(email), 64)]` in a dedicated `user_email_prefix_indices` table. Admin search uses an EXISTS subquery against this table. Prefix rows are generated at user creation and email-change time within the existing transactions. A startup backfill populates existing users idempotently.

**Why not the alternatives:**
- *Doc-only fix*: leaves admins unable to search by partial email — poor admin UX.
- *Application-layer decrypt-and-filter*: O(n) decryption per search; doesn't scale.
- *GIN array on users*: slower point-lookup; bloats users row; complicates future rotation.

---

## Schema

### New migration: `000076_add_user_email_prefix_indices`

```sql
-- .up.sql
CREATE TABLE IF NOT EXISTS user_email_prefix_indices (
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    prefix_blind_idx TEXT NOT NULL,
    PRIMARY KEY (user_id, prefix_blind_idx)
);

-- Covering index: answers EXISTS(WHERE prefix_blind_idx = $1) without heap access
CREATE INDEX IF NOT EXISTS idx_uepi_prefix_blind_idx_user
    ON user_email_prefix_indices(prefix_blind_idx, user_id);
```

```sql
-- .down.sql
DROP TABLE IF EXISTS user_email_prefix_indices;
```

**Design notes:**
- No `key_version` column — existing `email_blind_idx` has no versioning; adding it only to prefix rows without rotation logic implies support that doesn't exist. Omit until key rotation is a first-class feature for all blind indices.
- `PRIMARY KEY (user_id, prefix_blind_idx)` — free dedup + fast `DELETE WHERE user_id = $1`
- Covering index `(prefix_blind_idx, user_id)` — both columns needed by the EXISTS subquery; no heap access required
- `ON DELETE CASCADE` — no orphan rows on user hard-delete

---

## Cryptography

### Domain-separated blind index

Add `BlindIndexWithLabel` to `backend/internal/crypto/field_encryptor.go`:

```go
// BlindIndexWithLabel produces a domain-separated HMAC-SHA256 blind index.
// Using a label prevents semantic overlap between exact-email and prefix blind indices,
// which matters if the HMAC key is ever rotated (dual-read window).
// Use label "email_prefix" for all prefix table reads/writes.
func (fe *FieldEncryptor) BlindIndexWithLabel(label, plaintext string) string {
    mac := hmac.New(sha256.New, fe.hmacKey)
    mac.Write([]byte(label + ":" + plaintext))
    return hex.EncodeToString(mac.Sum(nil))
}
```

The existing `BlindIndex()` method is unchanged — exact `email_blind_idx` lookups continue to use it.

### Normalization

**Do NOT introduce NFC normalization.** The existing email identity code (`profile_repository.go:387`, `backfill.go:154`) uses only `strings.ToLower(strings.TrimSpace(email))`. Prefix indices must use the same normalization to avoid prefix-search and exact-lookup disagreeing on the same email. Define a single helper that matches the existing convention:

```go
// NormalizeEmailForBlindIndex matches the existing email normalization used across
// profile_repository.go and backfill.go. Must be identical on creation, update,
// backfill, and admin search.
func NormalizeEmailForBlindIndex(email string) string {
    return strings.ToLower(strings.TrimSpace(email))
}
```

Place this in `backend/internal/crypto/field_encryptor.go` or a nearby `normalize.go` and use it everywhere that was previously inlining `strings.ToLower(strings.TrimSpace(...))`.

### Prefix generation

```go
const (
    minPrefixLen = 3
    maxPrefixLen = 64
)

// EmailPrefixBlindIndices generates domain-separated blind indices for all
// email prefixes of rune-length [minPrefixLen, maxPrefixLen].
// Rune-based slicing prevents splitting multi-byte UTF-8 characters.
// Returns nil if email normalizes to fewer than minPrefixLen runes.
func EmailPrefixBlindIndices(enc *FieldEncryptor, email string) []string {
    normalized := NormalizeEmailForBlindIndex(email)
    runes := []rune(normalized)
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

---

## Repository Changes

### `backend/internal/repository/admin_user_repository.go`

Replace the exact blind-index match in `ListUsers()`:

```go
// BEFORE:
clause += fmt.Sprintf(" AND u.email_blind_idx = $%d", argIndex)
args = append(args, r.enc.BlindIndex(strings.ToLower(strings.TrimSpace(filters.Search))))

// AFTER:
clause += fmt.Sprintf(
    " AND EXISTS (SELECT 1 FROM user_email_prefix_indices p"+
        " WHERE p.user_id = u.id AND p.prefix_blind_idx = $%d)", argIndex)
args = append(args, r.enc.BlindIndexWithLabel("email_prefix",
    crypto.NormalizeEmailForBlindIndex(filters.Search)))
```

Increment `argIndex` after appending the arg (already done in the existing clause-building loop — verify it's in the right place relative to other filters, LIMIT, and OFFSET).

`EXISTS` is preferred over JOIN here to avoid potential duplicate rows and to match the existing filter-clause structure.

### `backend/internal/repository/profile_repository.go`

**Add private batch-insert helper** using a single multi-row INSERT (not `pgx.SendBatch` — the project doesn't use it and batch result-draining footguns are real):

```go
func (r *profileRepository) insertPrefixIndicesTx(
    ctx context.Context, tx pgx.Tx, userID string, blindIndices []string,
) error {
    if len(blindIndices) == 0 {
        return nil
    }
    // Build: INSERT INTO user_email_prefix_indices (user_id, prefix_blind_idx)
    //        VALUES ($1, $2), ($1, $3), ... ON CONFLICT DO NOTHING
    // Single statement, no per-row round trips.
}
```

**Hook into `CreateUserTx()`** — after `INSERT INTO users`, within the same transaction:

```go
indices := crypto.EmailPrefixBlindIndices(r.enc, user.Email)
if err := r.insertPrefixIndicesTx(ctx, tx, user.ID, indices); err != nil {
    return err
}
```

**Hook into `UpdateUserEmailTx()`** — CRITICAL: within the same transaction as `UPDATE users` (atomically replace old prefix indices with new ones):

```go
// Delete old prefix indices first
if _, err := tx.Exec(ctx,
    "DELETE FROM user_email_prefix_indices WHERE user_id = $1", userID); err != nil {
    return err
}
// Insert new prefix indices for the updated email
indices := crypto.EmailPrefixBlindIndices(r.enc, plainEmail)
if err := r.insertPrefixIndicesTx(ctx, tx, userID, indices); err != nil {
    return err
}
```

This ensures that no matter what order backfill and email updates run, the prefix rows always reflect the user's current email after any transaction commits.

---

## Backfill

Add `BackfillEmailPrefixIndices` to `backend/internal/database/backfill.go`, mirroring the exact structure of `BackfillEncryption`. Called from `backend/cmd/server/main.go` at startup after `BackfillEncryption`:

```go
if err := database.BackfillEmailPrefixIndices(context.Background(), db, fieldEncryptor); err != nil {
    appLogger.Error("Email prefix index backfill failed — exiting", "error", err)
    os.Exit(1)
}
```

**Implementation (closely mirrors existing `backfillUsers` structure):**

```go
func BackfillEmailPrefixIndices(ctx context.Context, db *pgxpool.Pool, enc *appcrypto.FieldEncryptor) error {
    if db == nil || enc == nil {
        return nil // encryption not configured (local dev without ENCRYPTION_KEY)
    }

    // Idempotency filter: only select users who have NO prefix rows yet.
    // On re-run after a partial or crashed backfill, already-processed users are
    // excluded by the NOT EXISTS clause — re-run is a fast no-op for them.
    // This mirrors BackfillEncryption's use of "WHERE email_blind_idx IS NULL".
    rows, err := db.Query(ctx, `
        SELECT u.id, u.email
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

    var userIDs, encryptedEmails []string
    for rows.Next() {
        var id, email string
        if err := rows.Scan(&id, &email); err != nil {
            return fmt.Errorf("scan row: %w", err)
        }
        userIDs = append(userIDs, id)
        encryptedEmails = append(encryptedEmails, email)
    }
    if err := rows.Err(); err != nil {
        return fmt.Errorf("iterate rows: %w", err)
    }
    if len(userIDs) == 0 {
        return nil // nothing to backfill — fast path on subsequent restarts
    }

    slog.InfoContext(ctx, "#1178: backfilling email prefix blind indices", "count", len(userIDs))

    skipped := 0
    for i, userID := range userIDs {
        // Reuse the existing encryptWithBlind helper: handles both legacy plaintext
        // and ENC$ ciphertext transparently (same dual-read logic as backfillUsers).
        encEmail := encryptedEmails[i]
        var plainEmail string
        if appcrypto.IsEncrypted(encEmail) {
            plainEmail, err = enc.DecryptString(encEmail)
            if err != nil {
                // Per-user decrypt error: log and skip — prefix search for this user
                // won't work until their email is touched (CreateUserTx / UpdateUserEmailTx
                // regenerates prefix rows correctly). Non-fatal: prefix search is admin-only.
                slog.WarnContext(ctx, "#1178: skipping user — decrypt error",
                    "user_id", userID, "error", err)
                skipped++
                continue
            }
        } else {
            plainEmail = encEmail // legacy pre-encryption plaintext
        }

        indices := appcrypto.EmailPrefixBlindIndices(enc, plainEmail)
        if len(indices) == 0 {
            continue // email too short for any prefix — safe skip
        }

        // Single multi-row INSERT with ON CONFLICT DO NOTHING.
        // Handles the narrow race where UpdateUserEmailTx ran between the
        // NOT EXISTS query above and this insert — those rows are simply skipped.
        if err := insertPrefixIndicesDB(ctx, db, userID, indices); err != nil {
            return fmt.Errorf("insert prefix indices for user %s: %w", userID, err)
        }
    }

    slog.InfoContext(ctx, "#1178: email prefix index backfill complete",
        "processed", len(userIDs)-skipped, "skipped", skipped)
    return nil
}
```

**Idempotency guarantee (zero manual intervention):**
- `NOT EXISTS` filter: re-run on any restart processes only users who still lack prefix rows — already-processed users are excluded in O(1) at the DB level
- `ON CONFLICT DO NOTHING` on INSERT: handles the narrow race where an email update runs concurrently during a backfill run; no duplicate or stale rows can accumulate across restarts
- `nil` early return: if encryption is not configured (local dev), backfill is a no-op
- Per-user decrypt errors: logged and skipped; those users' prefix rows are written correctly by their next `CreateUserTx` or `UpdateUserEmailTx` call — no manual fix needed
- Rolling deployment safe: multiple instances running concurrently all see the same `NOT EXISTS` filter and produce the same `ON CONFLICT DO NOTHING` inserts — no races across instances
- Migration ordering: `BackfillEmailPrefixIndices` is called after `MigrateUp()` in `main.go`, so the table always exists before the backfill runs

---

## Validation & API

### `backend/internal/huma/operations/admin.go`

Updated `ListUsersInput`:

```go
type ListUsersInput struct {
    inputs.AdminAuthenticatedInput
    inputs.PagePaginationInput
    Search  string `query:"search"   doc:"Search by email prefix, starts-with match (min 3 chars)"`
    Tier    string `query:"tier"     doc:"Filter by subscription tier"`
    SortBy  string `query:"sort_by"  enum:"created_at,last_login_at,subscription_tier,subscription_status" default:"created_at" doc:"Column to sort by"`
    SortDir string `query:"sort_dir" enum:"asc,desc" default:"desc" doc:"Sort direction"`
}
```

Add validation guard at the top of `ListUsers()`, following the existing handler validation convention in `mapper.go` (use the same error type and pattern used elsewhere for semantic validation failures — check `mapper.go:145` for the project convention before choosing between 400 and 422):

```go
search := strings.TrimSpace(input.Search)
if search != "" && len([]rune(search)) < 3 {
    return nil, huma.Error422UnprocessableEntity("search must be at least 3 characters")
    // Note: verify project convention in mapper.go — use 400 if that's what other
    // handlers use for semantic validation errors
}
```

### `backend/internal/domain/admin_user.go`

Update `AdminUserFilters.Search` comment:
```go
Search string // Prefix email match (starts-with, min 3 chars, case-insensitive)
```

---

## Frontend

### `apps/web/src/features/admin/components/UsersPage.tsx`

**REQUIRED (prevents 422 storm):** Suppress the API call when `search.length > 0 && search.length < 3`. The current code calls `useListAdminUsers(apiParams)` unconditionally — this must be guarded:

```tsx
// In useUserFilters.ts or UsersPage.tsx — suppress short searches at the filter level
const apiSearch = search.length >= 3 ? search : undefined;
// Pass apiSearch (not search) to the API hook params
```

Additional UI changes:
- Placeholder: `"Search by email prefix (min. 3 chars)..."`
- `minLength={3}` on the input element
- Hint text below input: `"Searches emails that start with your query"`

Generated TypeScript type comments auto-update via `make generate-api-client` (the `doc:` tag change propagates through Orval).

---

## Testing Strategy (TDD)

### Red-Green order

**1. Crypto unit tests** (write first):
- `EmailPrefixBlindIndices("test@example.com")` → correct count (12 indices for 14-char email, min 3)
- `EmailPrefixBlindIndices("ab")` → nil (below minimum)
- `EmailPrefixBlindIndices("")` → nil (safe empty case)
- `BlindIndexWithLabel("email_prefix", "x") != BlindIndex("x")` (domain separation)
- Rune-safe: multi-byte email returns correct prefix count

**2. Repository integration tests** (`admin_user_repository_integration_test.go` — NOT pgxmock):
   Following the existing `//go:build integration` pattern. pgxmock cannot catch SQL planner errors, constraint violations, or aggregate issues.
   - `ListUsers(search="tes")` → returns user whose email starts with "tes"
   - `ListUsers(search="tes")` → does NOT return user whose email doesn't start with "tes"
   - `ListUsers(search="@example")` → empty (not starts-with, not contained)
   - `ListUsers(search="tes", tier="premium")` → intersection filter works
   - `ListUsers(search="tes")` with pagination → correct total count
   - Email update → old prefix no longer matches; new prefix does
   - Cascade: deleting user removes all prefix rows

**3. Profile repository integration tests**:
   - `CreateUserTx` → prefix rows exist after creation
   - `insertPrefixIndicesTx` idempotent → re-run inserts 0 new rows
   - `UpdateUserEmailTx` → old email's prefix rows gone; new email's prefix rows present; no mix

**4. Backfill tests**:
   - Fresh table with N users → all have prefix rows after backfill
   - Re-run → no duplicates (ON CONFLICT DO NOTHING)
   - User with decrypt error → skipped without aborting, other users processed

**5. Handler tests** (unit, pgxmock OK for this layer):
   - `search=""` → no search clause applied
   - `search="ab"` → returns 422 (or 400 per convention)
   - `search="  ab  "` (trimmed = 2 chars) → returns 422
   - `search="abc"` → 200, passes filter to service

**6. E2E** (Playwright):
   - Admin types "test@" → sees users whose emails start with "test@"
   - Admin types "te" → no API call fired (frontend suppression), hint visible

---

## Files Changed

| File | Change |
|------|--------|
| `backend/migrations/000076_add_user_email_prefix_indices.up.sql` | New table + covering index |
| `backend/migrations/000076_add_user_email_prefix_indices.down.sql` | Drop table |
| `backend/internal/crypto/field_encryptor.go` | Add `BlindIndexWithLabel`, `NormalizeEmailForBlindIndex`, `EmailPrefixBlindIndices` |
| `backend/internal/repository/admin_user_repository.go` | Replace exact match with EXISTS prefix subquery |
| `backend/internal/repository/profile_repository.go` | Add `insertPrefixIndicesTx`; hook into `CreateUserTx` + `UpdateUserEmailTx` |
| `backend/internal/database/backfill.go` | Add `BackfillEmailPrefixIndices` |
| `backend/cmd/server/main.go` | Call `BackfillEmailPrefixIndices` at startup after `BackfillEncryption` |
| `backend/internal/huma/operations/admin.go` | Update doc string; add min-length validation guard |
| `backend/internal/domain/admin_user.go` | Update `Search` field comment |
| `apps/web/src/features/admin/components/UsersPage.tsx` | Suppress short searches; update placeholder/hint |
| `apps/web/src/features/admin/hooks/useUserFilters.ts` | Gate API search param on length ≥ 3 |

**Run after backend changes:** `make generate-openapi && make generate-api-client`

> **Dependency note:** `NormalizeEmailForBlindIndex` uses only stdlib (`strings`). No new imports needed.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Backfill computes HMAC on ciphertext instead of email | Explicitly decrypt before generating indices; `enc.DecryptString(encryptedEmail)` |
| Backfill race with concurrent email update | `UpdateUserEmailTx` atomically replaces all prefix rows; stale rows from backfill race self-heal |
| Long emails → many rows | Capped at 64-char prefix; ~62 rows per user max |
| UTF-8 prefix splitting | Rune-based slicing |
| Frontend 422 storm on short input | Frontend suppresses API call when `0 < search.length < 3` |
| pgxmock misses SQL bugs | Integration tests required for all repo changes |
| Prefix table leaks email length | Accepted trade-off; admin-only, HMAC key required to reverse |
| Search returns false positives for common prefixes | Acceptable for admin use; minimum 3 chars limits broadness |
| Future key rotation | Schema is straightforward to extend; no `key_version` complexity until rotation is a full feature |
