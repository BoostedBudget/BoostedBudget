package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCategorizationRuleService is a mock implementation of services.CategorizationRuleService
type mockCategorizationRuleService struct {
	rules         []*domain.CategorizationRule
	rule          *domain.CategorizationRule
	createErr     error
	getErr        error
	updateErr     error
	deleteErr     error
	toggleErr     error
	previewResult *domain.RulePreviewResult
	previewErr    error
}

func (m *mockCategorizationRuleService) GetUserRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.rules, nil
}

func (m *mockCategorizationRuleService) GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.CategorizationRule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.rule, nil
}

func (m *mockCategorizationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.rule, nil
}

func (m *mockCategorizationRuleService) UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateCategorizationRuleRequest) (*domain.CategorizationRule, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.rule, nil
}

func (m *mockCategorizationRuleService) DeleteRule(ctx context.Context, userID, ruleID string) error {
	return m.deleteErr
}

func (m *mockCategorizationRuleService) ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.CategorizationRule, error) {
	if m.toggleErr != nil {
		return nil, m.toggleErr
	}
	// Update the enabled status in the mock
	if m.rule != nil {
		m.rule.Enabled = enabled
	}
	return m.rule, nil
}

func (m *mockCategorizationRuleService) GetEnabledRules(ctx context.Context, userID string) ([]*domain.CategorizationRule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	var enabledRules []*domain.CategorizationRule
	for _, r := range m.rules {
		if r.Enabled {
			enabledRules = append(enabledRules, r)
		}
	}
	return enabledRules, nil
}

// Helper to create a test rule
func createTestRule(userID, name string) *domain.CategorizationRule {
	now := time.Now()
	return &domain.CategorizationRule{
		ID:             "rule-123",
		UserID:         userID,
		Name:           name,
		Enabled:        true,
		Priority:       1,
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "grocery"}},
		ConditionLogic: "AND",
		TargetTags:     []string{"groceries"},
		TagBehavior:    "add",
		Source:         "manual",
		MatchCount:     0,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// TestGetUserRules_Success tests successful retrieval of user rules.
func TestGetUserRules_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCategorizationRuleService{
		rules: []*domain.CategorizationRule{
			createTestRule(userID, "Grocery Rule"),
			createTestRule(userID, "Entertainment Rule"),
		},
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &GetUserRulesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	output, err := handler.GetUserRules(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 2)
	assert.Equal(t, "Grocery Rule", output.Body[0].Name)
	assert.Equal(t, "Entertainment Rule", output.Body[1].Name)
}

// TestGetUserRules_EmptyList tests that empty array is returned instead of null.
func TestGetUserRules_EmptyList(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		rules: nil,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &GetUserRulesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUserRules(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body, "Should return empty array, not nil")
	assert.Len(t, output.Body, 0)
}

// TestGetRuleByID_Success tests successful retrieval of a rule by ID.
func TestGetRuleByID_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCategorizationRuleService{
		rule: createTestRule(userID, "Grocery Rule"),
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &GetRuleByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "rule-123",
	}

	output, err := handler.GetRuleByID(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Grocery Rule", output.Body.Name)
	assert.True(t, output.Body.Enabled)
}

// TestGetRuleByID_NotFound tests 404 when rule doesn't exist.
func TestGetRuleByID_NotFound(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		getErr: domain.ErrRuleNotFound,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &GetRuleByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-rule",
	}

	output, err := handler.GetRuleByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCreateRule_Success tests successful rule creation.
func TestCreateRule_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockCategorizationRuleService{
		rule: createTestRule(userID, "New Rule"),
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &CreateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		Body: domain.CreateCategorizationRuleRequest{
			Name:       "New Rule",
			Conditions: []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
			TargetTags: []string{"test"},
		},
	}

	output, err := handler.CreateRule(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "New Rule", output.Body.Name)
}

// TestCreateRule_LimitExceeded tests 403 when rule limit is exceeded.
func TestCreateRule_LimitExceeded(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		createErr: domain.ErrRuleLimitExceeded,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &CreateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateCategorizationRuleRequest{
			Name:       "New Rule",
			Conditions: []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
			TargetTags: []string{"test"},
		},
	}

	output, err := handler.CreateRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestCreateRule_DuplicateName tests 409 when rule name already exists.
func TestCreateRule_DuplicateName(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		createErr: domain.ErrRuleAlreadyExists,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &CreateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateCategorizationRuleRequest{
			Name:       "Existing Rule",
			Conditions: []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "test"}},
			TargetTags: []string{"test"},
		},
	}

	output, err := handler.CreateRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// PreviewRule satisfies the service interface for the handler tests (#1625).
func (m *mockCategorizationRuleService) PreviewRule(
	_ context.Context, _ string, _ *domain.CategorizationRule, _ int,
) (*domain.RulePreviewResult, error) {
	if m.previewErr != nil {
		return nil, m.previewErr
	}
	if m.previewResult != nil {
		return m.previewResult, nil
	}
	return &domain.RulePreviewResult{}, nil
}

// TestCreateRule_ValidationError tests 400 for validation errors.
//
// #1625: the mock now returns the SENTINEL-WRAPPED error the real service returns. It used to
// return a bare errors.New, which passed only because the handler exact-string-matched three
// literal messages to 400. That allowlist is gone — classification lives in the error mapper —
// so a bare error here would (correctly) map to 500, exactly as an unwrapped service error
// should.
func TestCreateRule_ValidationError(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		createErr: fmt.Errorf("%w: name is required", domain.ErrInvalidRule),
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &CreateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateCategorizationRuleRequest{
			Name:       "",
			Conditions: []domain.RuleCondition{},
		},
	}

	output, err := handler.CreateRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

// TestUpdateRule_Success tests successful rule update.
func TestUpdateRule_Success(t *testing.T) {
	userID := "user-123"
	updatedRule := createTestRule(userID, "Updated Name")
	mockService := &mockCategorizationRuleService{
		rule: updatedRule,
	}

	handler := NewCategorizationRuleHandler(mockService)

	newName := "Updated Name"
	input := &UpdateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "rule-123",
		Body: domain.UpdateCategorizationRuleRequest{
			Name: &newName,
		},
	}

	output, err := handler.UpdateRule(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Updated Name", output.Body.Name)
}

// TestUpdateRule_NotFound tests 404 when updating nonexistent rule.
func TestUpdateRule_NotFound(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		updateErr: domain.ErrRuleNotFound,
	}

	handler := NewCategorizationRuleHandler(mockService)

	newName := "Updated Name"
	input := &UpdateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-rule",
		Body: domain.UpdateCategorizationRuleRequest{
			Name: &newName,
		},
	}

	output, err := handler.UpdateRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestDeleteRule_Success tests successful rule deletion.
func TestDeleteRule_Success(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		deleteErr: nil,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &DeleteRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "rule-123",
	}

	output, err := handler.DeleteRule(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, output)
}

// TestDeleteRule_NotFound tests 404 when deleting nonexistent rule.
func TestDeleteRule_NotFound(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		deleteErr: domain.ErrRuleNotFound,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &DeleteRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-rule",
	}

	output, err := handler.DeleteRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestToggleRule_Success tests successful rule toggle.
func TestToggleRule_Success(t *testing.T) {
	userID := "user-123"
	rule := createTestRule(userID, "Test Rule")
	rule.Enabled = true
	mockService := &mockCategorizationRuleService{
		rule: rule,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &ToggleRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "rule-123",
		Body: domain.ToggleCategorizationRuleRequest{
			Enabled: false,
		},
	}

	output, err := handler.ToggleRule(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.Enabled)
}

// TestToggleRule_NotFound tests 404 when toggling nonexistent rule.
func TestToggleRule_NotFound(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		toggleErr: domain.ErrRuleNotFound,
	}

	handler := NewCategorizationRuleHandler(mockService)

	input := &ToggleRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-rule",
		Body: domain.ToggleCategorizationRuleRequest{
			Enabled: true,
		},
	}

	output, err := handler.ToggleRule(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCategorizationRuleRouteRegistration tests that routes are properly registered.
func TestCategorizationRuleRouteRegistration(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		rules: []*domain.CategorizationRule{},
	}

	_, api := humatest.New(t)
	handler := NewCategorizationRuleHandler(mockService)
	RegisterCategorizationRuleRoutes(api, handler)

	// Test GET /api/v1/auto-categorization (without auth, should fail with 401)
	resp := api.Get("/api/v1/auto-categorization")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestCategorizationRuleResponseJSON tests JSON serialization of response.
func TestCategorizationRuleResponseJSON(t *testing.T) {
	mockService := &mockCategorizationRuleService{
		rules: []*domain.CategorizationRule{},
	}

	_, api := humatest.New(t)
	handler := NewCategorizationRuleHandler(mockService)
	RegisterCategorizationRuleRoutes(api, handler)

	resp := api.Get("/api/v1/auto-categorization")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}

// TestRuleValidationNeverReturns5xx is the anti-Sentry-noise proof at the HTTP boundary, and it
// covers BOTH handlers. Codex gap analysis A1: the existing tests assert error TEXT, which would
// still pass if the status regressed to 500 — and a 500 is exactly what these used to be, with
// sentry.CaptureException firing on every one of them (mapper.go only reports at >= 500).
//
// The handler has no allowlist any more; classification lives entirely in the error mapper, so a
// new Validate() failure that forgets to wrap a sentinel silently becomes a 500 again. This is
// the test that catches that.
func TestRuleValidationNeverReturns5xx(t *testing.T) {
	invalidBodies := []struct {
		name string
		body domain.CreateCategorizationRuleRequest
	}{
		{"unknown condition type (the reported payload)", domain.CreateCategorizationRuleRequest{
			Name:       "r",
			Conditions: []domain.RuleCondition{{Type: "amount", Value: 1000}},
			TargetKind: strPtrOps("clear"),
		}},
		{"unknown field", domain.CreateCategorizationRuleRequest{
			Name:       "r",
			Conditions: []domain.RuleCondition{{Field: "nope", Operator: "eq", Value: "x"}},
			TargetKind: strPtrOps("clear"),
		}},
		{"operator the field does not accept", domain.CreateCategorizationRuleRequest{
			Name:       "r",
			Conditions: []domain.RuleCondition{{Field: domain.FieldMerchant, Operator: "gt", Value: 5}},
			TargetKind: strPtrOps("clear"),
		}},
		{"value shape the evaluator can never use", domain.CreateCategorizationRuleRequest{
			Name:       "r",
			Conditions: []domain.RuleCondition{{Field: domain.FieldAmount, Operator: "gt", Value: "abc"}},
			TargetKind: strPtrOps("clear"),
		}},
		{"type contradicting field+operator", domain.CreateCategorizationRuleRequest{
			Name: "r",
			Conditions: []domain.RuleCondition{
				{Type: domain.ConditionAmountEquals, Field: domain.FieldMerchant, Operator: "contains", Value: "x"},
			},
			TargetKind: strPtrOps("clear"),
		}},
		{"missing name", domain.CreateCategorizationRuleRequest{
			Conditions: []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "x"}},
			TargetKind: strPtrOps("clear"),
		}},
		{"no conditions", domain.CreateCategorizationRuleRequest{
			Name:       "r",
			Conditions: []domain.RuleCondition{},
			TargetKind: strPtrOps("clear"),
		}},
		{"no action at all", domain.CreateCategorizationRuleRequest{
			Name:       "r",
			Conditions: []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "x"}},
		}},
	}

	// UpdateRule validates independently and, before #1625, had NO 400 mapping whatsoever — every
	// update validation failure was a 500. Codex review: the first version of this test only
	// exercised CreateRule while the commit message claimed both, so the handler that was
	// strictly worse had no coverage at all.
	for _, tt := range invalidBodies {
		// "no action at all" cannot be produced by THIS fixture through update: the request
		// merges into a loaded rule that already carries a target, and a nil TargetKind means
		// "leave it alone" rather than "remove it". It IS reachable by a different route —
		// emptying the tags of a tag-only rule — which TestUpdateRule_EmptyingTagsOnATagOnlyRule
		// covers below. Codex review: the earlier version of this comment claimed the case was
		// unreachable through update at all, which was wrong.
		if tt.name == "no action at all" {
			continue
		}
		t.Run("update/"+tt.name, func(t *testing.T) {
			// UpdateRule LOADS the existing rule before validating — it has to, since it merges
			// the request into it — so unlike the create path it cannot run against a nil
			// repository. This stub supplies an owned rule and nothing else; validation is still
			// the real domain code.
			svc := services.NewCategorizationRuleService(
				&stubRuleRepoForValidation{owner: "user-123"}, nil)
			handler := NewCategorizationRuleHandler(svc)

			conds := tt.body.Conditions
			_, err := handler.UpdateRule(context.Background(), &UpdateRuleInput{
				AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
				ID:                 "rule-1",
				Body: domain.UpdateCategorizationRuleRequest{
					Name:       &tt.body.Name,
					Conditions: &conds,
					TargetKind: tt.body.TargetKind,
				},
			})

			require.Error(t, err)
			assertClientError(t, err)
		})
	}

	for _, tt := range invalidBodies {
		t.Run("create/"+tt.name, func(t *testing.T) {
			// The REAL service and domain validation — a mock would only prove the mock's shape.
			svc := services.NewCategorizationRuleService(nil, nil)
			handler := NewCategorizationRuleHandler(svc)

			_, err := handler.CreateRule(context.Background(), &CreateRuleInput{
				AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
				Body:               tt.body,
			})

			require.Error(t, err)
			assertClientError(t, err)
		})
	}
}

// assertClientError fails when the mapped HTTP status is >= 500. A 5xx here means the error
// reached the mapper's default branch, which also fires sentry.CaptureException — user input
// mistakes logged as server errors.
func assertClientError(t *testing.T, err error) {
	t.Helper()
	var statusErr interface{ GetStatus() int }
	if !errors.As(err, &statusErr) {
		t.Fatalf("error carries no HTTP status: %v", err)
	}
	status := statusErr.GetStatus()
	assert.Less(t, status, 500,
		"validation failure mapped to %d — it must be a 4xx, or client input pollutes Sentry as a server error: %v",
		status, err)
	assert.GreaterOrEqual(t, status, 400, "expected a client error, got %d", status)
}

func strPtrOps(s string) *string { return &s }

// stubRuleRepoForValidation returns an owned rule from GetByID so UpdateRule reaches its
// validation step. Every other method is unreachable in these tests and fails loudly if that
// assumption ever stops holding.
type stubRuleRepoForValidation struct {
	owner string
}

func (s *stubRuleRepoForValidation) GetByID(_ context.Context, id string) (*domain.CategorizationRule, error) {
	return &domain.CategorizationRule{
		ID:             id,
		UserID:         s.owner,
		Name:           "existing",
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "x"}},
		ConditionLogic: "AND",
		TargetKind:     strPtrOps("clear"),
		TagBehavior:    "add",
		Source:         "manual",
	}, nil
}

func (s *stubRuleRepoForValidation) Create(context.Context, *domain.CategorizationRule) error {
	panic("Create must not be reached: these inputs are all invalid")
}
func (s *stubRuleRepoForValidation) Update(context.Context, *domain.CategorizationRule) error {
	panic("Update must not be reached: these inputs are all invalid")
}
func (s *stubRuleRepoForValidation) GetByUserID(context.Context, string) ([]*domain.CategorizationRule, error) {
	return nil, nil
}
func (s *stubRuleRepoForValidation) GetEnabledByUserID(context.Context, string) ([]*domain.CategorizationRule, error) {
	return nil, nil
}
func (s *stubRuleRepoForValidation) GetByName(context.Context, string, string) (*domain.CategorizationRule, error) {
	return nil, nil
}
func (s *stubRuleRepoForValidation) Delete(context.Context, string) error { return nil }
func (s *stubRuleRepoForValidation) CountByUserID(context.Context, string) (int, error) {
	return 0, nil
}
func (s *stubRuleRepoForValidation) IncrementMatchCount(context.Context, string) error    { return nil }
func (s *stubRuleRepoForValidation) IncrementOverrideCount(context.Context, string) error { return nil }
func (s *stubRuleRepoForValidation) ListAllForQuarantineCensus(context.Context) ([]*domain.CategorizationRule, error) {
	return nil, nil
}

// TestUpdateRule_EmptyingTagsOnATagOnlyRule covers the update-specific way to end up with a rule
// that does nothing: a tag-only rule (no target_kind) whose tags are replaced with an empty
// list. Both HasCategoryAction and HasTagAction are then false, so Validate must reject it — and
// as a 4xx, not the 500 every update validation failure used to be.
func TestUpdateRule_EmptyingTagsOnATagOnlyRule(t *testing.T) {
	svc := services.NewCategorizationRuleService(
		&stubTagOnlyRuleRepo{owner: "user-123"}, nil)
	handler := NewCategorizationRuleHandler(svc)

	emptyTags := []string{}
	_, err := handler.UpdateRule(context.Background(), &UpdateRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "rule-1",
		Body:               domain.UpdateCategorizationRuleRequest{TargetTags: &emptyTags},
	})

	require.Error(t, err, "a rule with neither a category action nor tags must be rejected")
	assertClientError(t, err)
}

// stubTagOnlyRuleRepo returns a rule whose ONLY action is its tags.
type stubTagOnlyRuleRepo struct{ owner string }

func (s *stubTagOnlyRuleRepo) GetByID(_ context.Context, id string) (*domain.CategorizationRule, error) {
	return &domain.CategorizationRule{
		ID:             id,
		UserID:         s.owner,
		Name:           "tag only",
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "x"}},
		ConditionLogic: "AND",
		TargetTags:     []string{"coffee"},
		TagBehavior:    "add",
		Source:         "manual",
	}, nil
}

func (s *stubTagOnlyRuleRepo) Create(context.Context, *domain.CategorizationRule) error {
	panic("Create must not be reached")
}
func (s *stubTagOnlyRuleRepo) Update(context.Context, *domain.CategorizationRule) error {
	panic("Update must not be reached: the merged rule is invalid")
}
func (s *stubTagOnlyRuleRepo) GetByUserID(context.Context, string) ([]*domain.CategorizationRule, error) {
	return nil, nil
}
func (s *stubTagOnlyRuleRepo) GetEnabledByUserID(context.Context, string) ([]*domain.CategorizationRule, error) {
	return nil, nil
}
func (s *stubTagOnlyRuleRepo) GetByName(context.Context, string, string) (*domain.CategorizationRule, error) {
	return nil, nil
}
func (s *stubTagOnlyRuleRepo) Delete(context.Context, string) error                 { return nil }
func (s *stubTagOnlyRuleRepo) CountByUserID(context.Context, string) (int, error)   { return 0, nil }
func (s *stubTagOnlyRuleRepo) IncrementMatchCount(context.Context, string) error    { return nil }
func (s *stubTagOnlyRuleRepo) IncrementOverrideCount(context.Context, string) error { return nil }
func (s *stubTagOnlyRuleRepo) ListAllForQuarantineCensus(context.Context) ([]*domain.CategorizationRule, error) {
	return nil, nil
}

// N2 (#1625): the literal /condition-schema route must actually be REACHABLE.
//
// It was registered 49 lines AFTER GET /{id} while its comment claimed the opposite, so humatest —
// which is registration-order sensitive — dispatched /condition-schema to GetRuleByID. Production's
// Echo router prefers the literal regardless, so this was never a production bug; it meant the
// endpoint was untestable, and no route test called it. That is how it stayed wrong.
//
// Asserting on the BODY, not just the status: both handlers 401 unauthenticated, so a status-only
// assertion passes under the very misrouting this test exists to catch.
func TestConditionSchemaRoute_IsNotShadowedByTheIDRoute(t *testing.T) {
	mockService := &mockCategorizationRuleService{rules: []*domain.CategorizationRule{}}

	// Both handlers 401 unauthenticated, so drive it with an authenticated context and check WHICH
	// handler answered — same technique the deleted-goals route test uses for this bug class.
	authed := func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, inputs.UserIDKey, "user-123"))
	}
	_, api := humatest.New(t)
	api.UseMiddleware(authed)
	handler := NewCategorizationRuleHandler(mockService)
	RegisterCategorizationRuleRoutes(api, handler)

	resp := api.Get("/api/v1/auto-categorization/condition-schema")

	// GetRuleByID would look up a rule whose id is the literal "condition-schema" and 404.
	require.Equal(t, http.StatusOK, resp.Code,
		"the literal route is shadowed by /{id} — body %s", resp.Body.String())
	assert.Contains(t, resp.Body.String(), "signed_amount",
		"response did not come from the schema handler")
}
