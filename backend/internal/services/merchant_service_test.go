package services

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock implementations

type mockMerchantRepo struct {
	mock.Mock
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

func (m *mockMerchantOverrideRepo) MergeAndFlatten(ctx context.Context, userID string, sources []string, canonical string, toFlatten []string, displayNames []*string) ([]domain.MerchantNameOverride, error) {
	args := m.Called(ctx, userID, sources, canonical, toFlatten, displayNames)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.MerchantNameOverride), args.Error(1)
}

func (m *mockMerchantOverrideRepo) BulkApply(ctx context.Context, userID string, displayOnly []domain.MerchantNameOverride, groups []repository.BulkMergeGroup) ([]domain.MerchantNameOverride, error) {
	args := m.Called(ctx, userID, displayOnly, groups)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.MerchantNameOverride), args.Error(1)
}

// Test helper

func newMerchantService(mr *mockMerchantRepo, mor *mockMerchantOverrideRepo) *MerchantService {
	return NewMerchantService(mr, mor)
}

// ListMerchants tests

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

// GetDuplicateSuggestions tests

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

// SetOverride tests

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
	// Merge pointer: SetOverride now routes through applyMergeFlatten (G24).
	// Expects GetMergeTarget (resolveCanonical) + MergeAndFlatten, NOT Upsert.
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m1"
	targetID := "m2"

	expected := domain.MerchantNameOverride{
		ID:                   "ov-2",
		UserID:               userID,
		MerchantID:           merchantID,
		MergedIntoMerchantID: &targetID,
	}
	// resolveCanonical: m2 has no merge pointer → canonical = m2
	mor.On("GetMergeTarget", ctx, userID, targetID).Return("", nil)
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{merchantID}, targetID, []string{merchantID}, ([]*string)(nil),
	).Return([]domain.MerchantNameOverride{expected}, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, nil, &targetID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, targetID, *result.MergedIntoMerchantID)
	mor.AssertNotCalled(t, "Upsert")
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

// ClearOverride tests

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

// BulkSetOverrides tests

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

	// After the refactor, BulkSetOverrides calls BulkApply (not BulkUpsert/MergeAndFlatten).
	mor.On("BulkApply", ctx, userID,
		mock.MatchedBy(func(displayOnly []domain.MerchantNameOverride) bool {
			return len(displayOnly) == 2 &&
				displayOnly[0].MerchantID == "m1" &&
				displayOnly[1].MerchantID == "m2"
		}),
		[]repository.BulkMergeGroup{}, // no merge groups
	).Return(expected, nil)

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

	// One valid, one with neither field set — validation error before any write.
	dn := "Foo"
	reqs := []SetOverrideRequest{
		{MerchantID: "m1", DisplayName: &dn},
		{MerchantID: "m2"}, // invalid: no display_name, no merged_into
	}

	result, err := svc.BulkSetOverrides(ctx, "user-1", reqs)

	require.Error(t, err)
	assert.Nil(t, result)
	mor.AssertNotCalled(t, "BulkApply")
}

func TestMerchantService_BulkSetOverrides_EmptySlice_ReturnsEmpty(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	result, err := svc.BulkSetOverrides(ctx, "user-1", nil)

	require.NoError(t, err)
	assert.Empty(t, result)
	mor.AssertNotCalled(t, "BulkApply")
}

// MergeMerchants tests

func TestMerchantService_MergeMerchants_Success(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	targetID := "m-target"
	sourceIDs := []string{"m1", "m2"}

	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m1", MergedIntoMerchantID: &targetID},
		{ID: "ov-2", UserID: userID, MerchantID: "m2", MergedIntoMerchantID: &targetID},
	}

	// target has no existing merge pointer — canonical is itself; toFlatten == sources only.
	mor.On("GetMergeTarget", ctx, userID, targetID).Return("", nil)
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{"m1", "m2"},   // sources
		targetID,                // canonical
		[]string{"m1", "m2"},   // toFlatten (no pre-merged target)
		([]*string)(nil),        // no display names on MergeMerchants path
	).Return(expected, nil)

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
	mor.AssertNotCalled(t, "MergeAndFlatten")
	mor.AssertNotCalled(t, "GetMergeTarget")
}

func TestMerchantService_MergeMerchants_RepoError(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	mor.On("GetMergeTarget", ctx, "user-1", "target").Return("", nil)
	mor.On("MergeAndFlatten", ctx, "user-1",
		[]string{"m1"}, "target", []string{"m1"}, ([]*string)(nil),
	).Return(nil, errors.New("db error"))

	result, err := svc.MergeMerchants(ctx, "user-1", []string{"m1"}, "target")

	require.Error(t, err)
	assert.Nil(t, result)
	mor.AssertExpectations(t)
}

// G17: merge chains — transitive resolution and self-skip.

func TestMerchantService_MergeMerchants_TargetAlreadyMerged_FlattensToCanonical(t *testing.T) {
	// Scenario: m-B is already merged into m-C (chain B→C).
	// Merging [m-A] → m-B: canonical resolves to m-C.
	// toFlatten = [m-A, m-B] — includes both the source (G20) and the old target (G17).
	// MergeAndFlatten re-points anything pointing at m-A or m-B → m-C atomically.
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	canonicalID := "m-C"

	mor.On("GetMergeTarget", ctx, userID, "m-B").Return("m-C", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-C").Return("", nil)
	canonical := canonicalID
	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m-A", MergedIntoMerchantID: &canonical},
	}
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{"m-A"},            // sources
		"m-C",                      // canonical
		[]string{"m-A", "m-B"},     // toFlatten: source + old intermediate target
		([]*string)(nil),           // no display names on MergeMerchants path
	).Return(expected, nil)

	result, err := svc.MergeMerchants(ctx, userID, []string{"m-A"}, "m-B")

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "m-A", result[0].MerchantID)
	assert.Equal(t, "m-C", *result[0].MergedIntoMerchantID)
	mor.AssertExpectations(t)
}

func TestMerchantService_MergeMerchants_SourceEqualsCanonicalTarget_Skipped(t *testing.T) {
	// Merging [m-A, m-B] → m-A: m-A is both a source and the canonical target → skipped.
	// Only m-B is a source; toFlatten = [m-B] (no pre-merged target since canonical == target).
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	targetID := "m-A"

	mor.On("GetMergeTarget", ctx, userID, "m-A").Return("", nil) // canonical = m-A
	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m-B", MergedIntoMerchantID: &targetID},
	}
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{"m-B"}, // sources (m-A skipped — equals canonical)
		"m-A",           // canonical
		[]string{"m-B"}, // toFlatten
		([]*string)(nil), // no display names on MergeMerchants path
	).Return(expected, nil)

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

// G20: source-side transitive merge — a source that is itself a prior canonical must have
// its inbound chains re-pointed to the new canonical atomically.

func TestMerchantService_MergeMerchants_SourceAlreadyCanonical_FlattensIncomingChains(t *testing.T) {
	// Pre-condition: A→C exists in DB (written by a prior merge [A,B]→C).
	// Action: merge [C,D]→E (target=E, canonical=E — no pre-merge on E).
	// Expected: MergeAndFlatten([C,D], E, toFlatten=[C,D]) called.
	// The repo impl re-points A→C to A→E inside the transaction, closing the depth-2 gap.
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	canonical := "m-E"

	mor.On("GetMergeTarget", ctx, userID, "m-E").Return("", nil) // E is canonical
	expected := []domain.MerchantNameOverride{
		{ID: "ov-C", UserID: userID, MerchantID: "m-C", MergedIntoMerchantID: &canonical},
		{ID: "ov-D", UserID: userID, MerchantID: "m-D", MergedIntoMerchantID: &canonical},
	}
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{"m-C", "m-D"}, // sources
		canonical,               // canonical
		[]string{"m-C", "m-D"}, // toFlatten — the repo will UPDATE A→C to A→E
		([]*string)(nil),        // no display names on MergeMerchants path
	).Return(expected, nil)

	result, err := svc.MergeMerchants(ctx, userID, []string{"m-C", "m-D"}, "m-E")

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "m-C", result[0].MerchantID)
	assert.Equal(t, "m-D", result[1].MerchantID)
	for _, r := range result {
		assert.Equal(t, canonical, *r.MergedIntoMerchantID)
	}
	mor.AssertExpectations(t)
}

func TestMerchantService_MergeMerchants_TargetAndSourceBothPreMerged(t *testing.T) {
	// Pre-condition: T→G and A→C both exist (two prior merges).
	// Action: merge [C,D]→T (target=T, but T resolves to canonical G).
	// Expected: MergeAndFlatten([C,D], G, toFlatten=[C,D,T]) called.
	// The repo will: re-point A→C to A→G AND B→T to B→G in the same UPDATE, atomically.
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	canonical := "m-G"

	// T→G chain: resolveCanonical("m-T") → GetMergeTarget("m-T")="m-G" → GetMergeTarget("m-G")="" → canonical="m-G"
	mor.On("GetMergeTarget", ctx, userID, "m-T").Return("m-G", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-G").Return("", nil)

	expected := []domain.MerchantNameOverride{
		{ID: "ov-C", UserID: userID, MerchantID: "m-C", MergedIntoMerchantID: &canonical},
		{ID: "ov-D", UserID: userID, MerchantID: "m-D", MergedIntoMerchantID: &canonical},
	}
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{"m-C", "m-D"},        // sources (neither is m-G)
		canonical,                      // canonical = m-G
		[]string{"m-C", "m-D", "m-T"}, // toFlatten: sources + old intermediate target m-T
		([]*string)(nil),               // no display names on MergeMerchants path
	).Return(expected, nil)

	result, err := svc.MergeMerchants(ctx, userID, []string{"m-C", "m-D"}, "m-T")

	require.NoError(t, err)
	require.Len(t, result, 2)
	for _, r := range result {
		assert.Equal(t, canonical, *r.MergedIntoMerchantID)
	}
	mor.AssertExpectations(t)
}

// GetMerchantOverrides tests

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

// G24: SetOverride/BulkSetOverrides merge writes routed through applyMergeFlatten tests

// SetOverride with a merge pointer must call GetMergeTarget + MergeAndFlatten, NOT Upsert.
func TestSetOverride_WithMergePointer_RoutesThroughFlatten(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m-B"
	targetID := "m-C"

	// C has no existing merge pointer → canonical is C
	mor.On("GetMergeTarget", ctx, userID, targetID).Return("", nil)
	expected := domain.MerchantNameOverride{
		ID: "ov-1", UserID: userID, MerchantID: merchantID, MergedIntoMerchantID: &targetID,
	}
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{merchantID},  // sources
		targetID,              // canonical
		[]string{merchantID},  // toFlatten (no pre-merged target)
		([]*string)(nil),      // no display name
	).Return([]domain.MerchantNameOverride{expected}, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, nil, &targetID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, targetID, *result.MergedIntoMerchantID)
	mor.AssertNotCalled(t, "Upsert")
	mor.AssertExpectations(t)
}

// SetOverride with only a display name must call Upsert, NOT MergeAndFlatten.
func TestSetOverride_PureRename_UsesUpsert(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m-B"
	displayName := "New Name"

	expected := &domain.MerchantNameOverride{
		ID: "ov-1", UserID: userID, MerchantID: merchantID, DisplayName: &displayName,
	}
	mor.On("Upsert", ctx, mock.MatchedBy(func(o domain.MerchantNameOverride) bool {
		return o.UserID == userID && o.MerchantID == merchantID &&
			o.DisplayName != nil && *o.DisplayName == displayName &&
			o.MergedIntoMerchantID == nil
	})).Return(expected, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, &displayName, nil)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	mor.AssertNotCalled(t, "MergeAndFlatten")
	mor.AssertExpectations(t)
}

// SetOverride with both display_name and merged_into must thread the display_name atomically
// into MergeAndFlatten — NO pre-Upsert (G27: avoids transient NULL on existing merge pointer).
func TestSetOverride_RenameAndMerge_RoutesThroughFlattenPreservingDisplayName(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m-B"
	targetID := "m-C"
	displayName := "Custom Name"

	mergeResult := domain.MerchantNameOverride{
		ID: "ov-1", UserID: userID, MerchantID: merchantID,
		DisplayName: &displayName, MergedIntoMerchantID: &targetID,
	}

	// G27: NO pre-Upsert. C has no existing merge pointer → canonical is C.
	mor.On("GetMergeTarget", ctx, userID, targetID).Return("", nil)
	// MergeAndFlatten receives the displayName atomically — no separate Upsert.
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{merchantID}, targetID, []string{merchantID},
		mock.MatchedBy(func(dn []*string) bool {
			return len(dn) == 1 && dn[0] != nil && *dn[0] == displayName
		}),
	).Return([]domain.MerchantNameOverride{mergeResult}, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, &displayName, &targetID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, displayName, *result.DisplayName)
	assert.Equal(t, targetID, *result.MergedIntoMerchantID)
	// No pre-Upsert must have been called (G27 atomic fix).
	mor.AssertNotCalled(t, "Upsert")
	mor.AssertExpectations(t)
}

// SetOverride with merge pointer pointing at a pre-merged target resolves to the canonical.
func TestSetOverride_WithMergePointer_TargetPreMerged_ResolvesToCanonical(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m-B"
	targetID := "m-T" // T→G chain in DB
	canonicalID := "m-G"

	// resolveCanonical: T→G, G has no merge pointer
	mor.On("GetMergeTarget", ctx, userID, targetID).Return(canonicalID, nil)
	mor.On("GetMergeTarget", ctx, userID, canonicalID).Return("", nil)
	expected := domain.MerchantNameOverride{
		ID: "ov-1", UserID: userID, MerchantID: merchantID, MergedIntoMerchantID: &canonicalID,
	}
	// MergeAndFlatten called with canonical=G, toFlatten includes source + old intermediate T
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{merchantID},           // sources
		canonicalID,                    // canonical = G
		[]string{merchantID, targetID}, // toFlatten: source + old T
		([]*string)(nil),               // no display name
	).Return([]domain.MerchantNameOverride{expected}, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, nil, &targetID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, canonicalID, *result.MergedIntoMerchantID)
	mor.AssertExpectations(t)
}

// BulkSetOverrides with a mixed batch: display-only entries → BulkUpsert,
// merge-bearing entries → applyMergeFlatten (via GetMergeTarget + MergeAndFlatten).
func TestBulkSetOverrides_SplitsMergeAndDisplayEntries(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	dn1 := "Starbucks"
	target := "m-C"

	reqs := []SetOverrideRequest{
		{MerchantID: "m1", DisplayName: &dn1},             // display-only
		{MerchantID: "m2", MergedIntoMerchantID: &target}, // merge-bearing
	}

	combined := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m1", DisplayName: &dn1},
		{ID: "ov-2", UserID: userID, MerchantID: "m2", MergedIntoMerchantID: &target},
	}

	// Service resolves canonical for m-C before calling BulkApply.
	mor.On("GetMergeTarget", ctx, userID, target).Return("", nil)
	// After the refactor, service calls BulkApply (single atomic write).
	mor.On("BulkApply", ctx, userID,
		mock.MatchedBy(func(displayOnly []domain.MerchantNameOverride) bool {
			return len(displayOnly) == 1 && displayOnly[0].MerchantID == "m1"
		}),
		mock.MatchedBy(func(groups []repository.BulkMergeGroup) bool {
			return len(groups) == 1 &&
				groups[0].Canonical == target &&
				len(groups[0].Sources) == 1 && groups[0].Sources[0] == "m2"
		}),
	).Return(combined, nil)

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "m1", result[0].MerchantID)
	assert.Equal(t, "m2", result[1].MerchantID)
	assert.Equal(t, target, *result[1].MergedIntoMerchantID)
	mor.AssertExpectations(t)
}

// BulkSetOverrides combined entry (both DisplayName and MergedIntoMerchantID) must
// appear exactly once in the response — from MergeAndFlatten only (G27: no staging BulkUpsert).
// G27: display_name is threaded atomically through MergeAndFlatten — no pre-write that could
// transiently NULL an existing merge pointer.
func TestBulkSetOverrides_CombinedEntry_NoDuplicateInResponse(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	dn := "Whole Foods"
	target := "m-T"

	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", DisplayName: &dn, MergedIntoMerchantID: &target},
	}

	// Target T has no existing merge pointer → canonical = T.
	mor.On("GetMergeTarget", ctx, userID, target).Return("", nil)
	mergeResult := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m-A", DisplayName: &dn, MergedIntoMerchantID: &target},
	}
	// After atomicity refactor: single BulkApply call (no separate BulkUpsert + MergeAndFlatten).
	// Display-name is threaded through the group's DisplayNames slice (G27 atomic).
	mor.On("BulkApply", ctx, userID,
		mock.MatchedBy(func(displayOnly []domain.MerchantNameOverride) bool {
			return len(displayOnly) == 0
		}),
		mock.MatchedBy(func(groups []repository.BulkMergeGroup) bool {
			return len(groups) == 1 &&
				groups[0].Canonical == target &&
				len(groups[0].Sources) == 1 && groups[0].Sources[0] == "m-A" &&
				groups[0].DisplayNames != nil &&
				len(groups[0].DisplayNames) == 1 && groups[0].DisplayNames[0] != nil &&
				*groups[0].DisplayNames[0] == dn
		}),
	).Return(mergeResult, nil)

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.NoError(t, err)
	require.Len(t, result, 1, "combined entry must appear exactly once (BulkApply result only)")
	assert.Equal(t, "m-A", result[0].MerchantID)
	assert.Equal(t, target, *result[0].MergedIntoMerchantID)
	// MergeAndFlatten must NOT be called independently (atomic via BulkApply).
	// (The exported BulkUpsert was deleted in #1565 — BulkApply's display-only leg now
	// reaches the same SQL through the unexported bulkUpsertTx, inside its own tx.)
	mor.AssertNotCalled(t, "MergeAndFlatten")
	mor.AssertExpectations(t)
}

// BulkSetOverrides merge entry with a pre-merged target resolves to the canonical.
func TestBulkSetOverrides_MergeEntry_TargetPreMerged_ResolvesToCanonical(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	target := "m-T"
	canonical := "m-G"

	reqs := []SetOverrideRequest{
		{MerchantID: "m-B", MergedIntoMerchantID: &target}, // T→G in DB
	}

	// resolveCanonical: T→G, G is terminal
	mor.On("GetMergeTarget", ctx, userID, target).Return(canonical, nil)
	mor.On("GetMergeTarget", ctx, userID, canonical).Return("", nil)
	mergeResult := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m-B", MergedIntoMerchantID: &canonical},
	}
	// After atomicity refactor: BulkApply with one group (no separate MergeAndFlatten).
	// toFlatten includes source + intermediate target T (since canonical G != original target T).
	mor.On("BulkApply", ctx, userID,
		mock.MatchedBy(func(displayOnly []domain.MerchantNameOverride) bool {
			return len(displayOnly) == 0
		}),
		mock.MatchedBy(func(groups []repository.BulkMergeGroup) bool {
			if len(groups) != 1 {
				return false
			}
			g := groups[0]
			return g.Canonical == canonical &&
				len(g.Sources) == 1 && g.Sources[0] == "m-B" &&
				len(g.ToFlatten) == 2 &&
				g.ToFlatten[0] == "m-B" && g.ToFlatten[1] == target &&
				g.DisplayNames == nil
		}),
	).Return(mergeResult, nil)

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, canonical, *result[0].MergedIntoMerchantID)
	mor.AssertNotCalled(t, "MergeAndFlatten")
	mor.AssertExpectations(t)
}

// G27: atomic combined rename+merge correctness tests

// G27 (HIGH): If MergeAndFlatten fails on a combined rename+merge request, the existing
// merged_into_merchant_id of a pre-existing override must NOT be cleared. With the atomic
// fix (no pre-Upsert), if applyMergeFlatten returns an error, nothing is written — the
// pre-existing row is untouched.
func TestSetOverride_RenameAndMerge_MergeAndFlattenFails_DoesNotUnmerge(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m-B"
	targetID := "m-C"
	displayName := "New Name"

	// C has no existing merge pointer → canonical is C.
	mor.On("GetMergeTarget", ctx, userID, targetID).Return("", nil)
	// MergeAndFlatten fails (DB error, ctx cancelled, etc.)
	mor.On("MergeAndFlatten", ctx, userID,
		[]string{merchantID}, targetID, []string{merchantID},
		mock.MatchedBy(func(dn []*string) bool {
			return len(dn) == 1 && dn[0] != nil && *dn[0] == displayName
		}),
	).Return(nil, errors.New("db error"))

	result, err := svc.SetOverride(ctx, userID, merchantID, &displayName, &targetID)

	// The service must propagate the error.
	require.Error(t, err)
	assert.Nil(t, result)
	// CRITICAL: Upsert must NOT have been called — no pre-write that would transiently
	// clear an existing merged_into_merchant_id before the failed flatten (G27).
	mor.AssertNotCalled(t, "Upsert")
	mor.AssertExpectations(t)
}

// G27 self-merge fallback: when merchantID == canonical (self-merge, no write occurs) but
// a display_name was also provided, the display must still be persisted via Upsert.
func TestSetOverride_SelfMergeWithRename_StillPersistsDisplayName(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m-A"
	targetID := "m-A" // self-merge: merchantID == mergedInto
	displayName := "Self Renamed"

	// resolveCanonical: m-A has no merge pointer → canonical = m-A
	mor.On("GetMergeTarget", ctx, userID, targetID).Return("", nil)
	// MergeAndFlatten is NOT called (all sources equal canonical → filtered out).
	// G27 self-merge fallback: Upsert must be called to persist the display_name.
	expected := &domain.MerchantNameOverride{
		ID: "ov-1", UserID: userID, MerchantID: merchantID, DisplayName: &displayName,
	}
	mor.On("Upsert", ctx, mock.MatchedBy(func(o domain.MerchantNameOverride) bool {
		return o.UserID == userID && o.MerchantID == merchantID &&
			o.DisplayName != nil && *o.DisplayName == displayName
	})).Return(expected, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, &displayName, &targetID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, displayName, *result.DisplayName)
	// MergeAndFlatten must NOT have been called (sources were empty after self-merge filter).
	mor.AssertNotCalled(t, "MergeAndFlatten")
	mor.AssertExpectations(t)
}

// G27 bulk self-merge fallback: when a BulkSetOverrides entry has merchant_id == resolved
// canonical (self-merge), applyMergeFlatten returns empty. If the entry also carried a
// display_name, Upsert must still be called to persist the rename — otherwise it is silently lost.
func TestBulkSetOverrides_SelfMergeWithRename_StillPersistsDisplayName(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m-A"
	targetID := "m-A" // self-merge: merchant_id == merged_into_merchant_id
	dn := "Self Renamed"

	reqs := []SetOverrideRequest{
		{MerchantID: merchantID, DisplayName: &dn, MergedIntoMerchantID: &targetID},
	}

	// resolveCanonical: m-A has no merge pointer → canonical = m-A (self-merge).
	// Self-merge renames now go into displayOnly inside BulkApply (atomicity refactor).
	mor.On("GetMergeTarget", ctx, userID, targetID).Return("", nil)

	// BulkApply called with displayOnly=[{m-A, dn}] + empty groups (no real merge sources).
	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: merchantID, DisplayName: &dn},
	}
	mor.On("BulkApply", ctx, userID,
		mock.MatchedBy(func(displayOnly []domain.MerchantNameOverride) bool {
			return len(displayOnly) == 1 &&
				displayOnly[0].MerchantID == merchantID &&
				displayOnly[0].DisplayName != nil && *displayOnly[0].DisplayName == dn
		}),
		mock.MatchedBy(func(groups []repository.BulkMergeGroup) bool {
			return len(groups) == 0
		}),
	).Return(expected, nil)

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, dn, *result[0].DisplayName)
	// MergeAndFlatten + Upsert must NOT be called (both folded into BulkApply).
	mor.AssertNotCalled(t, "MergeAndFlatten")
	mor.AssertNotCalled(t, "Upsert")
	mor.AssertExpectations(t)
}

// TestSetOverride_PureRename_DoesNotClobberMergePointer_ServiceLayer verifies that the service's
// pure-rename branch calls Upsert with MergedIntoMerchantID=nil — the SQL COALESCE in the repo
// is what preserves the existing merge pointer. This test confirms the service routing is correct;
// the real pointer-preservation is validated by the integration tests (G31).
func TestSetOverride_PureRename_DoesNotClobberMergePointer_ServiceLayer(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	merchantID := "m-A"
	displayName := "New Label"

	// The service calls Upsert with nil merge pointer — the SQL COALESCE preserves the existing one.
	returned := &domain.MerchantNameOverride{
		ID: "ov-1", UserID: userID, MerchantID: merchantID, DisplayName: &displayName,
	}
	mor.On("Upsert", ctx, mock.MatchedBy(func(o domain.MerchantNameOverride) bool {
		return o.UserID == userID && o.MerchantID == merchantID &&
			o.DisplayName != nil && *o.DisplayName == displayName &&
			o.MergedIntoMerchantID == nil
	})).Return(returned, nil)

	result, err := svc.SetOverride(ctx, userID, merchantID, &displayName, nil)
	require.NoError(t, err)
	assert.Equal(t, returned, result)
	// Must NOT have called MergeAndFlatten for a pure rename.
	mor.AssertNotCalled(t, "MergeAndFlatten")
	mor.AssertExpectations(t)
}

// BulkSetOverrides propagates a BulkApply repo error to the caller.
func TestBulkSetOverrides_BulkApplyError_Propagates(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	target := "m-T"

	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", MergedIntoMerchantID: &target},
	}

	mor.On("GetMergeTarget", ctx, userID, target).Return("", nil)
	mor.On("BulkApply", ctx, userID,
		mock.Anything,
		mock.Anything,
	).Return(nil, errors.New("db error"))

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.Error(t, err)
	assert.Nil(t, result)
	mor.AssertExpectations(t)
}

// G43: BulkSetOverrides must reject a request that references the same merchant_id more than once.
// A self-contradictory bulk request (rename m-A AND merge m-A in the same call, or two display
// entries for the same merchant) previously slipped through validation and produced a duplicate
// response row. The correct policy is to reject before any partition/read/write.

func TestBulkSetOverrides_DuplicateMerchantID_DisplayOnlyTwice_Rejected(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)

	dn1 := "Name One"
	dn2 := "Name Two"

	// Same merchant_id appears twice (both display-only — conflicting names).
	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", DisplayName: &dn1},
		{MerchantID: "m-A", DisplayName: &dn2},
	}

	result, err := svc.BulkSetOverrides(context.Background(), "user-1", reqs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "m-A")
	assert.Nil(t, result)
	// No write must occur.
	mor.AssertNotCalled(t, "BulkApply")
	mor.AssertNotCalled(t, "GetMergeTarget")
}

func TestBulkSetOverrides_DuplicateMerchantID_DisplayAndMerge_Rejected(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)

	dn := "Renamed"
	target := "m-B"

	// Same merchant_id m-A appears once as display-only AND once as merge-bearing —
	// a self-contradictory request that previously caused a duplicate response row.
	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", DisplayName: &dn},
		{MerchantID: "m-A", MergedIntoMerchantID: &target},
	}

	result, err := svc.BulkSetOverrides(context.Background(), "user-1", reqs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "m-A")
	assert.Nil(t, result)
	// No write must occur — the guard fires in Phase-1 validation, before any DB call.
	mor.AssertNotCalled(t, "BulkApply")
	mor.AssertNotCalled(t, "GetMergeTarget")
}

func TestBulkSetOverrides_DuplicateMerchantID_MergeTwice_Rejected(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	target1 := "m-B"
	target2 := "m-C"

	// Same source merchant_id m-A appears twice as merge-bearing (different targets).
	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", MergedIntoMerchantID: &target1},
		{MerchantID: "m-A", MergedIntoMerchantID: &target2},
	}

	result, err := svc.BulkSetOverrides(context.Background(), "user-1", reqs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "m-A")
	assert.Nil(t, result)
	mor.AssertNotCalled(t, "BulkApply")
	mor.AssertNotCalled(t, "GetMergeTarget")
}

// TestBulkSetOverrides_MergeCycle_TwoNode_Rejected verifies rejection of 2-node cycle [{A→B},{B→A}].
func TestBulkSetOverrides_MergeCycle_TwoNode_Rejected(t *testing.T) {
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(nil, mor)
	ctx := context.Background()
	userID := "user-1"

	mor.On("GetMergeTarget", ctx, userID, "m-A").Return("", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-B").Return("", nil)

	targetB := "m-B"
	targetA := "m-A"
	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", MergedIntoMerchantID: &targetB},
		{MerchantID: "m-B", MergedIntoMerchantID: &targetA},
	}

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "same request")
	assert.Nil(t, result)
	mor.AssertNotCalled(t, "BulkApply")
}

// TestBulkSetOverrides_MergeCycle_ThreeNode_Rejected verifies rejection of 3-node cycle [{A→B},{B→C},{C→A}].
func TestBulkSetOverrides_MergeCycle_ThreeNode_Rejected(t *testing.T) {
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(nil, mor)
	ctx := context.Background()
	userID := "user-1"

	mor.On("GetMergeTarget", ctx, userID, "m-A").Return("", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-B").Return("", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-C").Return("", nil)

	targetB := "m-B"
	targetC := "m-C"
	targetA := "m-A"
	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", MergedIntoMerchantID: &targetB},
		{MerchantID: "m-B", MergedIntoMerchantID: &targetC},
		{MerchantID: "m-C", MergedIntoMerchantID: &targetA},
	}

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "same request")
	assert.Nil(t, result)
	mor.AssertNotCalled(t, "BulkApply")
}

// TestBulkSetOverrides_MergeFanIn_NonCyclic_Succeeds verifies that a non-cyclic
// fan-in request [{A→C},{B→C}] is not rejected (G44 is not over-broad).
func TestBulkSetOverrides_MergeFanIn_NonCyclic_Succeeds(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()
	userID := "user-1"
	targetC := "m-C"

	mor.On("GetMergeTarget", ctx, userID, targetC).Return("", nil)

	mergeResult := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: userID, MerchantID: "m-A", MergedIntoMerchantID: &targetC},
		{ID: "ov-2", UserID: userID, MerchantID: "m-B", MergedIntoMerchantID: &targetC},
	}
	mor.On("BulkApply", ctx, userID,
		mock.MatchedBy(func(displayOnly []domain.MerchantNameOverride) bool {
			return len(displayOnly) == 0
		}),
		mock.MatchedBy(func(groups []repository.BulkMergeGroup) bool {
			return len(groups) == 1 && groups[0].Canonical == targetC && len(groups[0].Sources) == 2
		}),
	).Return(mergeResult, nil)

	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", MergedIntoMerchantID: &targetC},
		{MerchantID: "m-B", MergedIntoMerchantID: &targetC},
	}

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.NoError(t, err)
	require.Len(t, result, 2)
	mor.AssertExpectations(t)
}

// TestBulkSetOverrides_MergeChain_AcyclicThreeNode_Rejected verifies rejection of chain [{A→B},{B→C}].
func TestBulkSetOverrides_MergeChain_AcyclicThreeNode_Rejected(t *testing.T) {
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(nil, mor)
	ctx := context.Background()
	userID := "user-1"

	mor.On("GetMergeTarget", ctx, userID, "m-B").Return("", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-C").Return("", nil)

	targetB := "m-B"
	targetC := "m-C"
	reqs := []SetOverrideRequest{
		{MerchantID: "m-A", MergedIntoMerchantID: &targetB},
		{MerchantID: "m-B", MergedIntoMerchantID: &targetC},
	}

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "same request")
	assert.Nil(t, result)
	mor.AssertNotCalled(t, "BulkApply")
}

// TestBulkSetOverrides_MergeChain_ReverseOrder_Rejected verifies rejection of reverse-order chain [{Y→Z},{X→Y}].
func TestBulkSetOverrides_MergeChain_ReverseOrder_Rejected(t *testing.T) {
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(nil, mor)
	ctx := context.Background()
	userID := "user-1"

	mor.On("GetMergeTarget", ctx, userID, "m-Z").Return("", nil)
	mor.On("GetMergeTarget", ctx, userID, "m-Y").Return("", nil)

	targetZ := "m-Z"
	targetY := "m-Y"
	reqs := []SetOverrideRequest{
		{MerchantID: "m-Y", MergedIntoMerchantID: &targetZ},
		{MerchantID: "m-X", MergedIntoMerchantID: &targetY},
	}

	result, err := svc.BulkSetOverrides(ctx, userID, reqs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "same request")
	assert.Nil(t, result)
	mor.AssertNotCalled(t, "BulkApply")
}

// NormalizeFunc wired in constructor test

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

// Mock analytics for testing

type mockAnalytics struct {
	mock.Mock
}

func (m *mockAnalytics) Capture(distinctID, event string, properties map[string]interface{}) {
	m.Called(distinctID, event, properties)
}

func (m *mockAnalytics) Identify(distinctID string, properties map[string]interface{}) {
	m.Called(distinctID, properties)
}

func (m *mockAnalytics) Close() {
	m.Called()
}

// SetAnalytics tests

func TestMerchantService_SetAnalytics_NilSafe_SetOverride(t *testing.T) {
	// Test that SetOverride without SetAnalytics doesn't panic
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	displayName := "Starbucks"
	expected := &domain.MerchantNameOverride{
		ID:          "ov-1",
		UserID:      "user-1",
		MerchantID:  "m1",
		DisplayName: &displayName,
	}
	mor.On("Upsert", ctx, mock.Anything).Return(expected, nil)

	// SetOverride without SetAnalytics — should not panic
	result, err := svc.SetOverride(ctx, "user-1", "m1", &displayName, nil)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestMerchantService_SetOverride_FiresAnalyticsEvent(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	analytics := new(mockAnalytics)
	svc.SetAnalytics(analytics)
	ctx := context.Background()

	displayName := "Starbucks"
	expected := &domain.MerchantNameOverride{
		ID:          "ov-1",
		UserID:      "user-1",
		MerchantID:  "m1",
		DisplayName: &displayName,
	}
	mor.On("Upsert", ctx, mock.Anything).Return(expected, nil)

	analytics.On("Capture", "user-1", phclient.EventMerchantOverrideSet, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["merchant_id"] == "m1" && props["has_display_name"] == true && props["is_merge"] == false
	})).Return()

	result, err := svc.SetOverride(ctx, "user-1", "m1", &displayName, nil)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	analytics.AssertExpectations(t)
}

func TestMerchantService_SetOverride_WithMerge_FiresAnalyticsWithMergeFlag(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	analytics := new(mockAnalytics)
	svc.SetAnalytics(analytics)
	ctx := context.Background()

	targetID := "m2"
	expected := domain.MerchantNameOverride{
		ID:                   "ov-2",
		UserID:               "user-1",
		MerchantID:           "m1",
		MergedIntoMerchantID: &targetID,
	}
	mor.On("GetMergeTarget", ctx, "user-1", targetID).Return("", nil)
	mor.On("MergeAndFlatten", ctx, "user-1",
		[]string{"m1"}, targetID, []string{"m1"}, ([]*string)(nil),
	).Return([]domain.MerchantNameOverride{expected}, nil)

	analytics.On("Capture", "user-1", phclient.EventMerchantOverrideSet, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["merchant_id"] == "m1" && props["has_display_name"] == false && props["is_merge"] == true
	})).Return()

	_, err := svc.SetOverride(ctx, "user-1", "m1", nil, &targetID)

	require.NoError(t, err)
	analytics.AssertExpectations(t)
}

func TestMerchantService_ClearOverride_FiresAnalyticsEvent(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	analytics := new(mockAnalytics)
	svc.SetAnalytics(analytics)
	ctx := context.Background()

	mor.On("Delete", ctx, "user-1", "m1").Return(nil)
	analytics.On("Capture", "user-1", phclient.EventMerchantOverrideCleared, map[string]interface{}{
		"merchant_id": "m1",
	}).Return()

	err := svc.ClearOverride(ctx, "user-1", "m1")

	require.NoError(t, err)
	analytics.AssertExpectations(t)
}

func TestMerchantService_ClearOverride_NilSafe(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	mor.On("Delete", ctx, "user-1", "m1").Return(nil)

	err := svc.ClearOverride(ctx, "user-1", "m1")

	require.NoError(t, err)
}

func TestMerchantService_MergeMerchants_FiresAnalyticsEvent(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	analytics := new(mockAnalytics)
	svc.SetAnalytics(analytics)
	ctx := context.Background()

	targetID := "m-target"
	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: "user-1", MerchantID: "m1", MergedIntoMerchantID: &targetID},
		{ID: "ov-2", UserID: "user-1", MerchantID: "m2", MergedIntoMerchantID: &targetID},
	}

	mor.On("GetMergeTarget", ctx, "user-1", targetID).Return("", nil)
	mor.On("MergeAndFlatten", ctx, "user-1",
		[]string{"m1", "m2"}, targetID, []string{"m1", "m2"}, ([]*string)(nil),
	).Return(expected, nil)

	analytics.On("Capture", "user-1", phclient.EventMerchantsMerged, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["source_merchant_id"] == "m1" && props["target_merchant_id"] == targetID
	})).Return()
	analytics.On("Capture", "user-1", phclient.EventMerchantsMerged, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["source_merchant_id"] == "m2" && props["target_merchant_id"] == targetID
	})).Return()

	result, err := svc.MergeMerchants(ctx, "user-1", []string{"m1", "m2"}, targetID)

	require.NoError(t, err)
	require.Len(t, result, 2)
	analytics.AssertExpectations(t)
}

func TestMerchantService_MergeMerchants_NilSafe(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	targetID := "m-target"
	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: "user-1", MerchantID: "m1", MergedIntoMerchantID: &targetID},
	}

	mor.On("GetMergeTarget", ctx, "user-1", targetID).Return("", nil)
	mor.On("MergeAndFlatten", ctx, "user-1",
		[]string{"m1"}, targetID, []string{"m1"}, ([]*string)(nil),
	).Return(expected, nil)

	result, err := svc.MergeMerchants(ctx, "user-1", []string{"m1"}, targetID)

	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestMerchantService_BulkSetOverrides_FiresAnalyticsEvent(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	analytics := new(mockAnalytics)
	svc.SetAnalytics(analytics)
	ctx := context.Background()

	dn1 := "Starbucks"
	dn2 := "Amazon Prime"
	reqs := []SetOverrideRequest{
		{MerchantID: "m1", DisplayName: &dn1},
		{MerchantID: "m2", DisplayName: &dn2},
	}

	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: "user-1", MerchantID: "m1", DisplayName: &dn1},
		{ID: "ov-2", UserID: "user-1", MerchantID: "m2", DisplayName: &dn2},
	}

	mor.On("BulkApply", ctx, "user-1", mock.Anything, mock.Anything).Return(expected, nil)

	analytics.On("Capture", "user-1", phclient.EventMerchantsBulkOverridden, map[string]interface{}{
		"count": 2,
	}).Return()

	result, err := svc.BulkSetOverrides(ctx, "user-1", reqs)

	require.NoError(t, err)
	require.Len(t, result, 2)
	analytics.AssertExpectations(t)
}

func TestMerchantService_BulkSetOverrides_NilSafe(t *testing.T) {
	mr := new(mockMerchantRepo)
	mor := new(mockMerchantOverrideRepo)
	svc := newMerchantService(mr, mor)
	ctx := context.Background()

	dn1 := "Starbucks"
	reqs := []SetOverrideRequest{
		{MerchantID: "m1", DisplayName: &dn1},
	}

	expected := []domain.MerchantNameOverride{
		{ID: "ov-1", UserID: "user-1", MerchantID: "m1", DisplayName: &dn1},
	}

	mor.On("BulkApply", ctx, "user-1", mock.Anything, mock.Anything).Return(expected, nil)

	result, err := svc.BulkSetOverrides(ctx, "user-1", reqs)

	require.NoError(t, err)
	require.Len(t, result, 1)
}
