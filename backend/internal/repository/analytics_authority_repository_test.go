package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyticsAuthorityHoldsConsentAndIdentityLockThroughEmission(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewAnalyticsAuthorityRepository(mock, newTestFieldEncryptor(t))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(analyticsAuthorityQuery)).WithArgs("internal-user").
		WillReturnRows(pgxmock.NewRows([]string{"clerk_user_id"}).AddRow("user_clerk"))
	mock.ExpectRollback()

	called := false
	granted, err := repo.WithConsentedSubject(context.Background(), "internal-user", func(subject string) {
		called = true
		assert.Equal(t, "user_clerk", subject)
	})

	require.NoError(t, err)
	assert.True(t, granted)
	assert.True(t, called)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAnalyticsAuthorityCleanupFailureDoesNotMisreportAnEmittedEvent(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewAnalyticsAuthorityRepository(mock, newTestFieldEncryptor(t))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(analyticsAuthorityQuery)).WithArgs("internal-user").
		WillReturnRows(pgxmock.NewRows([]string{"clerk_user_id"}).AddRow("user_clerk"))
	mock.ExpectRollback().WillReturnError(errors.New("connection already closed"))

	called := false
	granted, err := repo.WithConsentedSubject(context.Background(), "internal-user", func(string) { called = true })

	require.NoError(t, err)
	assert.True(t, granted)
	assert.True(t, called)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAnalyticsAuthorityFailsClosedWithoutEligibleRow(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewAnalyticsAuthorityRepository(mock, newTestFieldEncryptor(t))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(analyticsAuthorityQuery)).WithArgs("internal-user").
		WillReturnRows(pgxmock.NewRows([]string{"clerk_user_id"}))
	mock.ExpectRollback()

	granted, err := repo.WithConsentedSubject(context.Background(), "internal-user", func(string) {
		t.Fatal("must not emit")
	})

	require.NoError(t, err)
	assert.False(t, granted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAnalyticsAuthorityFailsClosedOnLookupError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	repo := NewAnalyticsAuthorityRepository(mock, newTestFieldEncryptor(t))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(analyticsAuthorityQuery)).WithArgs("internal-user").
		WillReturnError(errors.New("database unavailable"))
	mock.ExpectRollback()

	granted, err := repo.WithConsentedSubject(context.Background(), "internal-user", func(string) {
		t.Fatal("must not emit")
	})

	require.Error(t, err)
	assert.False(t, granted)
	require.NoError(t, mock.ExpectationsWereMet())
}
