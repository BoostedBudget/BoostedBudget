package errors_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

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
