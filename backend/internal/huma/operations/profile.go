package operations

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// ProfileHandler handles user profile-related HTTP requests.
type ProfileHandler struct {
	service               services.ProfileService
	overrideTierValidator func(tierName string) bool
}

// NewProfileHandler creates a new ProfileHandler instance.
func NewProfileHandler(service services.ProfileService) *ProfileHandler {
	return &ProfileHandler{service: service}
}

// SetOverrideTierValidator injects a function that reports whether a manual_tier_override value
// names a real, currently-configured tier. enrichUserProfile uses it to keep the profile
// response's derived comp fields (effective_status, is_comped, comp_tier) from reporting a
// malformed override as an active comp (Issue #1413 round-5 finding): G2 closed the
// access-granting hole on GetEffectiveSubscriptionStatusByUserID
// (backend/internal/repository/profile_repository.go), but this REPRESENTATION path kept
// calling ComputeEffectiveStatus()/HasActiveManualOverride() unconditionally, so a corrupt
// override could still make the profile response — and therefore the frontend's derived
// hasWriteAccess — claim a comp the read-only middleware would then 403 on write.
//
// Same setter-injection convention, same func(string) bool signature, and the same nil-safe
// "skip, don't deny" semantics as repository.profileRepository.SetOverrideTierValidator — see
// that method's doc comment (profile_repository.go) for the full rationale, which applies here
// unchanged: main.go loads the tier config once at startup and os.Exit(1)s if that fails, so in
// production this validator is either wired for the entire process lifetime or the server never
// boots. If never called, validation is skipped and an active override is trusted — the pre-fix
// behavior — rather than denying every comp, which would turn a future wiring oversight into an
// instant outage for every currently-comped user.
func (h *ProfileHandler) SetOverrideTierValidator(validator func(tierName string) bool) {
	h.overrideTierValidator = validator
}

// --- Input/Output Types ---

// GetProfileInput represents the input for getting user profile.
type GetProfileInput struct {
	inputs.AuthenticatedWithEmailInput
}

// GetProfileOutput represents the response for getting user profile.
type GetProfileOutput struct {
	Body *domain.User
}

// UpdateProfileInput represents the input for updating user profile.
type UpdateProfileInput struct {
	inputs.AuthenticatedInput
	Body domain.UpdateProfileRequest
}

// UpdateProfileOutput represents the response for updating user profile.
type UpdateProfileOutput struct {
	Body *domain.User
}

// --- Handler Methods ---

// GetProfile handles GET /api/v1/user/profile requests.
// Retrieves the authenticated user's profile, creating one if it's their first login.
func (h *ProfileHandler) GetProfile(ctx context.Context, input *GetProfileInput) (*GetProfileOutput, error) {
	user, err := h.service.GetProfile(ctx, input.UserID, input.Email)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retrieve profile")
	}

	return &GetProfileOutput{Body: h.enrichUserProfile(ctx, user)}, nil
}

// UpdateProfile handles PUT /api/v1/user/profile requests.
// Updates the authenticated user's profile with validated data.
func (h *ProfileHandler) UpdateProfile(ctx context.Context, input *UpdateProfileInput) (*UpdateProfileOutput, error) {
	user, err := h.service.UpdateProfile(ctx, input.UserID, &input.Body)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidEmail):
			return nil, huma.Error400BadRequest("Invalid email address")
		case errors.Is(err, services.ErrInvalidRegion):
			return nil, huma.Error400BadRequest("Invalid primary region")
		case errors.Is(err, services.ErrInvalidTimezone):
			return nil, huma.Error422UnprocessableEntity("Invalid timezone: must be a valid IANA timezone (e.g. America/New_York)")
		case errors.Is(err, services.ErrInvalidTimezoneSource):
			return nil, huma.Error422UnprocessableEntity("Invalid timezone_source: must be one of default, auto, user")
		case errors.Is(err, repository.ErrDuplicateEmail):
			return nil, huma.Error409Conflict("Email address already in use")
		default:
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
	}

	return &UpdateProfileOutput{Body: h.enrichUserProfile(ctx, user)}, nil
}

// enrichUserProfile computes derived fields for the user profile response.
func (h *ProfileHandler) enrichUserProfile(ctx context.Context, user *domain.User) *domain.User {
	user.ComputeTrialDaysRemaining()
	user.EffectiveTier = user.ComputeEffectiveTier()

	// Issue #1413: expose the effective status/comp state so the frontend stops rendering
	// a comped user as "expired". subscription_status keeps its exact current meaning
	// (admin UI and billing pages read it) — these are additive, derived fields.
	//
	// Round-5 finding: manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
	// constraint. Use EffectiveStatusWithActiveOverride (not ComputeEffectiveStatus() and
	// HasActiveManualOverride() called separately) for the same single-clock-read reason
	// documented on that method, then reject a malformed override the same way
	// GetEffectiveSubscriptionStatusByUserID does on the access-granting path — this
	// REPRESENTATION path must never claim a comp the middleware would not actually grant.
	status, overrideValue, hasOverride := user.EffectiveStatusWithActiveOverride()
	if hasOverride && h.overrideTierValidator != nil && !h.overrideTierValidator(overrideValue) {
		slog.WarnContext(ctx, "rejecting manual tier override in profile response: value does not name a configured tier",
			"user_id", user.ID, "manual_tier_override", overrideValue)
		status = user.SubscriptionStatus
		hasOverride = false
	}

	user.EffectiveStatus = status
	user.IsComped = hasOverride
	if user.IsComped {
		user.CompTier = overrideValue
		user.CompEndsAt = user.ManualTierEndsAt // nil = permanent comp
	}
	return user
}

// RegisterProfileRoutes registers profile routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterProfileRoutes(api huma.API, handler *ProfileHandler) {
	// GET /api/v1/user/profile - Get user profile
	huma.Register(api, huma.Operation{
		OperationID: "getUserProfile",
		Method:      http.MethodGet,
		Path:        "/api/v1/user/profile",
		Summary:     "Get user profile",
		Description: "Retrieves the authenticated user's profile, creating one if it's their first login.",
		Tags:        []string{"Profile"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetProfile)

	// PUT /api/v1/user/profile - Update user profile
	huma.Register(api, huma.Operation{
		OperationID: "updateUserProfile",
		Method:      http.MethodPut,
		Path:        "/api/v1/user/profile",
		Summary:     "Update user profile",
		Description: "Updates the authenticated user's profile with validated data.",
		Tags:        []string{"Profile"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateProfile)
}
