package domain

import "time"

// ReferralCode represents a user's referral code
type ReferralCode struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Code      string     `json:"code"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Referral represents a referral relationship between users
type Referral struct {
	ID             string     `json:"id"`
	ReferrerID     string     `json:"referrer_id"`
	ReferredID     string     `json:"referred_id"`
	ReferralCodeID *string    `json:"referral_code_id,omitempty"`
	Status         string     `json:"status"` // pending, completed, expired, cancelled
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// ReferralStatus constants
const (
	ReferralStatusPending   = "pending"
	ReferralStatusCompleted = "completed"
	ReferralStatusExpired   = "expired"
	ReferralStatusCancelled = "cancelled"
)
