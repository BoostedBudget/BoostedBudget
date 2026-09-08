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
	// expiry.
	//
	// Ownership is enforced by the same statement's WHERE (`li.id = $1 AND li.user_id = $2`), so
	// a mint for someone else's item inserts nothing and returns ErrRepairNonceNotMinted rather
	// than creating an orphan nonce.
	//
	// On success the nonce's ID, CreatedAt and UpdatedAt are populated from RETURNING.
	Mint(ctx context.Context, nonce *domain.LinkedItemRepairNonce) error

	// Burn atomically spends the nonce whose sha256 digest is tokenHash, but ONLY if — in the
	// SAME statement — every one of the following holds:
	//
	//   - it was minted for this user and this linked item (userID, linkedItemID);
	//   - it has not been burned already (used_at IS NULL);
	//   - it has not expired (expires_at > now(), the Plaid link_token's own expiry).
	//
	// Returns (true, nil) when the row was burned. Returns (false, nil) — NOT an error — when the
	// UPDATE matched zero rows. False is a REFUSAL, and callers must treat it as one: it is the
	// authorization decision itself, not a missing-optimisation to swallow. Two concurrent repair
	// calls presenting the same raw nonce therefore cannot both succeed, whatever their
	// interleaving: the second one's `used_at IS NULL` is re-evaluated against the first one's
	// committed row version.
	//
	// Deliberately reports a single boolean rather than distinguishing "no such nonce" from
	// "already spent" from "expired": the caller turns every one of them into the
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
			linked_item_id, user_id, token_hash, expires_at
		)
		SELECT li.id, li.user_id, $3, $4
		FROM linked_items li
		WHERE li.id = $1
		  AND li.user_id = $2
		RETURNING id, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		nonce.LinkedItemID,
		nonce.UserID,
		nonce.TokenHash,
		nonce.ExpiresAt,
	).Scan(&nonce.ID, &nonce.CreatedAt, &nonce.UpdatedAt)
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
		UPDATE linked_item_repair_nonces
		SET used_at = now()
		WHERE token_hash = $1
		  AND user_id = $2
		  AND linked_item_id = $3
		  AND used_at IS NULL
		  AND expires_at > now()
	`

	tag, err := r.db.Exec(ctx, query, tokenHash, userID, linkedItemID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
