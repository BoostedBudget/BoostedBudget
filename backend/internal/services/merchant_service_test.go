package services

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mocks
// ============================================================================

type mockMerchantRepo struct {
	mock.Mock
}

func (m *mockMerchantRepo) GetOrCreateMerchants(ctx context.Context, names []string) (map[string]string, error) {
	args := m.Called(ctx, names)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *mockMerchantRepo) GetOrCreateMerchantsWithNormalize(ctx context.Context, names []string, normalize func(string) string) (map[string]string, error) {
	args := m.Called(ctx, names, normalize)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *mockMerchantRepo) ListMerchants(ctx context.Context, userID string) ([]domain.MerchantSummary, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.MerchantSummary), args.Error(1)
}

func (m *mockMerchantRepo) GetDuplicateSuggestions(ctx context.Context, userID string, threshold float64) ([][]domain.MerchantSummary, error) {
	args := m.Called(ctx, userID, threshold)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([][]domain.MerchantSummary), args.Error(1)
}

type mockMerchantOverrideRepo struct {
	mock.Mock
}

func (m *mockMerchantOverrideRepo) Upsert(ctx context.Context, override domain.MerchantNameOverride) (*domain.MerchantNameOverride, error) {
	args := m.Called(ctx, override)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.MerchantNameOverride), args.Error(1)
}

func (m *mockMerchantOverrideRepo) BulkUpsert(ctx context.Context, overrides []domain.MerchantNameOverride) ([]domain.MerchantNameOverride, error) {
	args := m.Called(ctx, overrides)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.MerchantNameOverride), args.Error(1)
}

func (m *mockMerchantOverrideRepo) GetByUserID(ctx context.Context, userID string) ([]domain.MerchantNameOverride, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.MerchantNameOverride), args.Error(1)
}

func (m *mockMerchantOverrideRepo) Delete(ctx context.Context, userID, merchantID string) error {
	args := m.Called(ctx, userID, merchantID)
	return args.Error(0)
}

func (m *mockMerchantOverrideRepo) GetMergeTarget(ctx context.Context, userID, merchantID string) (string, error) {
	args := m.Called(ctx, userID, merchantID)
	return args.String(0), args.Error(1)
}

func (m *mockMerchantOverrideRepo) ReassignMergeTarget(ctx context.Context, userID, oldTargetID, newTargetID string) error {
	args := m.Called(ctx, userID, oldTargetID, newTargetID)
	return args.Error(0)
}

// ============================================================================
// Helpers
// ============================================================================

func strPtrM(s string) *string { return &s }

func newMerchantService(mr *mockMerchantRepo, mor *mockMerchantOverrideRepo) *MerchantService {
	return NewMerchantService(mr, mor)
}

// ============================================================================
// ListMerchants
// ============================================================================

func TestMerchantService_ListMerchants_Success(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"

	expected := []domain.MerchantSummary{
		{MerchantID: "m1", NormalizedName: "Starbucks", TransactionCount: 5},
		{MerchantID: "m2", NormalizedName: "Amazon", TransactionCount: 12},
	}
	mr.On("ListMerchants", ctx, userID).Return(expected, nil)

	result, err := svc.ListMerchants(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	mr.AssertExpectations(t)
}

func TestMerchantService_ListMerchants_RepoError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"

	mr.On("ListMerchants", ctx, userID).Return(nil, errors.New("db error"))

	result, err := svc.ListMerchants(ctx, userID)

	require.Error(t, err)
	assert.Nil(t, result)
	mr.AssertExpectations(t)
}

// ============================================================================
// GetDuplicateSuggestions
// ============================================================================

func TestMerchantService_GetDuplicateSuggestions_UsesDefaultThreshold(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"

	groups := [][]domain.MerchantSummary{
		{{MerchantID: "m1"}, {MerchantID: "m2"}},
	}
	// Must be called with the default threshold 0.6
	mr.On("GetDuplicateSuggestions", ctx, userID, 0.6).Return(groups, nil)

	result, err := svc.GetDuplicateSuggestions(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, groups, result)
	mr.AssertExpectations(t)
}

func TestMerchantService_GetDuplicateSuggestions_RepoError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	mr.On("GetDuplicateSuggestions", ctx, "user-1", 0.6).Return(nil, errors.New("db error"))

	result, err := svc.GetDuplicateSuggestions(ctx, "user-1")

	require.Error(t, err)
	assert.Nil(t, result)
	mr.AssertExpectations(t)
}

// ============================================================================
// SetOverride
// ============================================================================

func TestMerchantService_SetOverride_DisplayName(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m1"
	displayName := "Starbucks"

	expected := &domain.MerchantNameOverride{
		ID:          "ov-1",
		UserID:      userID,
		MerchantID:  merchantID,
		DisplayName: &displayName,
	}
	mor.On("Upsert", ctx, mock.MatchedBy(func(o domain.MerchantNameOverride) bool {
		return o.UserID == userID && o.MerchantID == merchantID && o.DisplayName != nil && *o.DisplayName == displayName
	})).Return(expected, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, &displayName, nil)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	mor.AssertExpectations(t)
}

func TestMerchantService_SetOverride_MergedInto(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m1"
	targetID := "m2"

	expected := &domain.MerchantNameOverride{
		ID:                   "ov-2",
		UserID:               userID,
		MerchantID:           merchantID,
		MergedIntoMerchantID: &targetID,
	}
	mor.On("Upsert", ctx, mock.MatchedBy(func(o domain.MerchantNameOverride) bool {
		return o.UserID == userID && o.MerchantID == merchantID && o.MergedIntoMerchantID != nil && *o.MergedIntoMerchantID == targetID
	})).Return(expected, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, nil, &targetID)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	mor.AssertExpectations(t)
}

func TestMerchantService_SetOverride_NeitherFieldSet_ReturnsError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	result, err := svc.SetOverride(ctx, "user-1", "m1", nil, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	mor.AssertNotCalled(t, "Upsert")
}

func TestMerchantService_SetOverride_RepoError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	displayName := "Foo"

	mor.On("Upsert", ctx, mock.Anything).Return(nil, errors.New("db error"))

	result, err := svc.SetOverride(ctx, "user-1", "m1", &displayName, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	mor.AssertExpectations(t)
}

// ============================================================================
// ClearOverride
// ============================================================================

func TestMerchantService_ClearOverride_Success(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	mor.On("Delete", ctx, "user-1", "m1").Return(nil)

	err := svc.ClearOverride(ctx, "user-1", "m1")

	require.NoError(t, err)
	mor.AssertExpectations(t)
}

func TestMerchantService_ClearOverride_RepoError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	mor.On("Delete", ctx, "user-1", "m1").Return(errors.New("not found"))

	err := svc.ClearOverride(ctx, "user-1", "m1")

	require.Error(t, err)
	mor.AssertExpectations(t)
}

// ============================================================================
// BulkSetOverrides
// ============================================================================

func TestMerchantService_BulkSetOverrides_Success(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"

	dn1 := "Starbucks"
	dn2 := "Amazon Prime"
	reqs := []SetOverrideRequest{
		{MerchantID: "m1", DisplayName: &dn1},
		{MerchantID: "m2", DisplayName: &dn2},
	}

	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m1", DisplayName: &dn1},
		{ID: "ov-2", UserID: userID, MerchantID: "m2", DisplayName: &dn2},
	}

	mor.On("BulkUpsert", ctx, mock.MatchedBy(func(overrides []domain.MerchantNameOverride) bool {
		return len(overrides) == 2 && overrides[0].MerchantID == "m1" && overrides[1].MerchantID == "m2"
	})).Return(expected, nil)

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	mor.AssertExpectations(t)
}

func TestMerchantService_BulkSetOverrides_InvalidEntry_ReturnsError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	// One valid, one with neither field set
	dn := "Foo"
	reqs := []SetOverrideRequest{
		{MerchantID: "m1", DisplayName: &dn},
		{MerchantID: "m2"}, // invalid: no display_name, no merged_into
	}

	result, err := svc.BulkSetOverrides(ctx, "user-1", reqs)

	require.Error(t, err)
	assert.Nil(t, result)
	mor.AssertNotCalled(t, "BulkUpsert")
}

func TestMerchantService_BulkSetOverrides_EmptySlice_ReturnsEmpty(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	result, err := svc.BulkSetOverrides(ctx, "user-1", nil)

	require.NoError(t, err)
	assert.Empty(t, result)
	mor.AssertNotCalled(t, "BulkUpsert")
}

// ============================================================================
// MergeMerchants
// ============================================================================

func TestMerchantService_MergeMerchants_Success(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	targetID := "m-target"
	sourceIDs := []string{"m1", "m2"}

	targetPtr1 := targetID
	targetPtr2 := targetID
	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m1", MergedIntoMerchantID: &targetPtr1},
		{ID: "ov-2", UserID: userID, MerchantID: "m2", MergedIntoMerchantID: &targetPtr2},
	}

	// target has no existing merge pointer — canonical is itself.
	mor.On("GetMergeTarget", ctx, userID, targetID).Return("", nil)
	mor.On("BulkUpsert", ctx, mock.MatchedBy(func(overrides []domain.MerchantNameOverride) bool {
		if len(overrides) != 2 {
			return false
		}
		for _, o := range overrides {
			if o.MergedIntoMerchantID == nil || *o.MergedIntoMerchantID != targetID {
				return false
			}
			if o.UserID != userID {
				return false
			}
		}
		return true
	})).Return(expected, nil)

	result, err := svc.MergeMerchants(ctx, userID, sourceIDs, targetID)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	mor.AssertExpectations(t)
}

func TestMerchantService_MergeMerchants_EmptySourceIDs_ReturnsEmpty(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	result, err := svc.MergeMerchants(ctx, "user-1", nil, "target")

	require.NoError(t, err)
	assert.Empty(t, result)
	mor.AssertNotCalled(t, "BulkUpsert")
	mor.AssertNotCalled(t, "GetMergeTarget")
}

func TestMerchantService_MergeMerchants_RepoError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	mor.On("GetMergeTarget", ctx, "user-1", "target").Return("", nil)
	mor.On("BulkUpsert", ctx, mock.Anything).Return(nil, errors.New("db error"))

	result, err := svc.MergeMerchants(ctx, "user-1", []string{"m1"}, "target")

	require.Error(t, err)
	assert.Nil(t, result)
	mor.AssertExpectations(t)
}

// G17: merge chains — transitive resolution and self-skip.

func TestMerchantService_MergeMerchants_TargetAlreadyMerged_FlattensToCanonical(t *testing.T) {
	// Scenario: m-B is already merged into m-C (chain B→C).
	// Merging m-A → m-B should write m-A → m-C (canonical), not m-A → m-B.
	// Existing overrides pointing at m-B are also re-flattened to m-C.
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	canonicalID := "m-C"

	mor.On("GetMergeTarget", ctx, userID, "m-B").Return("m-C", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-C").Return("", nil)
	mor.On("ReassignMergeTarget", ctx, userID, "m-B", "m-C").Return(nil)
	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m-A", MergedIntoMerchantID: &canonicalID},
	}
	mor.On("BulkUpsert", ctx, mock.MatchedBy(func(overrides []domain.MerchantNameOverride) bool {
		return len(overrides) == 1 &&
			overrides[0].MerchantID == "m-A" &&
			overrides[0].MergedIntoMerchantID != nil &&
			*overrides[0].MergedIntoMerchantID == "m-C"
	})).Return(expected, nil)

	result, err := svc.MergeMerchants(ctx, userID, []string{"m-A"}, "m-B")

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "m-A", result[0].MerchantID)
	assert.Equal(t, "m-C", *result[0].MergedIntoMerchantID)
	mor.AssertExpectations(t)
}

func TestMerchantService_MergeMerchants_SourceEqualsCanonicalTarget_Skipped(t *testing.T) {
	// Merging [m-A, m-B] → m-A: m-A is both a source and the canonical target.
	// m-A must be skipped; only m-B should be written.
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	targetID := "m-A"

	mor.On("GetMergeTarget", ctx, userID, "m-A").Return("", nil) // canonical = m-A
	targetPtr := targetID
	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m-B", MergedIntoMerchantID: &targetPtr},
	}
	mor.On("BulkUpsert", ctx, mock.MatchedBy(func(overrides []domain.MerchantNameOverride) bool {
		return len(overrides) == 1 && overrides[0].MerchantID == "m-B"
	})).Return(expected, nil)

	result, err := svc.MergeMerchants(ctx, userID, []string{"m-A", "m-B"}, "m-A")

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "m-B", result[0].MerchantID)
	mor.AssertExpectations(t)
}

func TestMerchantService_MergeMerchants_CycleGuard_TwoNode_ReturnsError(t *testing.T) {
	// Cycle in the DB: A→B, B→A. resolveCanonical detects the revisit and returns ErrMergeCycle.
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"

	// A→B→A: resolveCanonical("m-A") sees A, then B, then A again (cycle) → ErrMergeCycle.
	mor.On("GetMergeTarget", ctx, userID, "m-A").Return("m-B", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-B").Return("m-A", nil)

	_, err := svc.MergeMerchants(ctx, userID, []string{"m-X"}, "m-A")

	require.Error(t, err)
	require.ErrorIs(t, err, ErrMergeCycle)
}

func TestMerchantService_MergeMerchants_CycleGuard_ThreeNode_ReturnsError(t *testing.T) {
	// Three-node cycle: A→B, B→C, C→A. resolveCanonical must detect the cycle and return
	// ErrMergeCycle — not infinite-loop, and not silently succeed.
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"

	mor.On("GetMergeTarget", ctx, userID, "m-A").Return("m-B", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-B").Return("m-C", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-C").Return("m-A", nil)

	_, err := svc.MergeMerchants(ctx, userID, []string{"m-X"}, "m-A")

	require.Error(t, err)
	require.ErrorIs(t, err, ErrMergeCycle)
}

// ============================================================================
// GetMerchantOverrides
// ============================================================================

func TestMerchantService_GetMerchantOverrides_ReturnsOverrides(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	displayName := "Whole Foods"
	expected := []domain.MerchantNameOverride{{ID: "ov-1", MerchantID: "m-1", DisplayName: &displayName}}
	mor.On("GetByUserID", ctx, "user-1").Return(expected, nil)

	result, err := svc.GetMerchantOverrides(ctx, "user-1")

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}


func TestMerchantService_GetMerchantOverrides_RepoError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	mor.On("GetByUserID", ctx, "user-1").Return(nil, errors.New("db error"))

	_, err := svc.GetMerchantOverrides(ctx, "user-1")

	require.Error(t, err)
}

// ============================================================================
// NormalizeFunc wired in constructor
// ============================================================================

func TestMerchantService_NormalizeFuncIsWired(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := NewMerchantService(mr, mor)

	// The normalize func should clean a raw name
	assert.NotNil(t, svc.normalize)
	// Verify it's actually the merchant.Normalize function by behaviour
	result := svc.normalize("SQ * STARBUCKS #123")
	assert.Equal(t, "Starbucks", result)
}
