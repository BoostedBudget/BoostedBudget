package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrDashboardPreferencesNotFound is returned when dashboard preferences are not found
	ErrDashboardPreferencesNotFound = errors.New("dashboard preferences not found")
)

// DashboardPreferencesRepository defines the interface for dashboard preferences data access
type DashboardPreferencesRepository interface {
	// GetByUserID retrieves dashboard preferences for a specific user
	// Returns ErrDashboardPreferencesNotFound if no preferences exist
	GetByUserID(ctx context.Context, userID string) (*domain.DashboardPreferences, error)

	// Upsert creates or updates dashboard preferences for a user
	// Uses PostgreSQL UPSERT (ON CONFLICT UPDATE) for atomic operation
	Upsert(ctx context.Context, prefs *domain.DashboardPreferences) error

	// Delete removes dashboard preferences for a user
	Delete(ctx context.Context, userID string) error
}

// dashboardPreferencesRepository implements DashboardPreferencesRepository using PostgreSQL
type dashboardPreferencesRepository struct {
	db DB
}

// NewDashboardPreferencesRepository creates a new DashboardPreferencesRepository instance
func NewDashboardPreferencesRepository(db DB) DashboardPreferencesRepository {
	return &dashboardPreferencesRepository{db: db}
}

// GetByUserID retrieves dashboard preferences for a specific user
func (r *dashboardPreferencesRepository) GetByUserID(ctx context.Context, userID string) (*domain.DashboardPreferences, error) {
	query := `
		SELECT id, user_id, layout, version, created_at, updated_at
		FROM user_dashboard_preferences
		WHERE user_id = $1
	`

	var prefs domain.DashboardPreferences
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&prefs.ID,
		&prefs.UserID,
		&prefs.Layout,
		&prefs.Version,
		&prefs.CreatedAt,
		&prefs.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDashboardPreferencesNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get dashboard preferences: %w", err)
	}

	return &prefs, nil
}

// Upsert creates or updates dashboard preferences for a user
// Uses PostgreSQL UPSERT with ON CONFLICT to handle both insert and update atomically
func (r *dashboardPreferencesRepository) Upsert(ctx context.Context, prefs *domain.DashboardPreferences) error {
	// Generate UUID if not set
	if prefs.ID == "" {
		prefs.ID = uuid.New().String()
	}

	now := time.Now()
	prefs.UpdatedAt = now

	// Ensure layout is valid JSON
	if prefs.Layout == nil {
		defaultLayout := domain.DefaultDashboardLayout()
		layoutJSON, err := json.Marshal(defaultLayout)
		if err != nil {
			return fmt.Errorf("failed to marshal default layout: %w", err)
		}
		prefs.Layout = layoutJSON
		prefs.Version = defaultLayout.Version
	}

	query := `
		INSERT INTO user_dashboard_preferences (id, user_id, layout, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id) DO UPDATE SET
			layout = EXCLUDED.layout,
			version = EXCLUDED.version,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at
	`

	err := r.db.QueryRow(ctx, query,
		prefs.ID,
		prefs.UserID,
		prefs.Layout,
		prefs.Version,
		now, // created_at for new records
		now, // updated_at
	).Scan(&prefs.ID, &prefs.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to upsert dashboard preferences: %w", err)
	}

	return nil
}

// Delete removes dashboard preferences for a user
func (r *dashboardPreferencesRepository) Delete(ctx context.Context, userID string) error {
	query := `DELETE FROM user_dashboard_preferences WHERE user_id = $1`

	ct, err := r.db.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete dashboard preferences: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return ErrDashboardPreferencesNotFound
	}

	return nil
}
