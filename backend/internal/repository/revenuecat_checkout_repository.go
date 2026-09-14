package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type RevenueCatCheckoutRepository interface {
	Begin(context.Context, string) (domain.RevenueCatCheckoutIntent, error)
	Release(context.Context, string, string) error
}
type revenueCatCheckoutRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

func NewRevenueCatCheckoutRepository(db DB, enc *appcrypto.FieldEncryptor) RevenueCatCheckoutRepository {
	return &revenueCatCheckoutRepository{db: db, enc: enc}
}

// Begin serializes first-party checkout admission with refresh/grants for this
// customer. The public SDK remains callable outside this flow; this is an
// ordinary double-window guard, not a security boundary against direct purchases.
func (r *revenueCatCheckoutRepository) Begin(ctx context.Context, userID string) (domain.RevenueCatCheckoutIntent, error) {
	out := domain.RevenueCatCheckoutIntent{}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	session, err := lockRevenueCatCustomer(ctx, tx, userID)
	if err != nil {
		return out, err
	}
	session.enc = r.enc
	state, err := session.Read(ctx)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC()
	if err := state.CanStartCheckout(now); err != nil {
		return out, err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT id FROM revenuecat_checkout_intents WHERE user_id=$1 AND closed_at IS NULL AND expires_at > $2 LIMIT 1`, userID, now).Scan(&existing)
	if err == nil {
		return out, domain.ErrRevenueCatCheckoutInProgress
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	out = domain.RevenueCatCheckoutIntent{ID: uuid.NewString(), ExpiresAt: now.Add(30 * time.Minute), Offering: "web"}
	if _, err := tx.Exec(ctx, `INSERT INTO revenuecat_checkout_intents(id,user_id,expires_at) VALUES($1,$2,$3)`, out.ID, userID, out.ExpiresAt); err != nil {
		return domain.RevenueCatCheckoutIntent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RevenueCatCheckoutIntent{}, err
	}
	return out, nil
}

// Release marks an intent closed without deleting its operational history.
// Closing an intent never changes subscription access or confirms payment.
func (r *revenueCatCheckoutRepository) Release(ctx context.Context, userID, id string) error {
	_, err := r.db.Exec(ctx, `UPDATE revenuecat_checkout_intents SET closed_at=clock_timestamp() WHERE id=$1 AND user_id=$2 AND closed_at IS NULL`, id, userID)
	return err
}
