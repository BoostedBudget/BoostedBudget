package services

import (
	"context"
	"errors"
	"sort"
	"strings"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var ErrRevenueCatMultipleSubscriptions = errors.New("multiple paid subscriptions require operator review")

type revenueCatPaddleSubscriptions interface {
	GetSubscription(context.Context, string) (*paddle.Subscription, error)
}
type RevenueCatPaddleTransactions interface {
	GetTransaction(context.Context, *paddle.GetTransactionRequest) (*paddle.Transaction, error)
}
type RevenueCatBillingService struct {
	summary       RevenueCatSummaryReader
	subscriptions revenueCatPaddleSubscriptions
	transactions  RevenueCatPaddleTransactions
}

func NewRevenueCatBillingService(summary RevenueCatSummaryReader, subscriptions revenueCatPaddleSubscriptions, transactions RevenueCatPaddleTransactions) *RevenueCatBillingService {
	return &RevenueCatBillingService{summary: summary, subscriptions: subscriptions, transactions: transactions}
}

// Resolve follows only server-observed RevenueCat references. Product changes can
// produce multiple RevenueCat records for one Paddle subscription; conversely,
// genuinely distinct subscriptions must never be silently selected or cancelled.
func (s *RevenueCatBillingService) Resolve(ctx context.Context, user *domain.User) (*paddle.Subscription, RevenueCatSummary, error) {
	var summary RevenueCatSummary
	if user == nil || user.IsDeleted || s.summary == nil || s.subscriptions == nil {
		return nil, summary, domain.ErrRevenueCatUnresolved
	}
	summary = s.summary.Summary(ctx, user)
	if !summary.BillingAvailable {
		return nil, summary, domain.ErrRevenueCatUnresolved
	}
	ids := map[string]bool{}
	for _, b := range summary.Billing {
		if b.Store == "promotional" || b.Status == "expired" {
			continue
		}
		if b.Store != "paddle" {
			return nil, summary, domain.ErrRevenueCatUnresolved
		}
		id := b.StoreSubscriptionIdentifier
		id, err := s.subscriptionID(ctx, id)
		if err != nil {
			return nil, summary, err
		}
		ids[id] = true
	}
	if len(ids) == 0 {
		return nil, summary, ErrSubscriptionNotFound
	}
	if len(ids) > 1 {
		return nil, summary, ErrRevenueCatMultipleSubscriptions
	}
	for id := range ids {
		sub, err := s.subscriptions.GetSubscription(ctx, id)
		if err != nil || sub == nil || sub.ID != id {
			return nil, summary, domain.ErrRevenueCatUnresolved
		}
		return sub, summary, nil
	}
	return nil, summary, ErrSubscriptionNotFound
}

// ManagementURL returns the RevenueCat-resolved self-service management URL for the
// customer's single paid subscription. RevenueCat populates this for Paddle
// subscriptions when the Paddle API key carries the "Customer portal sessions (Write)"
// permission; an empty result is an unresolved authority, never a fabricated link.
func (s *RevenueCatBillingService) ManagementURL(ctx context.Context, user *domain.User) (string, error) {
	_, summary, err := s.Resolve(ctx, user)
	if err != nil {
		return "", err
	}
	for _, b := range summary.Billing {
		if b.Store == "paddle" && b.Status != "expired" && b.ManagementURL != "" {
			return b.ManagementURL, nil
		}
	}
	return "", domain.ErrRevenueCatUnresolved
}

func (s *RevenueCatBillingService) subscriptionID(ctx context.Context, id string) (string, error) {
	if strings.HasPrefix(id, "txn_") {
		if s.transactions == nil {
			return "", domain.ErrRevenueCatUnresolved
		}
		tx, err := s.transactions.GetTransaction(ctx, &paddle.GetTransactionRequest{TransactionID: id})
		if err != nil || tx == nil || tx.SubscriptionID == nil {
			return "", domain.ErrRevenueCatUnresolved
		}
		id = *tx.SubscriptionID
	}
	if !strings.HasPrefix(id, "sub_") {
		return "", domain.ErrRevenueCatUnresolved
	}
	return id, nil
}

// GDPR must inspect every real subscription, including duplicate purchases and
// records RevenueCat calls expired. Only Paddle can establish its billing state.
func (s *RevenueCatBillingService) ResolveAllForErasure(ctx context.Context, billing []domain.RevenueCatBilling) ([]*paddle.Subscription, error) {
	ids := map[string]bool{}
	for _, b := range billing {
		if b.Store == "promotional" {
			continue
		}
		if b.Store != "paddle" {
			return nil, domain.ErrRevenueCatUnresolved
		}
		id, err := s.subscriptionID(ctx, b.StoreSubscriptionIdentifier)
		if err != nil {
			return nil, err
		}
		ids[id] = true
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	out := make([]*paddle.Subscription, 0, len(ids))
	for _, id := range ordered {
		if s.subscriptions == nil {
			return nil, domain.ErrRevenueCatUnresolved
		}
		sub, err := s.subscriptions.GetSubscription(ctx, id)
		if err != nil || sub == nil || sub.ID != id || !strings.HasPrefix(sub.CustomerID, "ctm_") {
			return nil, domain.ErrRevenueCatUnresolved
		}
		out = append(out, sub)
	}
	return out, nil
}
