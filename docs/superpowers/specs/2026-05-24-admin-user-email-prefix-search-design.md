# Admin User Email Prefix Search — Design Spec
**Issue:** #1178  
**Date:** 2026-05-24  
**Status:** Approved

---

## Problem

`GET /api/v1/admin/users?search=` is documented as "Search by email (partial match)" but the repository performs an exact HMAC-SHA256 blind-index equality lookup (`WHERE email_blind_idx = $1`). Searching for `test@` silently returns no results instead of matching `test@example.com`. The mislabel appears in four places: the Go `doc:` tag, the domain `AdminUserFilters.Search` comment, the generated TypeScript type comment, and the frontend placeholder text.

## Goal

Support true starts-with prefix search on admin email lookup without storing plaintext or decrypting emails at query time. Minimum 3-char search, results capped at indexed prefixes ≤ 64 chars.

---

## Chosen Approach: Prefix Blind Indices (separate table)

Store HMAC-SHA256 blind indices for every email prefix of length 3..min(len(email), 64) in a dedicated `user_email_prefix_indices` table. Admin search JOINs against this table. New rows are generated at user creation and email-change time (within the existing transactions). A startup backfill populates existing users idempotently.

**Why not the alternatives:**
- *Doc-only fix*: leaves admins unable to search by partial email — poor admin UX.
- *Application-layer decrypt-and-filter*: O(n) decryption per search; doesn't scale.
- *GIN array on users*: slower point-lookup than B-tree; bloats the users row; complicates key rotation.

---

## Schema

### New migration: `000076_add_user_email_prefix_indices`

```sql
-- .up.sql
CREATE TABLE IF NOT EXISTS user_email_prefix_indices (
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    prefix_blind_idx TEXT NOT NULL,
    key_version      TEXT NOT NULL DEFAULT 'v1',
    PRIMARY KEY (user_id, prefix_blind_idx)
);

-- Covering index: satisfies EXISTS(... WHERE prefix_blind_idx = $1) with no heap access
CREATE INDEX IF NOT EXISTS idx_uepi_prefix_blind_idx_user
    ON user_email_prefix_indices(prefix_blind_idx, user_id);
```

```sql
-- .down.sql
DROP TABLE IF EXISTS user_email_prefix_indices;
```

**Design notes:**
- `PRIMARY KEY (user_id, prefix_blind_idx)` — free dedup + fast `DELETE WHERE user_id = $1`
- `key_version = 'v1'` — enables future HMAC key rotation via dual-read/dual-write without full table rebuild; existing `email_blind_idx` lacks this, so this is an improvement over the legacy column
- `ON DELETE CASCADE` — no orphan rows on user hard-delete

---

## Cryptography

### Domain-separated blind index

Add `BlindIndexWithLabel` to `backend/internal/crypto/field_encryptor.go`:

```go
// BlindIndexWithLabel produces a domain-separated HMAC-SHA256 blind index.
// The label prevents semantic overlap between exact and prefix blind indices,
// which matters during key rotation (dual-read window).
func (fe *FieldEncryptor) BlindIndexWithLabel(label, plaintext string) string {
    mac := hmac.New(sha256.New, fe.hmacKey)
    mac.Write([]byte(label + ":" + plaintext))
    return hex.EncodeToString(mac.Sum(nil))
}
```

Use `BlindIndexWithLabel("email_prefix", prefix)` for all prefix table reads/writes.  
The existing `BlindIndex()` method is unchanged; exact `email_blind_idx` lookups continue to use it.

### Centralized normalization

Add `NormalizeEmail` in `backend/internal/crypto/field_encryptor.go` (or a nearby `normalize.go`):

```go
import "golang.org/x/text/unicode/norm"

// NormalizeEmail applies NFC Unicode normalization, lowercase, and trim.
// MUST be called identically in user creation, email update, backfill, and admin search.
func NormalizeEmail(email string) string {
    return strings.ToLower(strings.TrimSpace(norm.NFC.String(email)))
}
```

### Prefix generation

```go
const (
    minPrefixLen = 3
    maxPrefixLen = 64
)

// EmailPrefixBlindIndices generates blind indices for all email prefixes
// of rune-length [minPrefixLen, maxPrefixLen]. Rune-based slicing prevents
// splitting multi-byte UTF-8 characters.
func EmailPrefixBlindIndices(enc *FieldEncryptor, email string) []string {
    runes := []rune(NormalizeEmail(email))
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
args = append(args, r.enc.BlindIndexWithLabel("email_prefix", crypto.NormalizeEmail(filters.Search)))
```

The covering index `(prefix_blind_idx, user_id)` means the EXISTS subquery is answered from the index alone.

### `backend/internal/repository/profile_repository.go`

Add private batch-insert helper:

```go
func (r *profileRepository) insertPrefixIndicesTx(
    ctx context.Context, tx pgx.Tx, userID string, blindIndices []string,
) error {
    // Build batch INSERT with ON CONFLICT DO NOTHING
    // Use pgx.Batch for efficiency
}
```

**Hook into `CreateUserTx()`** — after the `INSERT INTO users`, within the same transaction:

```go
indices := crypto.EmailPrefixBlindIndices(r.enc, user.Email)
if err := r.insertPrefixIndicesTx(ctx, tx, user.ID, indices); err != nil {
    return err
}
```

**Hook into `UpdateUserEmailTx()`** — within the same transaction as the `UPDATE users`:

```go
// Atomically replace old prefix indices with new ones
if _, err := tx.Exec(ctx, "DELETE FROM user_email_prefix_indices WHERE user_id = $1", userID); err != nil {
    return err
}
indices := crypto.EmailPrefixBlindIndices(r.enc, plainEmail)
if err := r.insertPrefixIndicesTx(ctx, tx, userID, indices); err != nil {
    return err
}
```

---

## Backfill

Add `BackfillEmailPrefixIndices` to `backend/internal/database/backfill.go` (alongside existing `BackfillEncryption`). Call it from `backend/cmd/server/main.go` at startup, after `BackfillEncryption`:

```go
if err := database.BackfillEmailPrefixIndices(context.Background(), db, fieldEncryptor); err != nil {
    appLogger.Error("Email prefix index backfill failed", "error", err)
    os.Exit(1)
}
```

**Backfill algorithm:**

```go
func BackfillEmailPrefixIndices(ctx context.Context, db *pgxpool.Pool, enc *crypto.FieldEncryptor) error {
    // Process users in batches of 100
    // For each batch:
    //   SELECT id, email FROM users
    //   WHERE is_deleted = FALSE
    //   ORDER BY id
    //   OFFSET $cursor LIMIT 100
    //   FOR UPDATE SKIP LOCKED   ← skips users locked by concurrent email updates
    //
    // For each user row:
    //   1. Decrypt email (skip on decrypt error, log user ID)
    //   2. Generate EmailPrefixBlindIndices(enc, email)
    //   3. INSERT INTO user_email_prefix_indices ... ON CONFLICT DO NOTHING
    //   4. Commit per-user (or per-batch)
    //
    // Idempotent: existing rows are silently skipped
    // SKIP LOCKED: users being updated concurrently are skipped;
    //   UpdateUserEmailTx will write correct rows for them
}
```

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

Add guard in `ListUsers()` handler before building filters:

```go
search := strings.TrimSpace(input.Search)
if search != "" && len([]rune(search)) < 3 {
    return nil, huma.Error422UnprocessableEntity("search must be at least 3 characters")
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

- Placeholder: `"Search by email prefix (min. 3 chars)..."`
- `minLength={3}` on the input element
- Add descriptive hint below input: `"Searches emails that start with your query"`
- Suppress search API call while `search.length > 0 && search.length < 3` (debounce guard already exists; add length check)

Generated TypeScript type comments auto-update via `make generate-api-client`.

---

## Testing Strategy (TDD)

### Red-Green order

1. **Repository integration tests** (write first, against real DB):
   - `InsertPrefixIndicesTx` → correct row count for a known email
   - `InsertPrefixIndicesTx` → idempotent on re-run (`ON CONFLICT DO NOTHING`)
   - `ListUsers(search="tes")` → returns user whose email starts with "tes"
   - `ListUsers(search="tes")` → does NOT return user whose email doesn't start with "tes"
   - `ListUsers(search="@example")` → returns empty (not starts-with)
   - `UpdateUserEmailTx` → old prefix no longer matches; new prefix matches
   - Cascade: deleting user removes prefix rows

2. **Operation handler tests**:
   - `search=""` → no filter applied (all users returned)
   - `search="ab"` (2 chars) → 422
   - `search="abc"` (3 chars) → 200 with prefix filter applied
   - `search="  ab  "` (trimmed = 2 chars) → 422

3. **Backfill tests**:
   - Backfill on fresh table with 3 users → all 3 have prefix rows
   - Re-run backfill → no duplicates (idempotent)
   - User with no email (if possible) → skipped without error

4. **Frontend tests**:
   - Input with 2 chars → no API call fired
   - Input with 3 chars → API call fires with `search=abc`
   - Hint text visible
   - E2E: admin types "test@" → sees users whose emails start with "test@"

---

## Files Changed

| File | Change |
|------|--------|
| `backend/migrations/000076_add_user_email_prefix_indices.up.sql` | New table + indexes |
| `backend/migrations/000076_add_user_email_prefix_indices.down.sql` | Drop table |
| `backend/internal/crypto/field_encryptor.go` | Add `BlindIndexWithLabel`, `NormalizeEmail`, `EmailPrefixBlindIndices` |
| `backend/internal/repository/admin_user_repository.go` | Replace exact match with EXISTS prefix subquery |
| `backend/internal/repository/profile_repository.go` | Add `insertPrefixIndicesTx`; hook into `CreateUserTx` + `UpdateUserEmailTx` |
| `backend/internal/database/backfill.go` | Add `BackfillEmailPrefixIndices` |
| `backend/cmd/server/main.go` | Call `BackfillEmailPrefixIndices` at startup |
| `backend/internal/huma/operations/admin.go` | Update doc string; add 422 guard |
| `backend/internal/domain/admin_user.go` | Update `Search` field comment |
| `apps/web/src/features/admin/components/UsersPage.tsx` | Update placeholder, add minLength + hint |

**Run after backend changes:** `make generate-openapi && make generate-api-client`

> **Dependency note:** `NormalizeEmail` uses `golang.org/x/text/unicode/norm`. Already present in `backend/go.mod` as an indirect dependency (v0.36.0) — no new dependency needed; just add the import.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Backfill fails mid-run | Idempotent — safe to re-run on next restart; `SKIP LOCKED` avoids blocking email updates |
| Email change races with backfill | `FOR UPDATE SKIP LOCKED` in backfill; `UpdateUserEmailTx` writes correct rows transactionally |
| Long emails → many rows | Capped at 64-char prefix length |
| Unicode splitting | Rune-based prefix generation |
| Prefix table leaks email length | Accepted trade-off; admin-only, not user-facing; HMAC key still required to reverse |
| Future HMAC key rotation | `key_version` column enables dual-read/dual-write rotation without full rebuild |
| Search semantics confusion | UI says "starts with"; doc says "starts-with match"; 422 on <3 chars |
