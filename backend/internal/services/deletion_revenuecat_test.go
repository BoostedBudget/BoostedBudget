package services

import (
	"context"
	"errors"
	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"testing"
)

type deletionRCFake struct {
	subs   []*paddle.Subscription
	err    error
	erased bool
}

func (f *deletionRCFake) Prepare(ctx context.Context, _ string, _ bool, gate func(context.Context) (bool, error)) error {
	return revenueCatErasureGate(ctx, gate)
}
func (f *deletionRCFake) Subscriptions(context.Context, string) ([]*paddle.Subscription, error) {
	return f.subs, nil
}
func (f *deletionRCFake) Erase(ctx context.Context, _ string, gate func(context.Context) (bool, error)) error {
	if e := revenueCatErasureGate(ctx, gate); e != nil {
		return e
	}
	f.erased = true
	return f.err
}

func TestExecuteDeletionRevenueCatFailureStopsLocalCascadeAndRecordsResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)
	h.user.PaddleSubscriptionID = nil
	h.user.PaddleCustomerID = nil
	rc := &deletionRCFake{err: errors.New("remote timeout")}
	h.svc.SetRevenueCatCleanupService(rc)
	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))
	require.True(t, rc.erased)
	h.repo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything, mock.Anything)
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID, repository.DeletionResidueRevenueCatCustomerErased)
}

func TestExecuteDeletionRevenueCatCancelsEveryActualPaidSubscriptionBeforeArchive(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarnessWithStatus(t, ctx, "inactive")
	h.user.PaddleSubscriptionID = nil
	h.user.PaddleCustomerID = nil
	rc := &deletionRCFake{subs: []*paddle.Subscription{
		{ID: "sub_one", CustomerID: "ctm_real", Status: paddle.SubscriptionStatusActive},
		{ID: "sub_two", CustomerID: "ctm_real", Status: paddle.SubscriptionStatusPaused},
	}}
	h.svc.SetRevenueCatCleanupService(rc)
	h.paddle.ExpectedCalls = nil
	for _, id := range []string{"sub_one", "sub_two"} {
		h.paddle.On("CancelSubscriptionImmediately", ctx, id).Return(nil).Once().Run(func(a mock.Arguments) { *h.callOrder = append(*h.callOrder, a.String(1)) })
	}
	h.paddle.On("ArchiveAndAnonymizeCustomer", ctx, "ctm_real", expectedAnonymizedEmail).Return(nil).Once().Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "archive") })
	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))
	require.Equal(t, []string{"PlaidRemoveAll", "sub_one", "sub_two", "archive", "DeleteUserCascade"}, *h.callOrder)
	require.True(t, rc.erased)
	h.paddle.AssertExpectations(t)
}
