package domain_test

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestRateLimitTier_String(t *testing.T) {
	tests := []struct {
		tier domain.RateLimitTier
		want string
	}{
		{domain.RateLimitTierRegular, "regular"},
		{domain.RateLimitTierAuth, "auth"},
		{domain.RateLimitTierSentinel, "sentinel"},
	}

	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("RateLimitTier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

func TestRateLimitTier_String_UnknownValue(t *testing.T) {
	unknown := domain.RateLimitTier(99)
	if got := unknown.String(); got == "" {
		t.Errorf("RateLimitTier(99).String() returned empty string, want a non-empty fallback")
	}
}

func TestRateLimitDecision_Fields(t *testing.T) {
	d := domain.RateLimitDecision{Allowed: true, RetryAfter: 2 * time.Second}
	if !d.Allowed {
		t.Error("expected Allowed to be true")
	}
	if d.RetryAfter != 2*time.Second {
		t.Errorf("RetryAfter = %v, want 2s", d.RetryAfter)
	}
}
