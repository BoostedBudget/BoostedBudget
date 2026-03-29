package posthog

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

type privacyConsentAdapter struct {
	repo repository.PrivacyRepository
}

// NewPrivacyConsentAdapter creates a ConsentChecker backed by the privacy repository.
func NewPrivacyConsentAdapter(repo repository.PrivacyRepository) ConsentChecker {
	return &privacyConsentAdapter{repo: repo}
}

func (a *privacyConsentAdapter) HasAnalyticsConsent(ctx context.Context, userID string) (bool, error) {
	settings, err := a.repo.GetByUserID(ctx, userID)
	if err != nil {
		return false, err
	}
	return settings.GDPRAnalyticsConsent, nil
}
