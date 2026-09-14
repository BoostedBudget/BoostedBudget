package repository

import (
	"context"
	"errors"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"time"
)

type RevenueCatBootstrapRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

func NewRevenueCatBootstrapRepository(db DB, enc *appcrypto.FieldEncryptor) *RevenueCatBootstrapRepository {
	return &RevenueCatBootstrapRepository{db: db, enc: enc}
}

const revenueCatBootstrapColumns = `id,clerk_user_id,subscription_status,had_trial,trial_ends_at,manual_tier_override,manual_tier_ends_at,paddle_subscription_id,paddle_customer_id,is_deleted,deletion_requested_at`

func (r *RevenueCatBootstrapRepository) scanUser(row interface{ Scan(...any) error }) (*domain.User, error) {
	user := &domain.User{}
	err := row.Scan(&user.ID, &user.ClerkUserID, &user.SubscriptionStatus, &user.HadTrial, &user.TrialEndsAt, &user.ManualTierOverride, &user.ManualTierEndsAt, &user.PaddleSubscriptionID, &user.PaddleCustomerID, &user.IsDeleted, &user.DeletionRequestedAt)
	if err != nil {
		return nil, err
	}
	if r.enc == nil {
		return nil, errors.New("bootstrap requires field encryption configuration")
	}
	if user.ClerkUserID, err = r.enc.DecryptString(user.ClerkUserID); err != nil {
		return nil, errors.New("bootstrap identity decryption failed")
	}
	if user.PaddleSubscriptionID, err = r.enc.DecryptNullableString(user.PaddleSubscriptionID); err != nil {
		return nil, errors.New("bootstrap subscription reference decryption failed")
	}
	if user.PaddleCustomerID, err = r.enc.DecryptNullableString(user.PaddleCustomerID); err != nil {
		return nil, errors.New("bootstrap customer reference decryption failed")
	}
	return user, nil
}

// Inventory is read-only. The caller validates every candidate before applying any.
func (r *RevenueCatBootstrapRepository) Inventory(ctx context.Context) ([]*domain.User, error) {
	rows, err := r.db.Query(ctx, `SELECT `+revenueCatBootstrapColumns+` FROM users WHERE is_deleted=false ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.User{}
	for rows.Next() {
		user, e := r.scanUser(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

// Apply rechecks legacy facts under customer -> user locks. Any existing modern
// operation owns this customer's lifecycle; bootstrap never supersedes it.
func (r *RevenueCatBootstrapRepository) Apply(ctx context.Context, id string) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = lockRevenueCatCustomer(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	user, err := r.scanUser(tx.QueryRow(ctx, `SELECT `+revenueCatBootstrapColumns+` FROM users WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return 0, err
	}
	var managed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM revenuecat_operations WHERE user_id=$1)`, id).Scan(&managed); err != nil {
		return 0, err
	}
	if managed {
		return 0, tx.Commit(ctx)
	}
	if user.ClerkUserID == "" {
		return 0, errors.New("bootstrap customer has no Clerk identity")
	}
	ops, err := domain.PlanRevenueCatBootstrap(user, time.Now())
	if err != nil {
		return 0, err
	}
	for i := range ops {
		if err = insertRevenueCatOperation(ctx, tx, &ops[i]); err != nil {
			return 0, err
		}
	}
	if user.TrialEndsAt != nil {
		if _, err = tx.Exec(ctx, `UPDATE users SET had_trial=true WHERE id=$1`, id); err != nil {
			return 0, err
		}
	}
	return len(ops), tx.Commit(ctx)
}
