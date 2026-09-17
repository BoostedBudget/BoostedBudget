package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

type PostHogErasureOperator interface {
	RecordOperatorEvidence(context.Context, string, string, string, string) (string, error)
	RetargetUnconfigured(context.Context, string, string, string, string) (bool, error)
	ListOutstanding(context.Context, int) ([]repository.PostHogErasureOperatorRow, error)
	RevealOperatorIdentity(context.Context, string, string) (repository.PostHogErasureOperatorIdentity, error)
	RequeueDeadLetter(context.Context, string, string) (bool, error)
}

func (h *AdminHandler) WithPostHogErasureOperator(operator PostHogErasureOperator) *AdminHandler {
	h.postHogErasureOperator = operator
	return h
}

type PostHogErasureEvidenceInput struct {
	inputs.AdminAuthenticatedInput
	ErasureID string `path:"erasure_id" format:"uuid"`
	Body      PostHogErasureEvidenceBody
}
type PostHogErasureEvidenceBody struct {
	Surface  string `json:"surface" enum:"events,recordings,surveys,support"`
	Evidence string `json:"evidence" enum:"provider_zero,provider_case_deleted"`
}
type PostHogErasureRetargetInput struct {
	inputs.AdminAuthenticatedInput
	ErasureID string `path:"erasure_id" format:"uuid"`
	Body      PostHogErasureRetargetBody
}
type PostHogErasureRetargetBody struct {
	ProjectID string `json:"project_id" minLength:"1"`
	Region    string `json:"region" enum:"us,eu"`
}
type PostHogErasureOperatorOutput struct {
	Body struct {
		State string `json:"state"`
	}
}
type ListPostHogErasuresInput struct {
	inputs.AdminAuthenticatedInput
	Limit int `query:"limit" minimum:"1" maximum:"100" default:"50"`
}
type ListPostHogErasuresOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         []repository.PostHogErasureOperatorRow
}
type RevealPostHogErasureInput struct {
	inputs.AdminAuthenticatedInput
	ErasureID string `path:"erasure_id" format:"uuid"`
}
type RevealPostHogErasureOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         repository.PostHogErasureOperatorIdentity
}

func (h *AdminHandler) ListPostHogErasures(ctx context.Context, input *ListPostHogErasuresInput) (*ListPostHogErasuresOutput, error) {
	if h.postHogErasureOperator == nil {
		return nil, huma.Error503ServiceUnavailable("PostHog erasure operator unavailable")
	}
	rows, err := h.postHogErasureOperator.ListOutstanding(ctx, input.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("Unable to list PostHog erasures")
	}
	return &ListPostHogErasuresOutput{CacheControl: "no-store", Body: rows}, nil
}
func (h *AdminHandler) RevealPostHogErasure(ctx context.Context, input *RevealPostHogErasureInput) (*RevealPostHogErasureOutput, error) {
	if h.postHogErasureOperator == nil {
		return nil, huma.Error503ServiceUnavailable("PostHog erasure operator unavailable")
	}
	identity, err := h.postHogErasureOperator.RevealOperatorIdentity(ctx, input.ErasureID, input.UserID)
	if err != nil {
		return nil, huma.Error404NotFound("PostHog erasure not found")
	}
	return &RevealPostHogErasureOutput{CacheControl: "no-store", Body: identity}, nil
}

func (h *AdminHandler) RecordPostHogErasureEvidence(ctx context.Context, input *PostHogErasureEvidenceInput) (*PostHogErasureOperatorOutput, error) {
	if h.postHogErasureOperator == nil {
		return nil, huma.Error503ServiceUnavailable("PostHog erasure operator unavailable")
	}
	state, err := h.postHogErasureOperator.RecordOperatorEvidence(ctx, input.ErasureID, input.UserID, input.Body.Surface, input.Body.Evidence)
	if err != nil {
		return nil, huma.Error409Conflict("PostHog erasure evidence was not accepted")
	}
	out := &PostHogErasureOperatorOutput{}
	out.Body.State = state
	return out, nil
}

func (h *AdminHandler) RetargetPostHogErasure(ctx context.Context, input *PostHogErasureRetargetInput) (*PostHogErasureOperatorOutput, error) {
	if h.postHogErasureOperator == nil {
		return nil, huma.Error503ServiceUnavailable("PostHog erasure operator unavailable")
	}
	ok, err := h.postHogErasureOperator.RetargetUnconfigured(ctx, input.ErasureID, input.UserID, input.Body.ProjectID, input.Body.Region)
	if err != nil || !ok {
		return nil, huma.Error409Conflict("PostHog erasure obligation cannot be retargeted")
	}
	out := &PostHogErasureOperatorOutput{}
	out.Body.State = "retrying"
	return out, nil
}

func (h *AdminHandler) ReplayPostHogErasure(ctx context.Context, input *RevealPostHogErasureInput) (*PostHogErasureOperatorOutput, error) {
	if h.postHogErasureOperator == nil {
		return nil, huma.Error503ServiceUnavailable("PostHog erasure operator unavailable")
	}
	ok, err := h.postHogErasureOperator.RequeueDeadLetter(ctx, input.ErasureID, input.UserID)
	if err != nil || !ok {
		return nil, huma.Error409Conflict("PostHog erasure is not replayable")
	}
	out := &PostHogErasureOperatorOutput{}
	out.Body.State = "retrying"
	return out, nil
}

func RegisterAdminPostHogErasureOperations(api huma.API, handler *AdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listPostHogErasures", Method: http.MethodGet,
		Path: "/api/v1/admin/posthog-erasure", Summary: "List outstanding PostHog erasures",
		Tags: []string{"Admin"}, Security: []map[string][]string{{"clerk": {}}},
	}, handler.ListPostHogErasures)
	huma.Register(api, huma.Operation{
		OperationID: "revealPostHogErasure", Method: http.MethodPost,
		Path:    "/api/v1/admin/posthog-erasure/{erasure_id}/reveal",
		Summary: "Audit and reveal identifiers needed for provider verification",
		Tags:    []string{"Admin"}, Security: []map[string][]string{{"clerk": {}}},
	}, handler.RevealPostHogErasure)
	huma.Register(api, huma.Operation{
		OperationID: "recordPostHogErasureEvidence", Method: http.MethodPost,
		Path:    "/api/v1/admin/posthog-erasure/{erasure_id}/evidence",
		Summary: "Record bounded PostHog erasure evidence", Tags: []string{"Admin"}, Security: []map[string][]string{{"clerk": {}}},
	}, handler.RecordPostHogErasureEvidence)
	huma.Register(api, huma.Operation{
		OperationID: "retargetPostHogErasure", Method: http.MethodPost,
		Path:    "/api/v1/admin/posthog-erasure/{erasure_id}/retarget",
		Summary: "Retarget an unconfigured PostHog erasure", Tags: []string{"Admin"}, Security: []map[string][]string{{"clerk": {}}},
	}, handler.RetargetPostHogErasure)
	huma.Register(api, huma.Operation{
		OperationID: "replayPostHogErasure", Method: http.MethodPost,
		Path:    "/api/v1/admin/posthog-erasure/{erasure_id}/replay",
		Summary: "Replay a dead-lettered PostHog erasure", Tags: []string{"Admin"}, Security: []map[string][]string{{"clerk": {}}},
	}, handler.ReplayPostHogErasure)
}
