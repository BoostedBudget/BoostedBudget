package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// HouseholdHandler handles the Phase 3 (#1367) household REST endpoints: CRUD, member
// management, and the invite lifecycle including the consent-gated accept flow.
type HouseholdHandler struct {
	service     services.HouseholdService
	frontendURL string
}

// NewHouseholdHandler creates a new HouseholdHandler instance. frontendURL is used to
// build the shareable invite link returned by CreateInvite.
func NewHouseholdHandler(service services.HouseholdService, frontendURL string) *HouseholdHandler {
	return &HouseholdHandler{service: service, frontendURL: frontendURL}
}

// ─── POST /households ──────────────────────────────────────────────────────────

// CreateHouseholdRequestBody is the request body for creating a household.
type CreateHouseholdRequestBody struct {
	Name string `json:"name" doc:"Household display name" minLength:"1" maxLength:"255"`
}

// CreateHouseholdInput represents the input for creating a household.
type CreateHouseholdInput struct {
	inputs.AuthenticatedInput
	Body CreateHouseholdRequestBody
}

// CreateHouseholdOutput represents the response for creating a household.
type CreateHouseholdOutput struct {
	Body domain.Household
}

// CreateHousehold handles POST /api/v1/households. The caller becomes the household
// owner. 409 if the caller already owns or belongs to an active household.
func (h *HouseholdHandler) CreateHousehold(ctx context.Context, input *CreateHouseholdInput) (*CreateHouseholdOutput, error) {
	household, err := h.service.CreateHousehold(ctx, input.UserID, input.Body.Name)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &CreateHouseholdOutput{Body: *household}, nil
}

// ─── GET /households/me ─────────────────────────────────────────────────────────

// GetMyHouseholdInput represents the input for fetching the caller's household.
type GetMyHouseholdInput struct {
	inputs.AuthenticatedInput
}

// GetMyHouseholdResponseBody composes the household with seat usage/entitlement summary.
type GetMyHouseholdResponseBody struct {
	Household domain.Household                   `json:"household"`
	Summary   domain.HouseholdEntitlementSummary `json:"summary"`
}

// GetMyHouseholdOutput represents the response for fetching the caller's household.
type GetMyHouseholdOutput struct {
	Body GetMyHouseholdResponseBody
}

// GetMyHousehold handles GET /api/v1/households/me. 404 if the caller is not in any
// household.
func (h *HouseholdHandler) GetMyHousehold(ctx context.Context, input *GetMyHouseholdInput) (*GetMyHouseholdOutput, error) {
	household, summary, found, err := h.service.GetMyHousehold(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if !found {
		return nil, humaerrors.LoggingMapError(ctx, domain.ErrHouseholdNotFound)
	}
	return &GetMyHouseholdOutput{Body: GetMyHouseholdResponseBody{Household: *household, Summary: summary}}, nil
}

// ─── PATCH /households/me ───────────────────────────────────────────────────────

// UpdateHouseholdNameRequestBody is the request body for renaming a household.
type UpdateHouseholdNameRequestBody struct {
	Name string `json:"name" doc:"New household display name" minLength:"1" maxLength:"255"`
}

// UpdateHouseholdNameInput represents the input for renaming a household.
type UpdateHouseholdNameInput struct {
	inputs.AuthenticatedInput
	Body UpdateHouseholdNameRequestBody
}

// UpdateHouseholdNameOutput represents the response for renaming a household.
type UpdateHouseholdNameOutput struct {
	Body domain.Household
}

// UpdateHouseholdName handles PATCH /api/v1/households/me. Owner-only.
func (h *HouseholdHandler) UpdateHouseholdName(ctx context.Context, input *UpdateHouseholdNameInput) (*UpdateHouseholdNameOutput, error) {
	household, err := h.service.UpdateHouseholdName(ctx, input.UserID, input.Body.Name)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &UpdateHouseholdNameOutput{Body: *household}, nil
}

// ─── DELETE /households/me ──────────────────────────────────────────────────────

// DeleteHouseholdInput represents the input for disbanding a household.
type DeleteHouseholdInput struct {
	inputs.AuthenticatedInput
}

// DeleteHouseholdOutput represents the (empty) response for disbanding a household.
type DeleteHouseholdOutput struct{}

// DeleteHousehold handles DELETE /api/v1/households/me. Minimal stub (#1367 decision
// #2): owner-only; 409 if any active non-owner members exist. Full force-disband is
// Phase 7 (#1371).
func (h *HouseholdHandler) DeleteHousehold(ctx context.Context, input *DeleteHouseholdInput) (*DeleteHouseholdOutput, error) {
	if err := h.service.DeleteHousehold(ctx, input.UserID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &DeleteHouseholdOutput{}, nil
}

// ─── GET /households/me/members ─────────────────────────────────────────────────

// ListMembersInput represents the input for listing household members.
type ListMembersInput struct {
	inputs.AuthenticatedInput
}

// ListMembersOutput represents the response for listing household members.
// services.HouseholdMemberView carries role/status/joined_at plus a best-effort Clerk
// display name/avatar (#1367 — Phase 3 Round 2, GT-3) — deliberately no financial data,
// matching the issue's "no financial data" requirement.
type ListMembersOutput struct {
	Body []services.HouseholdMemberView
}

// ListMembers handles GET /api/v1/households/me/members.
func (h *HouseholdHandler) ListMembers(ctx context.Context, input *ListMembersInput) (*ListMembersOutput, error) {
	members, err := h.service.ListMembers(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &ListMembersOutput{Body: members}, nil
}

// ─── DELETE /households/me/members/{userId} ─────────────────────────────────────

// RemoveMemberInput represents the input for removing a member or self-leaving.
// TargetUserID (not UserID) avoids shadowing the embedded AuthenticatedInput.UserID —
// the actor's own resolved ID.
type RemoveMemberInput struct {
	inputs.AuthenticatedInput
	TargetUserID string `path:"userId" doc:"ID of the member to remove"`
}

// RemoveMemberOutput represents the (empty) response for removing a member.
type RemoveMemberOutput struct{}

// RemoveMember handles DELETE /api/v1/households/me/members/{userId}. The owner may
// remove any non-owner member; any member may remove themselves (self-leave). The
// owner can never be removed via this endpoint (exit only via DeleteHousehold).
func (h *HouseholdHandler) RemoveMember(ctx context.Context, input *RemoveMemberInput) (*RemoveMemberOutput, error) {
	if err := h.service.RemoveMember(ctx, input.UserID, input.TargetUserID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &RemoveMemberOutput{}, nil
}

// ─── POST /households/me/invites ────────────────────────────────────────────────

// CreateInviteRequestBody is the request body for inviting a member.
type CreateInviteRequestBody struct {
	Email string `json:"email" doc:"Email address to invite" format:"email"`
}

// CreateInviteInput represents the input for inviting a member.
type CreateInviteInput struct {
	inputs.AuthenticatedInput
	Body CreateInviteRequestBody
}

// CreateInviteResponseBody includes the shareable invite link alongside the invite
// record so the owner can share it manually if email delivery isn't fully configured
// (#1367 decision #4; see #1026).
type CreateInviteResponseBody struct {
	Invite     domain.HouseholdInvite `json:"invite"`
	InviteLink string                 `json:"invite_link"`
}

// CreateInviteOutput represents the response for inviting a member.
type CreateInviteOutput struct {
	Body CreateInviteResponseBody
}

// CreateInvite handles POST /api/v1/households/me/invites. Owner-only; 409 if no
// seats are available or a pending invite already exists for this email.
func (h *HouseholdHandler) CreateInvite(ctx context.Context, input *CreateInviteInput) (*CreateInviteOutput, error) {
	result, err := h.service.CreateInvite(ctx, input.UserID, input.Body.Email, h.frontendURL)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &CreateInviteOutput{Body: CreateInviteResponseBody{Invite: result.Invite, InviteLink: result.InviteLink}}, nil
}

// ─── GET /households/me/invites ─────────────────────────────────────────────────

// ListInvitesInput represents the input for listing pending invites.
type ListInvitesInput struct {
	inputs.AuthenticatedInput
}

// ListInvitesOutput represents the response for listing pending invites.
type ListInvitesOutput struct {
	Body []domain.HouseholdInvite
}

// ListInvites handles GET /api/v1/households/me/invites. Owner-only.
func (h *HouseholdHandler) ListInvites(ctx context.Context, input *ListInvitesInput) (*ListInvitesOutput, error) {
	invites, err := h.service.ListInvites(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &ListInvitesOutput{Body: invites}, nil
}

// ─── DELETE /households/me/invites/{id} ─────────────────────────────────────────

// RevokeInviteInput represents the input for revoking a pending invite.
type RevokeInviteInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Invite ID to revoke"`
}

// RevokeInviteOutput represents the (empty) response for revoking an invite.
type RevokeInviteOutput struct{}

// RevokeInvite handles DELETE /api/v1/households/me/invites/{id}. Owner-only.
func (h *HouseholdHandler) RevokeInvite(ctx context.Context, input *RevokeInviteInput) (*RevokeInviteOutput, error) {
	if err := h.service.RevokeInvite(ctx, input.UserID, input.ID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &RevokeInviteOutput{}, nil
}

// ─── GET /households/invites/{token} (unauthenticated) ──────────────────────────

// GetInvitePreviewInput represents the input for the public invite preview.
type GetInvitePreviewInput struct {
	Token string `path:"token" doc:"Raw invite token from the invite link"`
}

// GetInvitePreviewResponseBody is metadata-only (#1367 decision #1) — it deliberately
// does not list the invitee's accounts.
type GetInvitePreviewResponseBody struct {
	HouseholdName string `json:"household_name"`
	InviteeEmail  string `json:"invitee_email"`
	ExpiresAt     string `json:"expires_at"`
}

// GetInvitePreviewOutput represents the response for the public invite preview.
type GetInvitePreviewOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         GetInvitePreviewResponseBody
}

// GetInvitePreview handles GET /api/v1/households/invites/{token}. Unauthenticated —
// the token itself is the credential. Unknown, expired, and non-pending tokens all
// return the same 404 (no status oracle). Cache-Control: no-store — never cache a
// response keyed on a bearer token.
func (h *HouseholdHandler) GetInvitePreview(ctx context.Context, input *GetInvitePreviewInput) (*GetInvitePreviewOutput, error) {
	preview, err := h.service.GetInvitePreview(ctx, input.Token)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetInvitePreviewOutput{
		CacheControl: "no-store",
		Body: GetInvitePreviewResponseBody{
			HouseholdName: preview.HouseholdName,
			InviteeEmail:  preview.InviteeEmail,
			ExpiresAt:     preview.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		},
	}, nil
}

// ─── POST /households/invites/{token}/accept ────────────────────────────────────

// AcceptInviteRequestBody is the request body for accepting an invite.
type AcceptInviteRequestBody struct {
	Consent          bool     `json:"consent" doc:"Explicit consent to share accounts with the household"`
	HiddenAccountIDs []string `json:"hidden_account_ids,omitempty" doc:"user_account IDs (must be the caller's own) to keep private instead of sharing"`
}

// AcceptInviteInput represents the input for accepting an invite. Uses plain
// AuthenticatedInput (not AuthenticatedWithEmailInput) — the service compares against
// the caller's persisted, verified email, not a Clerk JWT claim, so no email resolution
// (and its Clerk-API fallback latency) is needed at the input-resolver layer.
type AcceptInviteInput struct {
	inputs.AuthenticatedInput
	Token string `path:"token" doc:"Raw invite token from the invite link"`
	Body  AcceptInviteRequestBody
}

// AcceptInviteResponseBody reports the caller's new membership.
type AcceptInviteResponseBody struct {
	HouseholdID string                 `json:"household_id"`
	Member      domain.HouseholdMember `json:"member"`
}

// AcceptInviteOutput represents the response for accepting an invite.
type AcceptInviteOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         AcceptInviteResponseBody
}

// AcceptInvite handles POST /api/v1/households/invites/{token}/accept — the
// consent-gated join transaction. See services.HouseholdService.AcceptInvite for the
// full guard sequence (email match, paying-invitee guard, owner-plan guard, seat
// re-check, IDOR-safe hidden-account validation).
func (h *HouseholdHandler) AcceptInvite(ctx context.Context, input *AcceptInviteInput) (*AcceptInviteOutput, error) {
	result, err := h.service.AcceptInvite(ctx, input.UserID, input.Token, input.Body.Consent, input.Body.HiddenAccountIDs)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &AcceptInviteOutput{
		CacheControl: "no-store",
		Body:         AcceptInviteResponseBody{HouseholdID: result.Household.ID, Member: result.Member},
	}, nil
}

// ─── POST /households/invites/{token}/decline ───────────────────────────────────

// DeclineInviteInput represents the input for declining an invite. Uses plain
// AuthenticatedInput — see AcceptInviteInput's doc comment for why no email claim is
// needed.
type DeclineInviteInput struct {
	inputs.AuthenticatedInput
	Token string `path:"token" doc:"Raw invite token from the invite link"`
}

// DeclineInviteOutput represents the (empty) response for declining an invite.
type DeclineInviteOutput struct {
	CacheControl string `header:"Cache-Control"`
}

// DeclineInvite handles POST /api/v1/households/invites/{token}/decline. Enforces the
// same email match as accept, preventing a third party from burning someone's invite.
func (h *HouseholdHandler) DeclineInvite(ctx context.Context, input *DeclineInviteInput) (*DeclineInviteOutput, error) {
	if err := h.service.DeclineInvite(ctx, input.UserID, input.Token); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &DeclineInviteOutput{CacheControl: "no-store"}, nil
}

// RegisterHouseholdRoutes registers all Phase 3 household routes with the Huma API.
// Literal subpaths (/me, /me/members, /me/invites) are registered before the
// unauthenticated /invites/{token} routes to avoid path-matching ambiguity.
func RegisterHouseholdRoutes(api huma.API, handler *HouseholdHandler) {
	huma.Register(api, huma.Operation{
		OperationID:   "createHousehold",
		Method:        http.MethodPost,
		Path:          "/api/v1/households",
		Summary:       "Create a household",
		Description:   "Creates a new household with the caller as owner.",
		Tags:          []string{"Household"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateHousehold)

	huma.Register(api, huma.Operation{
		OperationID: "getMyHousehold",
		Method:      http.MethodGet,
		Path:        "/api/v1/households/me",
		Summary:     "Get the caller's household",
		Tags:        []string{"Household"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetMyHousehold)

	huma.Register(api, huma.Operation{
		OperationID: "updateHouseholdName",
		Method:      http.MethodPatch,
		Path:        "/api/v1/households/me",
		Summary:     "Rename the household",
		Tags:        []string{"Household"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateHouseholdName)

	huma.Register(api, huma.Operation{
		OperationID:   "deleteHousehold",
		Method:        http.MethodDelete,
		Path:          "/api/v1/households/me",
		Summary:       "Disband the household",
		Description:   "Owner-only. Requires zero active non-owner members. Full force-disband is a Phase 7 (#1371) capability.",
		Tags:          []string{"Household"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteHousehold)

	huma.Register(api, huma.Operation{
		OperationID: "listHouseholdMembers",
		Method:      http.MethodGet,
		Path:        "/api/v1/households/me/members",
		Summary:     "List active household members",
		Tags:        []string{"Household"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ListMembers)

	huma.Register(api, huma.Operation{
		OperationID:   "removeHouseholdMember",
		Method:        http.MethodDelete,
		Path:          "/api/v1/households/me/members/{userId}",
		Summary:       "Remove a member or self-leave",
		Tags:          []string{"Household"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.RemoveMember)

	huma.Register(api, huma.Operation{
		OperationID:   "createHouseholdInvite",
		Method:        http.MethodPost,
		Path:          "/api/v1/households/me/invites",
		Summary:       "Invite a member by email",
		Tags:          []string{"Household"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateInvite)

	huma.Register(api, huma.Operation{
		OperationID: "listHouseholdInvites",
		Method:      http.MethodGet,
		Path:        "/api/v1/households/me/invites",
		Summary:     "List pending invites",
		Tags:        []string{"Household"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ListInvites)

	huma.Register(api, huma.Operation{
		OperationID:   "revokeHouseholdInvite",
		Method:        http.MethodDelete,
		Path:          "/api/v1/households/me/invites/{id}",
		Summary:       "Revoke a pending invite",
		Tags:          []string{"Household"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.RevokeInvite)

	huma.Register(api, huma.Operation{
		OperationID: "getHouseholdInvitePreview",
		Method:      http.MethodGet,
		Path:        "/api/v1/households/invites/{token}",
		Summary:     "Preview a household invite",
		Description: "Unauthenticated — the token is the credential. Metadata-only; does not list accounts.",
		Tags:        []string{"Household"},
		// No Security - public endpoint validated by the token.
	}, handler.GetInvitePreview)

	huma.Register(api, huma.Operation{
		OperationID: "acceptHouseholdInvite",
		Method:      http.MethodPost,
		Path:        "/api/v1/households/invites/{token}/accept",
		Summary:     "Accept a household invite",
		Tags:        []string{"Household"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.AcceptInvite)

	huma.Register(api, huma.Operation{
		OperationID:   "declineHouseholdInvite",
		Method:        http.MethodPost,
		Path:          "/api/v1/households/invites/{token}/decline",
		Summary:       "Decline a household invite",
		Tags:          []string{"Household"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeclineInvite)
}
