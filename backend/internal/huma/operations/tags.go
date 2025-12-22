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

// TagHandler handles user tag-related HTTP requests.
type TagHandler struct {
	service services.UserTagService
}

// NewTagHandler creates a new TagHandler instance.
func NewTagHandler(service services.UserTagService) *TagHandler {
	return &TagHandler{service: service}
}

// --- Input/Output Types ---

// GetUserTagsInput represents the input for getting user tags.
type GetUserTagsInput struct {
	inputs.AuthenticatedInput
}

// GetUserTagsOutput represents the response for getting user tags.
type GetUserTagsOutput struct {
	Body []*domain.UserTag
}

// GetTagByIDInput represents the input for getting a tag by ID.
type GetTagByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Tag UUID"`
}

// GetTagByIDOutput represents the response for getting a tag by ID.
type GetTagByIDOutput struct {
	Body *domain.UserTag
}

// CreateTagInput represents the input for creating a tag.
type CreateTagInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateUserTagRequest
}

// CreateTagOutput represents the response for creating a tag.
type CreateTagOutput struct {
	Body *domain.UserTag
}

// UpdateTagInput represents the input for updating a tag.
type UpdateTagInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Tag UUID"`
	Body domain.UpdateUserTagRequest
}

// UpdateTagOutput represents the response for updating a tag.
type UpdateTagOutput struct {
	Body *domain.UserTag
}

// DeleteTagInput represents the input for deleting a tag.
type DeleteTagInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Tag UUID"`
}

// --- Handler Methods ---

// GetUserTags handles GET /api/v1/tags requests.
// Retrieves all tags for the authenticated user.
func (h *TagHandler) GetUserTags(ctx context.Context, input *GetUserTagsInput) (*GetUserTagsOutput, error) {
	tags, err := h.service.GetUserTags(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Return empty array instead of null if no tags
	if tags == nil {
		tags = []*domain.UserTag{}
	}

	return &GetUserTagsOutput{Body: tags}, nil
}

// GetTagByID handles GET /api/v1/tags/:id requests.
// Retrieves a specific tag by ID.
func (h *TagHandler) GetTagByID(ctx context.Context, input *GetTagByIDInput) (*GetTagByIDOutput, error) {
	tag, err := h.service.GetTagByID(ctx, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Verify ownership
	if tag.UserID != input.UserID {
		return nil, huma.Error404NotFound("Tag not found")
	}

	return &GetTagByIDOutput{Body: tag}, nil
}

// CreateTag handles POST /api/v1/tags requests.
// Creates a new tag for the authenticated user.
func (h *TagHandler) CreateTag(ctx context.Context, input *CreateTagInput) (*CreateTagOutput, error) {
	tag, err := h.service.CreateTag(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &CreateTagOutput{Body: tag}, nil
}

// UpdateTag handles PUT /api/v1/tags/:id requests.
// Updates a tag's name or color.
func (h *TagHandler) UpdateTag(ctx context.Context, input *UpdateTagInput) (*UpdateTagOutput, error) {
	tag, err := h.service.UpdateTag(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &UpdateTagOutput{Body: tag}, nil
}

// DeleteTag handles DELETE /api/v1/tags/:id requests.
// Removes a tag from the user's collection.
func (h *TagHandler) DeleteTag(ctx context.Context, input *DeleteTagInput) (*struct{}, error) {
	err := h.service.DeleteTag(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return nil, nil
}

// RegisterTagRoutes registers tag routes with the Huma API.
// Note: These routes require the Auth0 middleware to be applied.
func RegisterTagRoutes(api huma.API, handler *TagHandler) {
	// GET /api/v1/tags - Get all user tags
	huma.Register(api, huma.Operation{
		OperationID: "getUserTags",
		Method:      http.MethodGet,
		Path:        "/api/v1/tags",
		Summary:     "Get user tags",
		Description: "Retrieves all tags for the authenticated user.",
		Tags:        []string{"Tags"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetUserTags)

	// GET /api/v1/tags/{id} - Get tag by ID
	huma.Register(api, huma.Operation{
		OperationID: "getTagByID",
		Method:      http.MethodGet,
		Path:        "/api/v1/tags/{id}",
		Summary:     "Get tag by ID",
		Description: "Retrieves a specific tag by its UUID.",
		Tags:        []string{"Tags"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetTagByID)

	// POST /api/v1/tags - Create a new tag
	huma.Register(api, huma.Operation{
		OperationID:   "createTag",
		Method:        http.MethodPost,
		Path:          "/api/v1/tags",
		Summary:       "Create a new tag",
		Description:   "Creates a new tag for the authenticated user.",
		Tags:          []string{"Tags"},
		Security:      []map[string][]string{{"auth0": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateTag)

	// PUT /api/v1/tags/{id} - Update a tag
	huma.Register(api, huma.Operation{
		OperationID: "updateTag",
		Method:      http.MethodPut,
		Path:        "/api/v1/tags/{id}",
		Summary:     "Update a tag",
		Description: "Updates a tag's name or color.",
		Tags:        []string{"Tags"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.UpdateTag)

	// DELETE /api/v1/tags/{id} - Delete a tag
	huma.Register(api, huma.Operation{
		OperationID:   "deleteTag",
		Method:        http.MethodDelete,
		Path:          "/api/v1/tags/{id}",
		Summary:       "Delete a tag",
		Description:   "Removes a tag from the user's collection.",
		Tags:          []string{"Tags"},
		Security:      []map[string][]string{{"auth0": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteTag)
}
