package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDataExportRepo records the user id it was asked about.
type stubDataExportRepo struct {
	export      *domain.DataExport
	err         error
	askedUserID string
}

func (s *stubDataExportRepo) ExportUserData(_ context.Context, userID string) (*domain.DataExport, error) {
	s.askedUserID = userID
	if s.err != nil {
		return nil, s.err
	}
	return s.export, nil
}

// recordingAnalytics captures the events fired, so the row_count property — the signal that tells us
// when the synchronous design has run out of road — is asserted rather than assumed.
type recordingAnalytics struct {
	events []struct {
		distinctID string
		event      string
		props      map[string]interface{}
	}
}

func (r *recordingAnalytics) Capture(distinctID, event string, props map[string]interface{}) {
	r.events = append(r.events, struct {
		distinctID string
		event      string
		props      map[string]interface{}
	}{distinctID, event, props})
}
func (r *recordingAnalytics) Identify(string, map[string]interface{}) {}
func (r *recordingAnalytics) Close()                                  {}

func exportFixture() *domain.DataExport {
	return &domain.DataExport{
		ExportVersion: domain.DataExportVersion,
		GeneratedAt:   time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC),
		UserID:        "user-a",
		Sections: map[string][]map[string]any{
			"transactions": {{"id": "t1"}, {"id": "t2"}},
			"budgets":      {{"id": "b1"}},
		},
		RowCount: 3,
	}
}

func TestExportUserData_ReturnsTheRequestedUsersExport(t *testing.T) {
	repo := &stubDataExportRepo{export: exportFixture()}
	svc := NewDataExportService(repo)

	export, err := svc.ExportUserData(context.Background(), "user-a")

	require.NoError(t, err)
	assert.Equal(t, "user-a", repo.askedUserID)
	assert.Equal(t, 3, export.RowCount)
	assert.Len(t, export.Sections, 2)
}

// TestExportUserData_EmptyUserID_IsRefused. An empty id would make the repository's `WHERE
// user_id = $1` predicates match nothing rather than everything, so this is not a data leak — but a
// silently empty export handed to a user exercising an Article 20 right IS a wrong answer, and it is
// indistinguishable from a correct one for a brand-new account. Refuse instead.
func TestExportUserData_EmptyUserID_IsRefused(t *testing.T) {
	repo := &stubDataExportRepo{export: exportFixture()}
	svc := NewDataExportService(repo)

	export, err := svc.ExportUserData(context.Background(), "")

	require.Error(t, err)
	assert.Nil(t, export)
	assert.Empty(t, repo.askedUserID, "the repository must not be reached with an empty id")
}

func TestExportUserData_RepositoryFailure_Propagates(t *testing.T) {
	sentinel := errors.New("boom")
	svc := NewDataExportService(&stubDataExportRepo{err: sentinel})

	export, err := svc.ExportUserData(context.Background(), "user-a")

	assert.Nil(t, export)
	require.ErrorIs(t, err, sentinel)
}

func TestExportUserData_FiresAnalyticsWithTheRowCount(t *testing.T) {
	analytics := &recordingAnalytics{}
	svc := NewDataExportService(&stubDataExportRepo{export: exportFixture()})
	svc.SetAnalytics(analytics)

	_, err := svc.ExportUserData(context.Background(), "user-a")
	require.NoError(t, err)

	require.Len(t, analytics.events, 1)
	assert.Equal(t, "user-a", analytics.events[0].distinctID)
	assert.Equal(t, "data_export_generated", analytics.events[0].event)
	assert.Equal(t, 3, analytics.events[0].props["row_count"])
	assert.Equal(t, 2, analytics.events[0].props["section_count"])
}

// TestExportUserData_NoAnalytics_IsNoOpSafe. Local dev and QA run without a PostHog key.
func TestExportUserData_NoAnalytics_IsNoOpSafe(t *testing.T) {
	svc := NewDataExportService(&stubDataExportRepo{export: exportFixture()})

	require.NotPanics(t, func() {
		_, err := svc.ExportUserData(context.Background(), "user-a")
		require.NoError(t, err)
	})
}

// A test that an analytics failure is surfaced to the caller would be wrong: the analytics client is
// fire-and-forget by contract. What IS worth pinning is that a failed export fires NO event, so the
// row-count signal never counts exports the user did not receive.
func TestExportUserData_Failure_FiresNoAnalytics(t *testing.T) {
	analytics := &recordingAnalytics{}
	svc := NewDataExportService(&stubDataExportRepo{err: errors.New("boom")})
	svc.SetAnalytics(analytics)

	_, err := svc.ExportUserData(context.Background(), "user-a")
	require.Error(t, err)
	assert.Empty(t, analytics.events)
}
