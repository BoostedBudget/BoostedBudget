package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

const analyticsAuthorityQuery = `
	SELECT u.clerk_user_id
	FROM users u
	JOIN privacy_settings ps ON ps.user_id = u.id
	WHERE u.id = $1
	  AND u.is_active = TRUE
	  AND u.is_deleted = FALSE
	  AND u.deletion_requested_at IS NULL
	  AND ps.gdpr_analytics_consent = TRUE
	FOR SHARE OF u, ps
`

// AnalyticsAuthorityRepository resolves consent and canonical identity atomically. The SHARE locks
// are held through emit, so a concurrent privacy UPDATE or user deletion cannot commit between the
// authority decision and the SDK enqueue boundary.
type AnalyticsAuthorityRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

func NewAnalyticsAuthorityRepository(db DB, enc *appcrypto.FieldEncryptor) *AnalyticsAuthorityRepository {
	return &AnalyticsAuthorityRepository{db: db, enc: enc}
}

func (r *AnalyticsAuthorityRepository) WithConsentedSubject(
	ctx context.Context,
	internalUserID string,
	emit func(clerkSubject string),
) (bool, error) {
	if r == nil || r.db == nil || r.enc == nil || emit == nil || strings.TrimSpace(internalUserID) == "" {
		return false, nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin analytics authority read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var encryptedSubject string
	if err := tx.QueryRow(ctx, analyticsAuthorityQuery, internalUserID).Scan(&encryptedSubject); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("read analytics authority: %w", err)
	}
	subject, err := r.enc.DecryptString(encryptedSubject)
	if err != nil || strings.TrimSpace(subject) == "" {
		if err != nil {
			return false, fmt.Errorf("decrypt analytics subject: %w", err)
		}
		return false, nil
	}

	emit(subject)
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit analytics authority read: %w", err)
	}
	return true, nil
}
