package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeSubscriptionStatus verifies the canonical Paddle→domain status mapping.
// #933: paused and canceled (single-l) both map to cancelled; all other values pass through.
func TestNormalizeSubscriptionStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"paused maps to cancelled", "paused", "cancelled"},
		{"canceled (single-l) maps to cancelled", "canceled", "cancelled"},
		{"active passthrough", "active", "active"},
		{"trialing passthrough", "trialing", "trialing"},
		{"past_due passthrough", "past_due", "past_due"},
		{"trial passthrough", "trial", "trial"},
		{"inactive passthrough", "inactive", "inactive"},
		{"cancelled (double-l) passthrough", "cancelled", "cancelled"},
		{"empty passthrough", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeSubscriptionStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

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
