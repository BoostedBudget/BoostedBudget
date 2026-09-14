package repository

import (
	"context"
	"time"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type RevenueCatErasureRepository struct{ repo *revenueCatRepository }

func NewRevenueCatErasureRepository(db DB, enc *appcrypto.FieldEncryptor) *RevenueCatErasureRepository {
	return &RevenueCatErasureRepository{repo: &revenueCatRepository{db: db, enc: enc}}
}

// Begin commits before any irreversible provider call. Grant writers and
// checkout admission read this fence under the same customer row lock.
func (r *RevenueCatErasureRepository) Begin(ctx context.Context, id string, retention bool) error {
	return r.repo.WithCustomer(ctx, id, func(session RevenueCatSession) error {
		s := session.(*revenueCatSession)
		state, err := s.Read(ctx)
		if err != nil {
			return err
		}
		if retention && !state.SafeForCleanup(time.Now()) {
			return domain.ErrRevenueCatUnresolved
		}
		if _, err = s.q.Exec(ctx, `UPDATE revenuecat_customers SET erasure_started_at=COALESCE(erasure_started_at,clock_timestamp()) WHERE user_id=$1`, id); err != nil {
			return err
		}
		_, err = s.q.Exec(ctx, `UPDATE revenuecat_operations SET stage='superseded',last_error='erasure_started' WHERE user_id=$1 AND stage='pending'`, id)
		return err
	})
}

func (r *RevenueCatErasureRepository) Confirm(ctx context.Context, id string) error {
	_, err := r.repo.db.Exec(ctx, `UPDATE revenuecat_customers SET erasure_confirmed_at=clock_timestamp() WHERE user_id=$1 AND erasure_started_at IS NOT NULL`, id)
	return err
}
