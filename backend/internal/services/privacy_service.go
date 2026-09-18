package services

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// PrivacyService defines the interface for privacy settings business logic
type PrivacyService interface {
	// GetPrivacySettings retrieves privacy settings for a user
	// Creates default GDPR-compliant settings if none exist (first-time user scenario)
	GetPrivacySettings(ctx context.Context, userID string) (*domain.PrivacySettings, error)

	// UpdatePrivacySettings validates and updates privacy settings
	// Enforces GDPR compliance rules (e.g., necessary consent cannot be disabled)
	// Returns updated settings on success
	UpdatePrivacySettings(ctx context.Context, userID, clerkSubject string, req *domain.UpdatePrivacySettingsRequest) (*domain.PrivacySettings, error)
}

// txBeginner starts the write transaction. Production is *pgxpool.Pool; tests use pgxmock.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PersonPropertyUnsetter removes person properties from the PostHog person profile. It is a
// narrow consumer interface on purpose: widening the shared posthog.Analytics interface would
// break every existing fake.
type PersonPropertyUnsetter interface {
	EmitPersonPropertyUnset(distinctID string, keys []string)
}

// RequestAuditSource reads the per-request audit metadata captured by middleware.RequestAudit.
// It is injected as a function value because internal/middleware imports internal/services
// (clerk.go), so importing middleware here would be an import cycle.
type RequestAuditSource func(ctx context.Context) (ip netip.Addr, ua string, requestID uuid.UUID)

// privacyService implements PrivacyService
type privacyService struct {
	repo         repository.PrivacyRepository
	auditRepo    repository.AuditLogMetadataRepository
	db           txBeginner
	unsetter     PersonPropertyUnsetter
	requestAudit RequestAuditSource
}

// NewPrivacyService creates a new PrivacyService instance
func NewPrivacyService(
	repo repository.PrivacyRepository,
	auditRepo repository.AuditLogMetadataRepository,
	db txBeginner,
	unsetter PersonPropertyUnsetter,
	requestAudit RequestAuditSource,
) PrivacyService {
	return &privacyService{
		repo:         repo,
		auditRepo:    auditRepo,
		db:           db,
		unsetter:     unsetter,
		requestAudit: requestAudit,
	}
}

// GetPrivacySettings retrieves privacy settings or creates defaults if they don't exist
// This follows the pattern of auto-creating user-specific resources on first access
func (s *privacyService) GetPrivacySettings(ctx context.Context, userID string) (*domain.PrivacySettings, error) {
	// Try to get existing privacy settings
	settings, err := s.repo.GetByUserID(ctx, userID)

	if err == repository.ErrPrivacySettingsNotFound {
		// First-time access - create default GDPR-compliant settings
		newSettings := domain.NewPrivacySettings(userID)
		if err := s.repo.Create(ctx, newSettings); err != nil {
			return nil, err
		}
		return newSettings, nil
	}

	if err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdatePrivacySettings validates and updates privacy settings with GDPR compliance
func (s *privacyService) UpdatePrivacySettings(ctx context.Context, userID, clerkSubject string, req *domain.UpdatePrivacySettingsRequest) (*domain.PrivacySettings, error) {
	// Validate request (GDPR compliance checks)
	// This ensures necessary consent cannot be disabled
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start privacy settings transaction: %w", err)
	}
	//nolint:errcheck // rollback is a no-op once Commit succeeds; best-effort cleanup otherwise.
	defer tx.Rollback(ctx)

	// Lock the row for the read-modify-write, so a concurrent consent change cannot interleave.
	settings, err := s.repo.GetByUserIDForUpdate(ctx, tx, userID)
	if err != nil {
		return nil, err
	}

	wasIdentifiedDiagnosticsEnabled := settings.IdentifiedDiagnosticsEnabled

	// Apply the updates to the settings
	// This method updates consent_timestamp and updated_at automatically
	req.ApplyTo(settings)

	// Validate the MERGED state — the request delta alone cannot see the stored row, so the
	// cross-field invariant (interaction capture requires analytics consent) is enforced here.
	if err := settings.Validate(); err != nil {
		return nil, err
	}

	// Persist the updated settings within the same transaction
	if err := s.repo.UpdateTx(ctx, tx, settings); err != nil {
		return nil, err
	}

	// Record the change in the same transaction as the state it describes. resource_id is the
	// user id so erasure redaction (which keys on resource_id) reaches this row.
	if err := s.auditRepo.LogInTxWithMetadata(ctx, tx, "privacy_settings.updated", "privacy_settings", userID,
		s.auditMetadata(ctx)); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit privacy settings transaction: %w", err)
	}

	// Post-commit: a true -> false delta of identified diagnostics removes the name from the
	// PostHog person profile. Emitted only after the new state is committed, so a failed write can
	// never strip a property the user still has enabled. The distinct id is the Clerk subject —
	// the PostHog person identity — never the internal user id.
	if wasIdentifiedDiagnosticsEnabled && !settings.IdentifiedDiagnosticsEnabled {
		s.unsetter.EmitPersonPropertyUnset(clerkSubject, []string{"name"})
	}

	return settings, nil
}

// auditMetadata reads the per-request audit metadata RequestAudit captured. A nil source (the
// middleware was not wired) records no metadata — the audit row still carries its load-bearing
// action/resource facts.
func (s *privacyService) auditMetadata(ctx context.Context) repository.AuditLogMetadata {
	if s.requestAudit == nil {
		return repository.AuditLogMetadata{}
	}
	ip, ua, requestID := s.requestAudit(ctx)
	return repository.AuditLogMetadata{IP: ip, UserAgent: ua, RequestID: requestID}
}
