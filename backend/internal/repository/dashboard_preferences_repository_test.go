package repository

import (
	"context"
	
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestDashboardPreferencesRepository_GetByUserID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer mock.Close()

	repo := NewDashboardPreferencesRepository(mock)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		userID := "user-123"
		layout := domain.DashboardLayout{
			Widgets: []domain.WidgetConfig{
				{ID: "w1", Type: domain.WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
			},
			Version: 1,
		}
		layoutJSON, _ := json.Marshal(layout)
		testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		rows := pgxmock.NewRows([]string{"id", "user_id", "layout", "version", "created_at", "updated_at"}).
			AddRow("pref-123", userID, layoutJSON, 1, testTime, testTime)

		mock.ExpectQuery("SELECT id, user_id, layout, version, created_at, updated_at").
			WithArgs(userID).
			WillReturnRows(rows)

		prefs, err := repo.GetByUserID(ctx, userID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if prefs == nil {
			t.Error("expected preferences, got nil")
		}
		if prefs.UserID != userID {
			t.Errorf("expected userID %s, got %s", userID, prefs.UserID)
		}

		// Verify layout is correctly parsed
		parsedLayout, err := prefs.GetLayout()
		if err != nil {
			t.Errorf("failed to parse layout: %v", err)
		}
		if len(parsedLayout.Widgets) != 1 {
			t.Errorf("expected 1 widget, got %d", len(parsedLayout.Widgets))
		}
	})

	t.Run("not found", func(t *testing.T) {
		userID := "nonexistent-user"

		mock.ExpectQuery("SELECT id, user_id, layout, version, created_at, updated_at").
			WithArgs(userID).
			WillReturnError(pgx.ErrNoRows)

		prefs, err := repo.GetByUserID(ctx, userID)
		if err != ErrDashboardPreferencesNotFound {
			t.Errorf("expected ErrDashboardPreferencesNotFound, got %v", err)
		}
		if prefs != nil {
			t.Error("expected nil preferences")
		}
	})
}

func TestDashboardPreferencesRepository_Upsert(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer mock.Close()

	repo := NewDashboardPreferencesRepository(mock)
	ctx := context.Background()

	t.Run("insert new preferences", func(t *testing.T) {
		layout := domain.DefaultDashboardLayout()
		layoutJSON, _ := json.Marshal(layout)
		testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		prefs := &domain.DashboardPreferences{
			UserID:  "user-123",
			Layout:  layoutJSON,
			Version: 1,
		}

		mock.ExpectQuery("INSERT INTO user_dashboard_preferences").
			WithArgs(pgxmock.AnyArg(), prefs.UserID, prefs.Layout, prefs.Version, pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(pgxmock.NewRows([]string{"id", "created_at"}).AddRow("new-id", testTime))

		err := repo.Upsert(ctx, prefs)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if prefs.ID == "" {
			t.Error("expected ID to be set")
		}
	})

	t.Run("insert with nil layout uses default", func(t *testing.T) {
		testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		prefs := &domain.DashboardPreferences{
			UserID: "user-456",
			Layout: nil, // Will use default
		}

		mock.ExpectQuery("INSERT INTO user_dashboard_preferences").
			WithArgs(pgxmock.AnyArg(), prefs.UserID, pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(pgxmock.NewRows([]string{"id", "created_at"}).AddRow("new-id", testTime))

		err := repo.Upsert(ctx, prefs)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if prefs.Layout == nil {
			t.Error("expected Layout to be set to default")
		}
	})
}

func TestDashboardPreferencesRepository_Delete(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer mock.Close()

	repo := NewDashboardPreferencesRepository(mock)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		userID := "user-123"

		mock.ExpectExec("DELETE FROM user_dashboard_preferences").
			WithArgs(userID).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := repo.Delete(ctx, userID)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		userID := "nonexistent-user"

		mock.ExpectExec("DELETE FROM user_dashboard_preferences").
			WithArgs(userID).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := repo.Delete(ctx, userID)
		if err != ErrDashboardPreferencesNotFound {
			t.Errorf("expected ErrDashboardPreferencesNotFound, got %v", err)
		}
	})
}
