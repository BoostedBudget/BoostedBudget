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

// CategorizationRuleHandler handles categorization rule-related HTTP requests.
type CategorizationRuleHandler struct {
	service services.CategorizationRuleService
}

// NewCategorizationRuleHandler creates a new CategorizationRuleHandler instance.
func NewCategorizationRuleHandler(service services.CategorizationRuleService) *CategorizationRuleHandler {
	return &CategorizationRuleHandler{service: service}
}

// --- Input/Output Types ---

// GetUserRulesInput represents the input for getting user rules.
type GetUserRulesInput struct {
	inputs.AuthenticatedInput
}

// GetUserRulesOutput represents the response for getting user rules.
type GetUserRulesOutput struct {
	Body []*domain.CategorizationRule
}

// GetRuleByIDInput represents the input for getting a rule by ID.
type GetRuleByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Rule UUID"`
}

// GetRuleByIDOutput represents the response for getting a rule by ID.
type GetRuleByIDOutput struct {
	Body *domain.CategorizationRule
}

// CreateRuleInput represents the input for creating a rule.
type CreateRuleInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateCategorizationRuleRequest
}

// CreateRuleOutput represents the response for creating a rule.
type CreateRuleOutput struct {
	Body *domain.CategorizationRule
}

// UpdateRuleInput represents the input for updating a rule.
type UpdateRuleInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Rule UUID"`
	Body domain.UpdateCategorizationRuleRequest
}

// UpdateRuleOutput represents the response for updating a rule.
type UpdateRuleOutput struct {
	Body *domain.CategorizationRule
}

// DeleteRuleInput represents the input for deleting a rule.
type DeleteRuleInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Rule UUID"`
}

// PreviewRuleInput is the input for a rule dry run (#1625). The body is the same shape as a
// create request, so the rule builder can preview exactly what it is about to save.
type PreviewRuleInput struct {
	inputs.AuthenticatedInput
	Body PreviewRuleBody
}

// PreviewRuleBody is a create request plus a scan limit.
type PreviewRuleBody struct {
	domain.CreateCategorizationRuleRequest
	// Limit caps how many recent transactions are examined. Defaults to
	// services.PreviewScanDefault, capped at services.PreviewScanMax — a preview is a synchronous
	// read on a form's hot path, not a history scan.
	Limit int `json:"limit,omitempty" minimum:"1" maximum:"500"`
}

// PreviewRuleOutput is the response for a rule dry run.
type PreviewRuleOutput struct {
	Body *domain.RulePreviewResult
}

// ToggleRuleInput represents the input for toggling a rule.
type ToggleRuleInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Rule UUID"`
	Body domain.ToggleCategorizationRuleRequest
}

// ToggleRuleOutput represents the response for toggling a rule.
type ToggleRuleOutput struct {
	Body *domain.CategorizationRule
}

// --- Handler Methods ---

// GetUserRules handles GET /api/v1/auto-categorization requests.
// Retrieves all categorization rules for the authenticated user.
func (h *CategorizationRuleHandler) GetUserRules(ctx context.Context, input *GetUserRulesInput) (*GetUserRulesOutput, error) {
	rules, err := h.service.GetUserRules(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null if no rules
	if rules == nil {
		rules = []*domain.CategorizationRule{}
	}

	return &GetUserRulesOutput{Body: rules}, nil
}

// GetRuleByID handles GET /api/v1/auto-categorization/:id requests.
// Retrieves a specific rule by ID.
func (h *CategorizationRuleHandler) GetRuleByID(ctx context.Context, input *GetRuleByIDInput) (*GetRuleByIDOutput, error) {
	rule, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetRuleByIDOutput{Body: rule}, nil
}

// CreateRule handles POST /api/v1/auto-categorization requests.
// Creates a new categorization rule for the authenticated user.
func (h *CategorizationRuleHandler) CreateRule(ctx context.Context, input *CreateRuleInput) (*CreateRuleOutput, error) {
	rule, err := h.service.CreateRule(ctx, input.UserID, &input.Body)
	if err != nil {
		// #1625: no allowlist here any more. This used to exact-string-match THREE messages to
		// map them to 400; every other validation failure — an unknown condition type included —
		// fell through to the mapper's default and became a 500 that also fired
		// sentry.CaptureException, so client input mistakes were logged as server errors.
		// domain.Validate() now returns sentinel-wrapped errors that the mapper classifies, which
		// fixes create and update together and stops the Sentry noise as a side effect
		// (LoggingMapError only reports at >= 500).
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &CreateRuleOutput{Body: rule}, nil
}

// UpdateRule handles PUT /api/v1/auto-categorization/:id requests.
// Updates a rule's configuration.
func (h *CategorizationRuleHandler) UpdateRule(ctx context.Context, input *UpdateRuleInput) (*UpdateRuleOutput, error) {
	rule, err := h.service.UpdateRule(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateRuleOutput{Body: rule}, nil
}

// DeleteRule handles DELETE /api/v1/auto-categorization/:id requests.
// Removes a rule.
func (h *CategorizationRuleHandler) DeleteRule(ctx context.Context, input *DeleteRuleInput) (*struct{}, error) {
	err := h.service.DeleteRule(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// ToggleRule handles POST /api/v1/auto-categorization/:id/toggle requests.
// Enables or disables a rule.
func (h *CategorizationRuleHandler) ToggleRule(ctx context.Context, input *ToggleRuleInput) (*ToggleRuleOutput, error) {
	rule, err := h.service.ToggleRule(ctx, input.UserID, input.ID, input.Body.Enabled)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &ToggleRuleOutput{Body: rule}, nil
}

// GetConditionSchemaInput takes nothing but the caller's identity.
type GetConditionSchemaInput struct {
	inputs.AuthenticatedInput
}

// GetConditionSchemaOutput is the published condition matrix.
type GetConditionSchemaOutput struct {
	Body domain.ConditionSchemaResponse
}

// GetConditionSchema handles GET /api/v1/auto-categorization/condition-schema.
//
// #1625 follow-up. The enum tags already make the VOCABULARY discoverable; they do not make the
// MATRIX discoverable — which operators a field accepts, what shape each needs, which closed value
// sets apply, or what the values MEAN. Two independent reviews found that an agent still could not
// build the reporter's own rule ("a $1,000 Zelle outflow") without guessing the sign convention,
// and a wrong guess validates silently and simply never matches.
//
// Static and user-independent: it is authenticated only because everything under /api/v1 is.
func (h *CategorizationRuleHandler) GetConditionSchema(_ context.Context, _ *GetConditionSchemaInput) (*GetConditionSchemaOutput, error) {
	return &GetConditionSchemaOutput{Body: domain.ConditionSchema()}, nil
}

// PreviewRule handles POST /api/v1/auto-categorization/preview.
// Reports which recent transactions a candidate rule WOULD match, without saving anything.
func (h *CategorizationRuleHandler) PreviewRule(ctx context.Context, input *PreviewRuleInput) (*PreviewRuleOutput, error) {
	rule := ruleFromCreateRequest(input.UserID, &input.Body.CreateCategorizationRuleRequest)

	result, err := h.service.PreviewRule(ctx, input.UserID, rule, input.Body.Limit)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &PreviewRuleOutput{Body: result}, nil
}

// ruleFromCreateRequest builds an in-memory candidate rule. It is deliberately NOT the service's
// create path: nothing here is persisted, and no ID is assigned.
func ruleFromCreateRequest(userID string, req *domain.CreateCategorizationRuleRequest) *domain.CategorizationRule {
	return &domain.CategorizationRule{
		UserID:                 userID,
		Name:                   req.Name,
		Description:            req.Description,
		Priority:               req.Priority,
		Conditions:             req.Conditions,
		ConditionLogic:         req.ConditionLogic,
		TargetKind:             req.TargetKind,
		TargetPFCPrimary:       req.TargetPFCPrimary,
		TargetPFCDetailed:      req.TargetPFCDetailed,
		TargetCustomCategoryID: req.TargetCustomCategoryID,
		TargetTags:             req.TargetTags,
		TagBehavior:            req.TagBehavior,
	}
}

// RegisterCategorizationRuleRoutes registers categorization rule routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterCategorizationRuleRoutes(api huma.API, handler *CategorizationRuleHandler) {
	// GET /api/v1/auto-categorization - Get all user rules
	huma.Register(api, huma.Operation{
		OperationID: "getUserRules",
		Method:      http.MethodGet,
		Path:        "/api/v1/auto-categorization",
		Summary:     "Get user categorization rules",
		Description: "Retrieves all categorization rules for the authenticated user, ordered by priority.",
		Tags:        []string{"Auto-Categorization"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetUserRules)

	// GET /api/v1/auto-categorization/condition-schema - the published condition matrix
	//
	// MUST be registered before GET /{id}. humatest resolves routes in registration order, so a
	// literal that shares a prefix with a parameterised sibling is shadowed by it when registered
	// second — which is what happened here: this route answered from GetRuleByID (200 with a null
	// body) and no test noticed, because none called it. Production's Echo router prefers the
	// literal regardless, so ordering never affected production; it only made the endpoint
	// untestable. Pinned by TestConditionSchemaRoute_IsNotShadowedByTheIDRoute.
	huma.Register(api, huma.Operation{
		OperationID: "getConditionSchema",
		Method:      http.MethodGet,
		Path:        "/api/v1/auto-categorization/condition-schema",
		Summary:     "Get the condition matrix",
		Description: "Returns every condition field, the operators each accepts, the value shape " +
			"each operator requires, closed value sets inlined where practical, and the semantics " +
			"that cannot be inferred from a type — notably that signed_amount is POSITIVE for " +
			"money leaving the account. The two PFC fields (pfc_primary, pfc_detailed) have " +
			"closed sets too, but each field's own notes name the endpoint that publishes it " +
			"instead of duplicating ~120 values here. Derived from the same matrix the validator " +
			"enforces, so it cannot drift from it.",
		Tags:     []string{"Auto-Categorization"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetConditionSchema)

	// GET /api/v1/auto-categorization/{id} - Get rule by ID
	huma.Register(api, huma.Operation{
		OperationID: "getRuleByID",
		Method:      http.MethodGet,
		Path:        "/api/v1/auto-categorization/{id}",
		Summary:     "Get rule by ID",
		Description: "Retrieves a specific categorization rule by its UUID.",
		Tags:        []string{"Auto-Categorization"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetRuleByID)

	// POST /api/v1/auto-categorization - Create a new rule
	huma.Register(api, huma.Operation{
		OperationID:   "createRule",
		Method:        http.MethodPost,
		Path:          "/api/v1/auto-categorization",
		Summary:       "Create a new categorization rule",
		Description:   "Creates a new categorization rule for the authenticated user.",
		Tags:          []string{"Auto-Categorization"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateRule)

	// PUT /api/v1/auto-categorization/{id} - Update a rule
	huma.Register(api, huma.Operation{
		OperationID: "updateRule",
		Method:      http.MethodPut,
		Path:        "/api/v1/auto-categorization/{id}",
		Summary:     "Update a rule",
		Description: "Updates a categorization rule's configuration.",
		Tags:        []string{"Auto-Categorization"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateRule)

	// DELETE /api/v1/auto-categorization/{id} - Delete a rule
	huma.Register(api, huma.Operation{
		OperationID:   "deleteRule",
		Method:        http.MethodDelete,
		Path:          "/api/v1/auto-categorization/{id}",
		Summary:       "Delete a rule",
		Description:   "Removes a categorization rule.",
		Tags:          []string{"Auto-Categorization"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteRule)

	// POST /api/v1/auto-categorization/preview - Dry-run a candidate rule
	//
	// Unlike condition-schema above, ordering is not load-bearing here: this is a POST and there
	// is no POST /{id} for it to be shadowed by. (An earlier revision of this comment claimed it
	// was "registered BEFORE the {id} routes" — it was not, and for preview it does not matter.)
	huma.Register(api, huma.Operation{
		OperationID: "previewRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/auto-categorization/preview",
		Summary:     "Preview a categorization rule",
		Description: "Reports which of your recent transactions a candidate rule would match, " +
			"WITHOUT saving the rule. Condition, shape and target-category validation is identical " +
			"to createRule. Two things preview cannot check, because they depend on state at the " +
			"moment of the write: rule names must be unique per user, and the target category must " +
			"still exist when you save.",
		Tags:     []string{"Auto-Categorization"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.PreviewRule)

	// POST /api/v1/auto-categorization/{id}/toggle - Toggle a rule
	huma.Register(api, huma.Operation{
		OperationID: "toggleRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/auto-categorization/{id}/toggle",
		Summary:     "Toggle a rule",
		Description: "Enables or disables a categorization rule.",
		Tags:        []string{"Auto-Categorization"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ToggleRule)
}
