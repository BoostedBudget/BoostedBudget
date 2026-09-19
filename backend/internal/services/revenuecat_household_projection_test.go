package services

import (
	"context"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestHouseholdRevenueCatTransactionProjectionIgnoresLegacyOverride(t *testing.T) {
	for _, tc := range []struct {
		name, grants string
		age          time.Duration
		pending      int
		want         string
		failure      bool
	}{
		{name: "confirmed comp", grants: `[{"key":"comp_household_3"}]`, want: "household_3"},
		{name: "confirmed lapse", grants: `[]`, want: "premium"},
		{name: "stale", grants: `[]`, age: time.Hour, failure: true},
		{name: "pending", grants: `[]`, pending: 1, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer db.Close()
			updated := time.Now().Add(-tc.age)
			db.ExpectQuery("SELECT grants, access_updated_at").WithArgs("owner").WillReturnRows(pgxmock.NewRows([]string{"grants", "access_updated_at", "pending"}).AddRow([]byte(tc.grants), &updated, tc.pending))
			override := "household_5"
			s := &householdService{entitlementSvc: &HouseholdEntitlementService{ownAccessResolver: &cleanupAccess{}}}
			access, err := s.ownAccessInTransaction(context.Background(), db, &domain.User{ID: "owner", SubscriptionTier: "household_5", SubscriptionStatus: "active", ManualTierOverride: &override})
			if tc.failure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, access.Tier)
			}
			require.NoError(t, db.ExpectationsWereMet())
		})
	}
}

func TestHouseholdRevenueCatBillingAdmissionIsIndependentOfAccess(t *testing.T) {
	key := "12345678901234567890123456789012"
	cipher, err := NewEncryptionService(&config.Config{EncryptionKey: key})
	require.NoError(t, err)
	enc, err := appcrypto.NewFieldEncryptor(cipher, []byte(key))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, billing    string
		age              time.Duration
		paid, unresolved bool
	}{
		{name: "comp no paid relationship", billing: `[]`},
		{name: "active promotional trial or comp is not paid", billing: `[{"status":"active","store":"promotional"}]`},
		{name: "promotional plus actual paid subscription", billing: `[{"status":"active","store":"promotional"},{"status":"active","store":"paddle"}]`, paid: true},
		{name: "promotional does not conceal unknown nonpromotional status", billing: `[{"status":"active","store":"promotional"},{"status":"new_status","store":"paddle"}]`, unresolved: true},
		{name: "paid plus comp", billing: `[{"status":"active","store":"paddle"}]`, paid: true},
		{name: "expired paid plus comp", billing: `[{"status":"expired","store":"paddle"}]`},
		{name: "stale billing", billing: `[]`, age: time.Hour, unresolved: true},
		{name: "unknown billing state", billing: `[{"status":"new_status","store":"paddle"}]`, unresolved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, e := pgxmock.NewPool()
			require.NoError(t, e)
			defer db.Close()
			encrypted, e := enc.EncryptString(tc.billing)
			require.NoError(t, e)
			updated := time.Now().Add(-tc.age)
			db.ExpectQuery("SELECT billing, billing_updated_at, billing_error").WithArgs("caller").WillReturnRows(pgxmock.NewRows([]string{"billing", "billing_updated_at", "billing_error"}).AddRow(encrypted, &updated, ""))
			s := &householdService{enc: enc, entitlementSvc: &HouseholdEntitlementService{ownAccessResolver: &cleanupAccess{}}}
			got, e := s.callerHasLiveBillingInTransaction(context.Background(), db, &domain.User{ID: "caller", SubscriptionStatus: "active"})
			require.Equal(t, tc.paid, got)
			require.Equal(t, tc.unresolved, e != nil)
			require.NoError(t, db.ExpectationsWereMet())
		})
	}
}
