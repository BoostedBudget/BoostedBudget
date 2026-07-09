package errors_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggingMapError_Nil(t *testing.T) {
	result := humaerrors.LoggingMapError(context.Background(), nil)
	assert.Nil(t, result)
}

func TestLoggingMapError_404(t *testing.T) {
	result := humaerrors.LoggingMapError(context.Background(), domain.ErrBudgetNotFound)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusNotFound, statusErr.GetStatus())
}

func TestLoggingMapError_500(t *testing.T) {
	unknownErr := errors.New("unexpected internal failure")
	result := humaerrors.LoggingMapError(context.Background(), unknownErr)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusInternalServerError, statusErr.GetStatus())
}

// Issue #1270: GAP-L — rule validation errors must return 400, not 500.
func TestLoggingMapError_400_InvalidRule(t *testing.T) {
	wrapped := fmt.Errorf("%w: condition field 'budget_category.bad_field' is not allowed for event 'budget_period_end'", domain.ErrInvalidBoost)
	result := humaerrors.LoggingMapError(context.Background(), wrapped)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}

// Issue #1270: GAP-L — foreign category_id in condition must return 403, not 500.
func TestLoggingMapError_403_RuleConditionForbidden(t *testing.T) {
	wrapped := fmt.Errorf("%w: condition category_id does not belong to this user", domain.ErrBoostConditionForbidden)
	result := humaerrors.LoggingMapError(context.Background(), wrapped)
	require.NotNil(t, result)

	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusForbidden, statusErr.GetStatus())
}

// Issue #1367 (Codex review, Medium finding 5): the paying-invitee 409 surfaces the
// caller's eligibility date when known, and falls back to the generic message when not.
func TestLoggingMapError_InviteeHasActivePaidSubscription_SurfacesDateWhenKnown(t *testing.T) {
	endsAt := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	err := &domain.InviteeHasActivePaidSubscriptionError{EligibleAfter: &endsAt}

	result := humaerrors.LoggingMapError(context.Background(), err)
	require.NotNil(t, result)
	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
	assert.Contains(t, statusErr.Error(), "March 15, 2026")
}

func TestLoggingMapError_InviteeHasActivePaidSubscription_NoDate_GenericMessage(t *testing.T) {
	err := &domain.InviteeHasActivePaidSubscriptionError{EligibleAfter: nil}

	result := humaerrors.LoggingMapError(context.Background(), err)
	require.NotNil(t, result)
	statusErr, ok := result.(huma.StatusError)
	require.True(t, ok, "expected huma.StatusError")
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
	assert.Contains(t, statusErr.Error(), "you can join a household once it ends")
}

// Issue #1367: household sentinel error → HTTP status mapping.
func TestLoggingMapError_Household(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"household not found", domain.ErrHouseholdNotFound, http.StatusNotFound},
		{"invite not found", domain.ErrInviteNotFound, http.StatusNotFound},
		{"member not found", domain.ErrMemberNotFound, http.StatusNotFound},
		{"already owns household", domain.ErrAlreadyOwnsHousehold, http.StatusConflict},
		{"already in household", domain.ErrAlreadyInHousehold, http.StatusConflict},
		{"household has active members", domain.ErrHouseholdHasActiveMembers, http.StatusConflict},
		{"household seats full", domain.ErrHouseholdSeatsFull, http.StatusConflict},
		{"invite pending exists", domain.ErrInvitePendingExists, http.StatusConflict},
		{"owner plan inactive", domain.ErrOwnerPlanInactive, http.StatusConflict},
		{"invitee has active paid subscription", domain.ErrInviteeHasActivePaidSubscription, http.StatusConflict},
		{"already covered by household", domain.ErrAlreadyCoveredByHousehold, http.StatusConflict},
		{"invite not pending", domain.ErrInviteNotPending, http.StatusUnauthorized},
		{"invite expired", domain.ErrInviteExpired, http.StatusUnauthorized},
		{"not household owner", domain.ErrNotHouseholdOwner, http.StatusForbidden},
		{"invite email mismatch", domain.ErrInviteEmailMismatch, http.StatusForbidden},
		{"invalid consent", domain.ErrInvalidConsent, http.StatusBadRequest},
		{"cannot invite self", domain.ErrCannotInviteSelf, http.StatusBadRequest},
		{"cannot remove owner", domain.ErrCannotRemoveOwner, http.StatusBadRequest},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := humaerrors.LoggingMapError(context.Background(), tc.err)
			require.NotNil(t, result)
			statusErr, ok := result.(huma.StatusError)
			require.True(t, ok, "expected huma.StatusError")
			assert.Equal(t, tc.status, statusErr.GetStatus())
		})
	}
}
