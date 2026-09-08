package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ErrRepairNonceNotMinted is returned by Mint when the INSERT ... SELECT matched no linked_items
// row — the item does not exist, or it is not owned by the given user. Deliberately the same
// non-disclosure shape the reconnect flow uses everywhere else: a caller cannot distinguish
// "doesn't exist" from "not yours".
var ErrRepairNonceNotMinted = errors.New("repair nonce could not be minted for this linked item")

// LinkedItemRepairNonceRepository is the data access layer for linked_item_repair_nonces
// (Issue #1620) — the single-use artifact that binds POST /api/v1/plaid/reconnect to a
// reconnect-link-token this server actually minted.
//
// Both methods are deliberately ONE statement each, in the shape
// LinkedItemRepository.RepairStatus/TransitionErrorStatus established on this branch: a narrow
// predicated write whose RowsAffected IS the decision. Neither reads state into Go and then
// decides — that is the TOCTOU defect class this branch has already shipped twice (commits
// 5d75eab0 → b668e408, and Issue #1609 P3-T2).
type LinkedItemRepairNonceRepository interface {
	// Mint inserts one nonce row for (linkedItemID, userID) with the given sha256 token hash and
	// expiry, and — in the SAME statement — snapshots the item's current last_failure_asserted_at
	// into the row (Issue #1623).
	//
	// The snapshot is taken server-side, by selecting from linked_items, precisely so no Go value
	// ever stands in for it: Burn compares this column against the item's live column, and both
	// must be the database's own clock.
	//
	// Ownership is enforced by the same statement's WHERE (`li.id = $1 AND li.user_id = $2`), so
	// a mint for someone else's item inserts nothing and returns ErrRepairNonceNotMinted rather
	// than creating an orphan nonce.
	//
	// On success the nonce's ID, LastFailureAssertedAt, CreatedAt and UpdatedAt are populated
	// from RETURNING.
	Mint(ctx context.Context, nonce *domain.LinkedItemRepairNonce) error

	// Burn atomically spends the nonce whose sha256 digest is tokenHash, but ONLY if — in the
	// SAME statement — every one of the following holds:
	//
	//   - it was minted for this user and this linked item (userID, linkedItemID);
	//   - it has not been burned already (used_at IS NULL);
	//   - it has not expired (expires_at > now(), the Plaid link_token's own expiry);
	//   - the item's live last_failure_asserted_at is still IS NOT DISTINCT FROM the value
	//     snapshotted at mint (Issue #1623). A failure asserted since this repair began means the
	//     repair is about an OLDER failure, and clearing on it would mark a currently-broken
	//     connection healthy. NULL matches NULL — an item with no assert on record — which is why
	//     this is IS NOT DISTINCT FROM and not `=`.
	//
	// ⚠ Known and accepted cost of that last predicate: a failure re-asserted DURING the user's
	// Link session (a sync worker discovering the same breakage while they re-authenticate)
	// refuses an otherwise-legitimate repair. The recovery is the same one an expired nonce
	// already has — re-open /accounts/reconnect, which mints a fresh nonce against the new
	// snapshot — and the alternative (accepting the repair) is the defect this predicate exists
	// to prevent. Broken items receive no scheduled sync in this codebase, so the window is
	// narrow, but it is not zero.
	//
	// Returns (true, nil) when the row was burned. Returns (false, nil) — NOT an error — when the
	// UPDATE matched zero rows. False is a REFUSAL, and callers must treat it as one: it is the
	// authorization decision itself, not a missing-optimisation to swallow. Two concurrent repair
	// calls presenting the same raw nonce therefore cannot both succeed, whatever their
	// interleaving: the second one's `used_at IS NULL` is re-evaluated against the first one's
	// committed row version.
	//
	// Deliberately reports a single boolean rather than distinguishing "no such nonce" from
	// "already spent" from "expired" from "a newer failure landed": the caller turns every one of them into the
	// same refusal and the same user-facing message ("start again from the Reconnect Bank
	// button"), and returning a reason a caller cannot act on differently would only invite
	// someone to act on it.
	Burn(ctx context.Context, tokenHash, userID, linkedItemID string) (bool, error)
}

type linkedItemRepairNonceRepository struct {
	db DB
}

// Compile-time proof the concrete type still satisfies the interface.
var _ LinkedItemRepairNonceRepository = (*linkedItemRepairNonceRepository)(nil)

// NewLinkedItemRepairNonceRepository creates a LinkedItemRepairNonceRepository.
//
// Takes no *appcrypto.FieldEncryptor: nothing in linked_item_repair_nonces is PII or a secret at
// rest. token_hash is already a one-way digest of a value that is never stored, and the remaining
// columns are two foreign keys and three timestamps.
func NewLinkedItemRepairNonceRepository(db DB) *linkedItemRepairNonceRepository { //nolint:revive // deliberate: mirrors NewLinkedItemRepository
	return &linkedItemRepairNonceRepository{db: db}
}

// Mint implements LinkedItemRepairNonceRepository.Mint — see that method's doc comment.
func (r *linkedItemRepairNonceRepository) Mint(ctx context.Context, nonce *domain.LinkedItemRepairNonce) error {
	const query = `
		INSERT INTO linked_item_repair_nonces (
			linked_item_id, user_id, token_hash, expires_at, last_failure_asserted_at
		)
		SELECT li.id, li.user_id, $3, $4, li.last_failure_asserted_at
		FROM linked_items li
		WHERE li.id = $1
		  AND li.user_id = $2
		RETURNING id, last_failure_asserted_at, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		nonce.LinkedItemID,
		nonce.UserID,
		nonce.TokenHash,
		nonce.ExpiresAt,
	).Scan(&nonce.ID, &nonce.LastFailureAssertedAt, &nonce.CreatedAt, &nonce.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRepairNonceNotMinted
		}
		return err
	}
	return nil
}

// Burn implements LinkedItemRepairNonceRepository.Burn — see that method's doc comment.
func (r *linkedItemRepairNonceRepository) Burn(ctx context.Context, tokenHash, userID, linkedItemID string) (bool, error) {
	const query = `
		UPDATE linked_item_repair_nonces n
		SET used_at = now()
		FROM linked_items li
		WHERE n.token_hash = $1
		  AND n.user_id = $2
		  AND n.linked_item_id = $3
		  AND n.used_at IS NULL
		  AND n.expires_at > now()
		  AND li.id = n.linked_item_id
		  AND li.last_failure_asserted_at IS NOT DISTINCT FROM n.last_failure_asserted_at
	`

	tag, err := r.db.Exec(ctx, query, tokenHash, userID, linkedItemID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
