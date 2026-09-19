package services

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type transactionalTrialProbe struct{ calls, legacyCalls int }

func (p *transactionalTrialProbe) GrantTrial(context.Context, string) error {
	p.legacyCalls++
	return errors.New("nontransactional trial issuance is forbidden")
}

func (p *transactionalTrialProbe) GrantTrialTx(ctx context.Context, tx pgx.Tx, userID string) error {
	p.calls++
	_, err := tx.Exec(ctx, "SELECT record_trial_intent($1)", userID)
	return err
}

func TestRegisterUserRevenueCatTrialIntentIsAtomic(t *testing.T) {
	for _, failIntent := range []bool{false, true} {
		name := "commit"
		if failIntent {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			pool, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer pool.Close()
			profiles := new(MockProfileRepository)
			identities := new(MockUserIdentityRepository)
			privacy := new(MockPrivacyRepository)
			identities.On("GetByClerkID", mock.Anything, "user_clerk").Return(nil, repository.ErrIdentityNotFound)
			profiles.On("GetUserByEmail", mock.Anything, "trial@example.com").Return(nil, repository.ErrUserNotFound)
			profiles.On("CreateUserTx", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				user := args.Get(2).(*domain.User)
				require.Equal(t, "inactive", user.SubscriptionStatus, "recording trial intent does not grant access")
			}).Return(nil)
			identities.On("CreateTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			privacy.On("CreateTx", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			pool.ExpectBegin()
			intent := pool.ExpectExec("SELECT record_trial_intent").WithArgs(pgxmock.AnyArg())
			if failIntent {
				intent.WillReturnError(errors.New("intent unavailable"))
				pool.ExpectRollback()
			} else {
				intent.WillReturnResult(pgxmock.NewResult("SELECT", 1))
				pool.ExpectCommit()
				pool.ExpectRollback()
			}
			granter := &transactionalTrialProbe{}
			svc := NewRegistrationService(profiles, identities, privacy, nil, pool)
			svc.SetTrialGranter(granter)
			user, err := svc.RegisterUser(context.Background(), "user_clerk", "trial@example.com", true)
			if failIntent {
				require.ErrorContains(t, err, "intent unavailable")
				require.Nil(t, user)
			} else {
				require.NoError(t, err)
				require.NotNil(t, user)
				require.True(t, user.HadTrial)
				require.Equal(t, "inactive", user.SubscriptionStatus)
			}
			require.Equal(t, 1, granter.calls)
			require.Zero(t, granter.legacyCalls)
			require.NoError(t, pool.ExpectationsWereMet())
			profiles.AssertExpectations(t)
			identities.AssertExpectations(t)
			privacy.AssertExpectations(t)
		})
	}
}

func TestRegisterUserRevenueCatRequiresTransactionOnlyForNewAccount(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new_account"
		if existing {
			name = "existing_account"
		}
		t.Run(name, func(t *testing.T) {
			profiles := new(MockProfileRepository)
			identities := new(MockUserIdentityRepository)
			privacy := new(MockPrivacyRepository)
			if existing {
				identities.On("GetByClerkID", mock.Anything, "user_clerk").Return(&domain.UserIdentity{UserID: "existing"}, nil)
				profiles.On("GetUserByID", mock.Anything, "existing").Return(&domain.User{ID: "existing", HadTrial: true}, nil)
			} else {
				identities.On("GetByClerkID", mock.Anything, "user_clerk").Return(nil, repository.ErrIdentityNotFound)
				profiles.On("GetUserByEmail", mock.Anything, "trial@example.com").Return(nil, repository.ErrUserNotFound)
			}
			granter := &transactionalTrialProbe{}
			svc := NewRegistrationService(profiles, identities, privacy, nil, nil)
			svc.SetTrialGranter(granter)
			user, err := svc.RegisterUser(context.Background(), "user_clerk", "trial@example.com", true)
			if existing {
				require.NoError(t, err)
				require.Equal(t, "existing", user.ID)
			} else {
				require.ErrorContains(t, err, "requires a database")
				require.Nil(t, user)
			}
			require.Zero(t, granter.calls)
			require.Zero(t, granter.legacyCalls)
		})
	}
}
