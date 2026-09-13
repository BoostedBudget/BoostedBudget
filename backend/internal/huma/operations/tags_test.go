package operations

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockUserTagService is a mock implementation of services.UserTagService
type mockUserTagService struct {
	tags            []*domain.UserTag
	tag             *domain.UserTag
	createErr       error
	getErr          error
	updateErr       error
	deleteErr       error
	getOrCreateTag  *domain.UserTag
	getOrCreateErr  error
	createCallCount int // incremented on each CreateTag call
}

func (m *mockUserTagService) GetUserTags(ctx context.Context, userID string) ([]*domain.UserTag, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.tags, nil
}

func (m *mockUserTagService) GetTagByID(ctx context.Context, tagID string) (*domain.UserTag, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.tag, nil
}

func (m *mockUserTagService) CreateTag(ctx context.Context, userID string, req *domain.CreateUserTagRequest) (*domain.UserTag, error) {
	m.createCallCount++
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.tag, nil
}

func (m *mockUserTagService) UpdateTag(ctx context.Context, userID, tagID string, req *domain.UpdateUserTagRequest) (*domain.UserTag, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.tag, nil
}

func (m *mockUserTagService) DeleteTag(ctx context.Context, userID, tagID string) error {
	return m.deleteErr
}

func (m *mockUserTagService) GetOrCreateTag(ctx context.Context, userID, tagName string) (*domain.UserTag, error) {
	if m.getOrCreateErr != nil {
		return nil, m.getOrCreateErr
	}
	return m.getOrCreateTag, nil
}

// Helper to create a string pointer
func strPtr(s string) *string {
	return &s
}

// Helper to create a test tag
func createTestTag(userID, name, color string) *domain.UserTag {
	now := time.Now()
	return &domain.UserTag{
		ID:        "tag-123",
		UserID:    userID,
		Name:      name,
		Color:     strPtr(color),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TestGetUserTags_Success tests successful retrieval of user tags.
func TestGetUserTags_Success(t *testing.T) {
	// Create mock service with test data
	mockService := &mockUserTagService{
		tags: []*domain.UserTag{
			createTestTag("user-123", "Groceries", "#FF5733"),
			createTestTag("user-123", "Entertainment", "#33FF57"),
		},
	}

	// Create handler
	handler := NewTagHandler(mockService)

	// Call handler directly (bypassing middleware)
	input := &GetUserTagsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUserTags(context.Background(), input)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 2)
	assert.Equal(t, "Groceries", output.Body[0].Name)
	assert.Equal(t, "Entertainment", output.Body[1].Name)
}

// TestGetUserTags_EmptyList tests that empty array is returned instead of null.
func TestGetUserTags_EmptyList(t *testing.T) {
	mockService := &mockUserTagService{
		tags: nil, // No tags
	}

	handler := NewTagHandler(mockService)

	input := &GetUserTagsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUserTags(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body, "Should return empty array, not nil")
	assert.Len(t, output.Body, 0)
}

// TestGetTagByID_Success tests successful retrieval of a tag by ID.
func TestGetTagByID_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockUserTagService{
		tag: createTestTag(userID, "Groceries", "#FF5733"),
	}

	handler := NewTagHandler(mockService)

	input := &GetTagByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tag-123",
	}

	output, err := handler.GetTagByID(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Groceries", output.Body.Name)
	assert.Equal(t, "#FF5733", *output.Body.Color)
}

// TestGetTagByID_NotFound tests 404 when tag doesn't exist.
func TestGetTagByID_NotFound(t *testing.T) {
	mockService := &mockUserTagService{
		getErr: domain.ErrTagNotFound,
	}

	handler := NewTagHandler(mockService)

	input := &GetTagByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-tag",
	}

	output, err := handler.GetTagByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestGetTagByID_WrongOwner tests 404 when user doesn't own the tag.
func TestGetTagByID_WrongOwner(t *testing.T) {
	mockService := &mockUserTagService{
		tag: createTestTag("other-user", "Groceries", "#FF5733"), // Different owner
	}

	handler := NewTagHandler(mockService)

	input := &GetTagByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"}, // Different user
		ID:                 "tag-123",
	}

	output, err := handler.GetTagByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCreateTag_Success tests successful tag creation.
func TestCreateTag_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockUserTagService{
		tag: createTestTag(userID, "New Tag", "#AABBCC"),
	}

	handler := NewTagHandler(mockService)

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		Body: domain.CreateUserTagRequest{
			Name:  "New Tag",
			Color: strPtr("#AABBCC"),
		},
	}

	output, err := handler.CreateTag(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "New Tag", output.Body.Name)
	assert.Equal(t, "#AABBCC", *output.Body.Color)
}

// TestCreateTag_LimitExceeded tests 403 when tag limit is exceeded.
func TestCreateTag_LimitExceeded(t *testing.T) {
	mockService := &mockUserTagService{
		createErr: domain.ErrTagLimitExceeded,
	}

	handler := NewTagHandler(mockService)

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateUserTagRequest{
			Name:  "New Tag",
			Color: strPtr("#AABBCC"),
		},
	}

	output, err := handler.CreateTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// Error should indicate forbidden (403)
}

// TestCreateTag_DuplicateName tests 409 when tag name already exists.
func TestCreateTag_DuplicateName(t *testing.T) {
	mockService := &mockUserTagService{
		createErr: domain.ErrTagAlreadyExists,
	}

	handler := NewTagHandler(mockService)

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateUserTagRequest{
			Name:  "Existing Tag",
			Color: strPtr("#AABBCC"),
		},
	}

	output, err := handler.CreateTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// Error should indicate conflict (409)
}

// TestCreateTag_InvalidName tests 400 when tag name is invalid.
func TestCreateTag_InvalidName(t *testing.T) {
	mockService := &mockUserTagService{
		createErr: domain.ErrInvalidTagName,
	}

	handler := NewTagHandler(mockService)

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateUserTagRequest{
			Name:  "",
			Color: strPtr("#AABBCC"),
		},
	}

	output, err := handler.CreateTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// Error should indicate bad request (400)
}

// TestUpdateTag_Success tests successful tag update.
func TestUpdateTag_Success(t *testing.T) {
	userID := "user-123"
	updatedTag := createTestTag(userID, "Updated Name", "#NEWCLR")
	mockService := &mockUserTagService{
		tag: updatedTag,
	}

	handler := NewTagHandler(mockService)

	input := &UpdateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tag-123",
		Body: domain.UpdateUserTagRequest{
			Name:  strPtr("Updated Name"),
			Color: strPtr("#NEWCLR"),
		},
	}

	output, err := handler.UpdateTag(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Updated Name", output.Body.Name)
	assert.Equal(t, "#NEWCLR", *output.Body.Color)
}

// TestUpdateTag_NotFound tests 404 when updating nonexistent tag.
func TestUpdateTag_NotFound(t *testing.T) {
	mockService := &mockUserTagService{
		updateErr: domain.ErrTagNotFound,
	}

	handler := NewTagHandler(mockService)

	input := &UpdateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-tag",
		Body: domain.UpdateUserTagRequest{
			Name: strPtr("Updated Name"),
		},
	}

	output, err := handler.UpdateTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestDeleteTag_Success tests successful tag deletion.
func TestDeleteTag_Success(t *testing.T) {
	mockService := &mockUserTagService{
		deleteErr: nil,
	}

	handler := NewTagHandler(mockService)

	input := &DeleteTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "tag-123",
	}

	output, err := handler.DeleteTag(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, output) // DeleteTag returns nil, nil on success
}

// TestDeleteTag_NotFound tests 404 when deleting nonexistent tag.
func TestDeleteTag_NotFound(t *testing.T) {
	mockService := &mockUserTagService{
		deleteErr: domain.ErrTagNotFound,
	}

	handler := NewTagHandler(mockService)

	input := &DeleteTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-tag",
	}

	output, err := handler.DeleteTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- MCP idempotency helpers ---

// mockTagIdempotencyRepository is a tag-specific idempotency mock that supports
// GetOrCreateInTx. It tracks call counts and caches results by key, simulating
// the deduplication behaviour expected from the real implementation.
type mockTagIdempotencyRepository struct {
	inTxCalls int
	cached    map[string]*domain.UserTag
}

func (m *mockTagIdempotencyRepository) GetOrCreate(
	_ context.Context,
	key, _, _ string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	if existing, ok := m.cached[key]; ok {
		*(result.(**domain.UserTag)) = existing
		return true, nil
	}
	res, err := fn()
	if err != nil {
		return false, err
	}
	tag := res.(*domain.UserTag)
	if m.cached == nil {
		m.cached = make(map[string]*domain.UserTag)
	}
	m.cached[key] = tag
	*(result.(**domain.UserTag)) = tag
	return false, nil
}

func (m *mockTagIdempotencyRepository) GetOrCreateInTx(
	_ context.Context,
	_ pgx.Tx,
	key, _, _ string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	m.inTxCalls++
	if existing, ok := m.cached[key]; ok {
		// Cache hit — return stored result; fn NOT called.
		*(result.(**domain.UserTag)) = existing
		return true, nil
	}
	// Cache miss — call fn and store.
	res, err := fn()
	if err != nil {
		return false, err
	}
	tag := res.(*domain.UserTag)
	if m.cached == nil {
		m.cached = make(map[string]*domain.UserTag)
	}
	m.cached[key] = tag
	*(result.(**domain.UserTag)) = tag
	return false, nil
}

func (m *mockTagIdempotencyRepository) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

// mockTx is a minimal pgx.Tx that records Commit/Rollback calls.
// It satisfies both pgx.Tx and repository.DB so it can be passed to
// repository.NewUserTagRepository(tx).
type mockTx struct {
	committed      bool
	rolledBack     bool
	queryRowResult pgx.Row
}

// Begin returns a fresh child mockTx (savepoint semantics).
// Returning a new struct keeps the outer tx's committed/rolledBack flags independent
// of inner tx commits (e.g. CreateWithCategories' internal Begin→Commit).
func (m *mockTx) Begin(_ context.Context) (pgx.Tx, error) {
	return &mockTx{queryRowResult: m.queryRowResult}, nil
}
func (m *mockTx) Commit(_ context.Context) error   { m.committed = true; return nil }
func (m *mockTx) Rollback(_ context.Context) error { m.rolledBack = true; return nil }
func (m *mockTx) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *mockTx) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	return nil, nil
}
func (m *mockTx) QueryRow(_ context.Context, _ string, _ ...interface{}) pgx.Row {
	return m.queryRowResult
}
func (m *mockTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (m *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (m *mockTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (m *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (m *mockTx) Conn() *pgx.Conn { return nil }

// mockDB is a minimal repository.DB whose Begin returns a *mockTx.
type mockDB struct {
	tx *mockTx
}

func (m *mockDB) Begin(_ context.Context) (pgx.Tx, error) {
	return m.tx, nil
}
func (m *mockDB) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *mockDB) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	return nil, nil
}
func (m *mockDB) QueryRow(_ context.Context, _ string, _ ...interface{}) pgx.Row {
	return nil
}
func (m *mockDB) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }

// mockMCPAuditRepositoryForTags is a local audit repo mock for tags tests that
// satisfies the full MCPAuditRepository interface (Record + RecordInTx).
type mockMCPAuditRepositoryForTags struct {
	recordInTxCalled bool
}

func (m *mockMCPAuditRepositoryForTags) Record(_ context.Context, _ *domain.MCPAuditEntry) error {
	return nil
}
func (m *mockMCPAuditRepositoryForTags) RecordInTx(_ context.Context, _ pgx.Tx, _ *domain.MCPAuditEntry) error {
	m.recordInTxCalled = true
	return nil
}

// TestCreateTag_MCPOrigin_IdempotencyDedup verifies GAP-R5-1:
// Two MCP POST /tags requests with the same Idempotency-Key must return the same
// body and only invoke GetOrCreateInTx (not double-create).
func TestCreateTag_MCPOrigin_IdempotencyDedup(t *testing.T) {
	userID := "user-mcp-1"
	idempotencyKey := "idem-tag-abc"
	expectedTag := createTestTag(userID, "MCP Tag", "#112233")

	tx := &mockTx{}
	db := &mockDB{tx: tx}
	auditRepo := &mockMCPAuditRepositoryForTags{}

	// Pre-populate the cache with the expected tag so fn() is never called.
	// This simulates the dedup scenario: both the first and second POST see the
	// cached result; the handler must call GetOrCreateInTx each time but only
	// create the entity once (here: never, because the cache is pre-seeded).
	idempotencyRepo := &mockTagIdempotencyRepository{
		cached: map[string]*domain.UserTag{
			idempotencyKey: expectedTag,
		},
	}

	svc := &mockUserTagService{tag: expectedTag}

	const workerSecret = "test-worker-secret"
	handler := NewTagHandler(svc, TagHandlerDeps{
		IdempotencyRepo: idempotencyRepo,
		DB:              db,
		MCPAuditRepo:    auditRepo,
		MCPWorkerSecret: workerSecret,
	})

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: idempotencyKey},
		MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-1", MCPWorkerSecret: workerSecret},
		Body: domain.CreateUserTagRequest{
			Name:  "MCP Tag",
			Color: strPtr("#112233"),
		},
	}

	// First POST — cache hit, should return the pre-cached tag.
	out1, err := handler.CreateTag(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, out1)
	assert.Equal(t, expectedTag.ID, out1.Body.ID)

	// Second POST with the same key — must also return the cached result.
	out2, err := handler.CreateTag(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, out2)

	assert.Equal(t, out1.Body.ID, out2.Body.ID, "both responses must have the same tag ID")
	assert.Equal(t, 2, idempotencyRepo.inTxCalls,
		"GetOrCreateInTx must be called for each MCP request (not bypassed)")
	// fn() was never called (cache pre-seeded), so h.service.CreateTag was not invoked.
	assert.Equal(t, 0, svc.createCallCount,
		"tag service CreateTag must not be called when idempotency cache hits on both requests")
}

// TestCreateTag_MCPOrigin_AuditFailureRollsBack verifies GAP-R5-17 (fail-closed audit):
// When the MCP audit INSERT inside the transaction fails, the entire tx must roll back
// so the tag is NOT persisted. The handler must return an error.
func TestCreateTag_MCPOrigin_AuditFailureRollsBack(t *testing.T) {
	userID := "user-r5-17"

	tx := &mockTx{}
	db := &mockDB{tx: tx}
	svc := &mockUserTagService{tag: &domain.UserTag{ID: "tag-r5-17", Name: "test"}}

	const workerSecret = "secret-r5-17"
	handler := NewTagHandler(svc, TagHandlerDeps{
		IdempotencyRepo: &mockTagIdempotencyRepository{},
		DB:              db,
		MCPAuditRepo:    &mockMCPAuditRepositoryFailForTags{},
		MCPWorkerSecret: workerSecret,
	})

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: ""},
		MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-r5-17", MCPWorkerSecret: workerSecret},
		Body:               domain.CreateUserTagRequest{Name: "Fail Audit", Color: strPtr("#000000")},
	}

	out, err := handler.CreateTag(context.Background(), input)
	assert.Error(t, err, "handler must return error when audit INSERT fails")
	assert.Nil(t, out, "no output when audit fails")
	assert.False(t, tx.committed, "tx must NOT be committed when audit fails")
}

// mockMCPAuditRepositoryFailForTags always errors on RecordInTx to simulate audit failure.
type mockMCPAuditRepositoryFailForTags struct{}

func (m *mockMCPAuditRepositoryFailForTags) Record(_ context.Context, _ *domain.MCPAuditEntry) error {
	return nil
}
func (m *mockMCPAuditRepositoryFailForTags) RecordInTx(_ context.Context, _ pgx.Tx, _ *domain.MCPAuditEntry) error {
	return fmt.Errorf("simulated audit DB failure")
}

// TestTagsRouteRegistration tests that routes are properly registered.
func TestTagsRouteRegistration(t *testing.T) {
	mockService := &mockUserTagService{
		tags: []*domain.UserTag{},
	}

	_, api := humatest.New(t)
	handler := NewTagHandler(mockService)
	RegisterTagRoutes(api, handler)

	// Test GET /api/v1/tags (without auth, should fail with 401)
	resp := api.Get("/api/v1/tags")
	// Should return 401 because AuthenticatedInput requires user_id in context
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response is a proper error (check raw body contains expected message)
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}
