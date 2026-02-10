package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubscriptionStatus_IsActiveSubscription(t *testing.T) {
	tests := []struct {
		name     string
		status   SubscriptionStatus
		expected bool
	}{
		{
			name:     "active status should be active",
			status:   SubscriptionStatusActive,
			expected: true,
		},
		{
			name:     "trial status should be active",
			status:   SubscriptionStatusTrial,
			expected: true,
		},
		{
			name:     "trialing status should be active",
			status:   SubscriptionStatusTrialing,
			expected: true,
		},
		{
			name:     "inactive status should not be active",
			status:   SubscriptionStatusInactive,
			expected: false,
		},
		{
			name:     "cancelled status should not be active",
			status:   SubscriptionStatusCancelled,
			expected: false,
		},
		{
			name:     "past_due status should not be active",
			status:   SubscriptionStatusPastDue,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.IsActiveSubscription()
			assert.Equal(t, tt.expected, result)
		})
	}
}
