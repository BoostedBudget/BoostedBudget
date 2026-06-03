# Issue #1010: Enforce Plaid Item Uniqueness Across All Users

## Context

BoostedBudget allows users to link bank accounts via Plaid. Currently, uniqueness is only enforced per-user — nothing prevents two different users from linking the same physical bank account. This opens a gaming vector: a user could create multiple BoostedBudget accounts and link the same bank account to each, bypassing subscription tier limits or abusing trials.

Plaid's `item_id` is unique per Link session (not per physical account), so the same bank account linked by two users produces different `item_id` values. We need a stable cross-session, cross-user identifier to detect duplicates.

## Decision Summary

| Decision | Choice |
|----------|--------|
| **Primary identifier** | `persistent_account_id` (Plaid's stable cross-session ID) |
| **Fallback identifier** | `institution_id \| mask \| type \| subtype` (when persistent ID unavailable) |
| **Enforcement** | DB-level partial UNIQUE index + application-level pre-check |
| **Joint accounts** | Blocked — household subscription is the path for shared accounts |
| **Existing data** | No migration needed (no production users yet) |
| **Disconnect policy** | Manual disconnect = hard delete = immediate release. Subscription disconnect (soft) = 90-day TTL hold |
| **Account deletion** | Fingerprints released only on irreversible CASCADE delete (not on deletion request) |

## Design

### New Columns on `accounts` Table

| Column | Type | Description |
|--------|------|-------------|
| `persistent_account_id` | TEXT, nullable | Plaid's stable cross-session account identifier, encrypted at rest |
| `persistent_account_id_blind_idx` | TEXT, nullable | Blind index of `persistent_account_id` for UNIQUE enforcement |
| `fallback_fingerprint_blind_idx` | TEXT, nullable | Blind index of `institution_id\|mask\|type\|subtype` for fallback UNIQUE enforcement |

Two separate fingerprint columns instead of one mixed column, addressing the concern that mixed identifier availability could let the same account bypass the check (user A gets persistent_id, user B doesn't → different single fingerprints → no collision).

### Fingerprint Computation

```
# Primary: persistent_account_id (when available from Plaid)
persistent_blind = BlindIndex("pid:" + persistent_account_id)

# Fallback: always computed regardless of persistent_id availability
mask_value = COALESCE(mask, "__NO_MASK__")
subtype_value = COALESCE(subtype, "__NO_SUBTYPE__")
fallback_blind = BlindIndex("fb:" + institution_id + "\x00" + mask_value + "\x00" + type + "\x00" + subtype_value)
```

Key design decisions (from Codex review):
- **Namespaced prefixes** (`pid:`, `fb:`) prevent theoretical cross-namespace hash collisions
- **Null-byte delimiter** (`\x00`) instead of `|` to eliminate ambiguity if components contain `|`
- **Sentinel values** for NULL mask/subtype ensure they participate in uniqueness
- **Both fingerprints always stored** — fallback is always computed (even when persistent_id exists) as a safety net
- **`subtype` included** in fallback — reduces false positives significantly (e.g., distinguishes checking vs savings at same institution with same last 4 digits)

### Server-Side Institution ID Verification

The `institution_id` parameter comes from the frontend (client-supplied). To prevent tampering, verify it against Plaid's server-side data:

```go
// After AccountsGet, verify institution_id from Plaid's response
plaidItem := accountsResp.GetItem()
serverInstitutionID := plaidItem.GetInstitutionId()
if serverInstitutionID != nil && *serverInstitutionID != institutionID {
    // Use Plaid's server-side value, log the mismatch
    institutionID = *serverInstitutionID
}
```

### Database Constraints

```sql
-- Migration 000062
ALTER TABLE accounts ADD COLUMN persistent_account_id TEXT;
ALTER TABLE accounts ADD COLUMN persistent_account_id_blind_idx TEXT;
ALTER TABLE accounts ADD COLUMN fallback_fingerprint_blind_idx TEXT;

-- Primary: catches duplicates when persistent_account_id is available
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS unique_persistent_account_id_active
    ON accounts (persistent_account_id_blind_idx)
    WHERE persistent_account_id_blind_idx IS NOT NULL AND status = 'active';

-- Fallback: catches duplicates when persistent_account_id is unavailable
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS unique_fallback_fingerprint_active
    ON accounts (fallback_fingerprint_blind_idx)
    WHERE fallback_fingerprint_blind_idx IS NOT NULL AND status = 'active';
```

Using `CREATE UNIQUE INDEX CONCURRENTLY` for zero-downtime safety (even though no production users yet, establishing good migration habits).

Two separate partial UNIQUE indexes, both only covering `status = 'active'`.

### Application-Level Check in `LinkAccounts()`

Between Step 4 (fetch Plaid accounts) and Step 8 (create linked item), add a fingerprint ownership check:

```go
// Step 6: Check cross-user account uniqueness (Issue #1010)
// Verify institution_id server-side from Plaid response
plaidItem := accountsResp.GetItem()
if serverInstID := plaidItem.GetInstitutionId(); serverInstID != nil && *serverInstID != "" {
    institutionID = *serverInstID
}

for _, plaidAccount := range plaidAccounts {
    persistentBlind, fallbackBlind := computeFingerprints(plaidAccount, institutionID, s.enc)

    // Check both fingerprints — either one matching means same physical account
    for _, fp := range []string{persistentBlind, fallbackBlind} {
        if fp == "" {
            continue
        }
        ownership, err := s.accountRepo.CheckFingerprintOwnership(ctx, fp, user.ID)
        if err != nil && !errors.Is(err, repository.ErrAccountNotFound) {
            return nil, fmt.Errorf("failed to check account fingerprint: %w", err)
        }
        if ownership != nil {
            if shouldReleaseFingerprint(ownership.ItemStatus, ownership.StatusChangedAt) {
                if err := s.accountRepo.ExpireAccountsByFingerprint(ctx, fp); err != nil {
                    return nil, fmt.Errorf("failed to expire old accounts: %w", err)
                }
            } else {
                return nil, ErrAccountOwnedByAnotherUser
            }
        }
    }
}
```

### Fingerprint Ownership Query

```sql
SELECT a.user_id, li.status, li.status_changed_at
FROM accounts a
JOIN linked_items li ON a.linked_item_id = li.id
WHERE (a.persistent_account_id_blind_idx = $1 OR a.fallback_fingerprint_blind_idx = $1)
  AND a.status = 'active'
  AND a.user_id != $2
LIMIT 1
```

Note: The query does NOT filter on `users.deletion_requested_at`. Per Codex review, deletion requests are cancelable (`CancelDeletion()`), so releasing fingerprints on request would create a race where another user claims the account before cancellation. Fingerprints are only released on irreversible CASCADE delete.

### Release Decision Logic

| Item Status | Age (since status change) | Action |
|-------------|---------------------------|--------|
| `active`, `login_required`, `error`, `pending_expiration` | Any | **Block** — account is in use |
| `disconnected` | < 90 days | **Block** — within grace period |
| `disconnected` | >= 90 days | **Allow** — expire old accounts, proceed |
| `revoked` | < 90 days | **Block** — within grace period |
| `revoked` | >= 90 days | **Allow** — expire old accounts, proceed |
| `replaced` | Any | N/A — accounts already `replaced` status, not in UNIQUE index |
| Manual disconnect (hard delete) | N/A | **Allow** — CASCADE deletes accounts and fingerprints |
| User hard-deleted (CASCADE) | N/A | **Allow** — rows physically removed |

### TTL Clock

The `linked_items.updated_at` column is fragile for TTL because it's updated by generic writes. Add a `status_changed_at` column to `linked_items` that is set only when `status` changes:

```sql
-- In migration 000062
ALTER TABLE linked_items ADD COLUMN status_changed_at TIMESTAMPTZ;
-- Backfill with updated_at for existing rows
UPDATE linked_items SET status_changed_at = updated_at WHERE status_changed_at IS NULL;
```

The service layer sets `status_changed_at` whenever it changes `linked_items.status`.

### Race Condition Protection

The two partial UNIQUE indexes handle the race condition where two users link simultaneously. If both pass the application-level check, the second INSERT fails on the constraint violation, which maps to `ErrAccountOwnedByAnotherUser`.

**Known limitation:** `LinkAccounts()` creates the `linked_items` row before inserting accounts (existing code pattern). If the account insert fails due to fingerprint collision, an orphaned linked item remains. This is a pre-existing issue (there's already a TODO in the code at `account_service.go:431`). For now, accept this — the orphaned linked item has no accounts and is inert. A follow-up issue can wrap the whole flow in a transaction.

### Error Handling

```go
// New error
ErrAccountOwnedByAnotherUser = errors.New("one or more accounts are already linked by another user")
```

Error mapper: `ErrAccountOwnedByAnotherUser` → HTTP 409 Conflict with message "This bank account is already linked to another BoostedBudget account. If you share this account with someone, consider a household subscription."

### Account Status Addition

Add `AccountStatusExpired` to handle TTL-released accounts:

```go
AccountStatusExpired AccountStatus = "expired"  // Fingerprint released after TTL
```

The `status` CHECK constraint on the `accounts` table needs updating to include 'expired'.

**Downstream impact:** Expired accounts should NOT appear in account listings or count toward subscription limits. Update `GetByUserID()` and `CountByUserID()` queries to filter `WHERE status = 'active'`.

### Re-link Scenarios

| Scenario | Behavior |
|----------|----------|
| Same user re-links same institution | Old accounts soft-deleted (status='replaced'), fingerprints released from index, new accounts created |
| Manual disconnect, different user links | Hard delete CASCADE removes accounts + fingerprints immediately, new user can link |
| Subscription disconnect, different user links within 90 days | Blocked by TTL |
| Subscription disconnect, different user links after 90 days | Old accounts expired, new user can link |
| User requests deletion, different user links within 30 days | **Blocked** — fingerprints held until irreversible delete |
| Hard delete (CASCADE after 30-day grace) | Accounts physically deleted, fingerprints gone, new user can link |
| User cancels deletion after requesting | Fingerprints remain held, no race condition |

## Known Limitations

1. **Mixed persistent_account_id availability:** If one user's link returns `persistent_account_id` and another's doesn't, the persistent-ID index won't catch the collision. The fallback index covers this case, but only if both have the same `institution_id + mask + type + subtype`.

2. **Fallback false positives:** Two legitimate different accounts at the same institution could share `mask + type + subtype`. This is rare (especially with subtype included) but theoretically possible. If this becomes an issue, we can add an admin override endpoint.

3. **Orphaned linked items on race:** The losing side of a uniqueness race leaves an orphaned `linked_items` row. Pre-existing limitation (TODO in codebase). Follow-up to wrap in transaction.

4. **Blind index key rotation:** All fingerprint blind indexes depend on the HMAC key. Key rotation requires a full backfill of fingerprint columns.

## Files to Modify

| File | Change |
|------|--------|
| `backend/migrations/000062_add_account_fingerprint.up.sql` | Add columns, 2 partial UNIQUE indexes (CONCURRENTLY), status CHECK update, status_changed_at on linked_items |
| `backend/migrations/000062_add_account_fingerprint.down.sql` | Rollback |
| `backend/internal/domain/account.go` | Add `PersistentAccountID`, `PersistentAccountIDBlindIdx`, `FallbackFingerprintBlindIdx` fields, `AccountStatusExpired` |
| `backend/internal/domain/linked_item.go` | Add `StatusChangedAt` field |
| `backend/internal/services/account_service.go` | `computeFingerprints()` helper, fingerprint check in `LinkAccounts()`, server-side institution_id verification, extract `persistent_account_id` in `convertPlaidAccount()`, set `status_changed_at` on status changes |
| `backend/internal/repository/account_repository.go` | Store fingerprints in `Create()`/`CreateBatch()`, add `CheckFingerprintOwnership()`, add `ExpireAccountsByFingerprint()`, update `GetByUserID()`/`CountByUserID()` to filter expired |
| `backend/internal/repository/linked_item_repository.go` | Persist `status_changed_at`, return in queries |
| `backend/internal/huma/errors/mapper.go` | Map `ErrAccountOwnedByAnotherUser` → 409 |
| `backend/internal/services/account_service_test.go` | Test: blocking, TTL expiry, same-user re-link, race condition, revoked status, server-side institution_id |
| `backend/internal/repository/account_repository_test.go` | Test: fingerprint storage, both UNIQUE constraints, TTL query, expired exclusion from listings |

## Testing Strategy

### Unit Tests (Service Layer)
- Mock `accountRepo.CheckFingerprintOwnership()` to test:
  - Blocks when another user owns fingerprint (active item)
  - Blocks during 90-day TTL (disconnected item < 90 days)
  - Blocks during 90-day TTL (revoked item < 90 days)
  - Allows after TTL expiry (disconnected >= 90 days)
  - Allows same-user re-link (fingerprint check skips same user_id)
  - Returns 409 error with correct message
  - Server-side institution_id verification overrides client value

### Integration Tests (Repository Layer)
- Test `unique_persistent_account_id_active` constraint fires on duplicate
- Test `unique_fallback_fingerprint_active` constraint fires on duplicate
- Test constraints allow same fingerprint when old account is `replaced` or `expired`
- Test `CheckFingerprintOwnership()` query with various item statuses
- Test `CountByUserID()` excludes expired accounts
- Test `GetByUserID()` excludes expired accounts

### Edge Cases
- NULL mask → sentinel value works correctly
- NULL subtype → sentinel value works correctly
- Missing `persistent_account_id` → only fallback fingerprint stored, persistent blind idx is NULL
- Both identifiers available → both fingerprints stored and checked
- Simultaneous linking race → DB constraint catches it
- Client sends tampered institution_id → server-side verification corrects it

## Verification

1. Write migration, run `./scripts/test-migrations.sh` (idempotency, rollback, schema tests)
2. Run `go test ./internal/services/... -v -run TestLinkAccounts`
3. Run `go test ./internal/repository/... -v -run TestAccountFingerprint`
4. Run `make test-fast` (full backend + frontend)
5. Manual test: Link same Plaid sandbox account from two different test users → second should get 409
