package services

import (
	"context"
	"errors"
	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"time"
)

type revenueCatErasureIntentRepository interface {
	Begin(context.Context, string, bool) error
	Confirm(context.Context, string) error
}
type RevenueCatDeletionCleaner interface {
	Prepare(context.Context, string, bool, func(context.Context) (bool, error)) error
	Subscriptions(context.Context, string) ([]*paddle.Subscription, error)
	Erase(context.Context, string, func(context.Context) (bool, error)) error
}
type RevenueCatErasureService struct {
	authority *RevenueCatService
	intents   revenueCatErasureIntentRepository
	billing   *RevenueCatBillingService
}

func NewRevenueCatErasureService(authority *RevenueCatService, intents revenueCatErasureIntentRepository, billing *RevenueCatBillingService) *RevenueCatErasureService {
	return &RevenueCatErasureService{authority: authority, intents: intents, billing: billing}
}
func revenueCatErasureGate(ctx context.Context, gate func(context.Context) (bool, error)) error {
	allowed, err := gate(ctx)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrDeletionAuthorisationRevoked
	}
	return nil
}
func (s *RevenueCatErasureService) Prepare(ctx context.Context, id string, retention bool, gate func(context.Context) (bool, error)) error {
	if s.authority == nil || s.intents == nil {
		return domain.ErrRevenueCatUnresolved
	}
	if err := revenueCatErasureGate(ctx, gate); err != nil {
		return err
	}
	state, err := s.authority.refresh(ctx, id, true, time.Now(), true)
	if err != nil {
		return err
	}
	if state.ErasureStartedAt == nil && (state.BillingUpdatedAt == nil || state.BillingError != "") {
		return domain.ErrRevenueCatUnresolved
	}
	if err := revenueCatErasureGate(ctx, gate); err != nil {
		return err
	}
	return s.intents.Begin(ctx, id, retention)
}
func (s *RevenueCatErasureService) Subscriptions(ctx context.Context, id string) ([]*paddle.Subscription, error) {
	state, err := s.authority.repo.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	if state.ErasureStartedAt == nil || state.BillingUpdatedAt == nil || state.BillingError != "" {
		return nil, domain.ErrRevenueCatUnresolved
	}
	user, err := s.authority.profiles.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil || user.IsDeleted {
		return nil, domain.ErrRevenueCatUnresolved
	}
	billing := append([]domain.RevenueCatBilling(nil), state.Billing...)
	// A pre-cutover subscription may not have imported yet. Its stored identity
	// remains an erasure obligation, but only a Paddle GET decides its real status.
	if user.PaddleSubscriptionID != nil {
		billing = append(billing, domain.RevenueCatBilling{Store: "paddle", StoreSubscriptionIdentifier: *user.PaddleSubscriptionID})
	}
	if s.billing == nil {
		for _, b := range billing {
			if b.Store != "promotional" {
				return nil, domain.ErrRevenueCatUnresolved
			}
		}
		return nil, nil
	}
	subs, err := s.billing.ResolveAllForErasure(ctx, billing)
	if err != nil {
		return nil, err
	}
	if user.PaddleSubscriptionID != nil && user.PaddleCustomerID != nil {
		legacyID, e := s.billing.subscriptionID(ctx, *user.PaddleSubscriptionID)
		if e != nil {
			return nil, e
		}
		for _, sub := range subs {
			if sub.ID == legacyID && sub.CustomerID != *user.PaddleCustomerID {
				return nil, domain.ErrRevenueCatUnresolved
			}
		}
	}
	return subs, nil
}
func (s *RevenueCatErasureService) Erase(ctx context.Context, id string, gate func(context.Context) (bool, error)) error {
	customer, _, err := s.authority.customerID(ctx, id)
	if err != nil {
		return err
	}
	provider, ok := s.authority.provider.(interface {
		DeleteCustomer(context.Context, string) error
		CustomerExists(context.Context, string) (bool, error)
	})
	if !ok {
		return domain.ErrRevenueCatUnresolved
	}
	if err := revenueCatErasureGate(ctx, gate); err != nil {
		return err
	}
	// The durable fence already exists; this lock also excludes readback refresh
	// during deletion. Authorization callbacks stay outside it to avoid reentrant
	// retention reads deadlocking on this same customer.
	err = s.authority.repo.WithCustomer(ctx, id, func(session repository.RevenueCatSession) error {
		state, e := session.Read(ctx)
		if e != nil {
			return e
		}
		if state.ErasureStartedAt == nil {
			return domain.ErrRevenueCatErasureStarted
		}
		e = provider.DeleteCustomer(ctx, customer)
		if revenueCatCustomerMissing(e) {
			return nil
		}
		if e != nil {
			return e
		}
		// DELETE 200 only queues asynchronous erasure. Keep local identity, billing
		// obligations and the fence until a GET proves the customer is absent.
		exists, e := provider.CustomerExists(ctx, customer)
		if e != nil {
			return e
		}
		if exists {
			return errors.New("RevenueCat erasure is queued; customer still exists")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.intents.Confirm(ctx, id)
}

func revenueCatCustomerMissing(err error) bool {
	var remote *rc.HTTPError
	return errors.As(err, &remote) && remote.StatusCode == 404
}
