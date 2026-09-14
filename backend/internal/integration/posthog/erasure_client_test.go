package posthog

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErasureClient_RequestPersonDeletionUsesDocumentedBulkEndpoint(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"persons_found":1,"persons_deleted":1,"events_queued_for_deletion":true,"recordings_queued_for_deletion":true,"deletion_errors":[]}`))
	}))
	defer server.Close()

	client, err := NewErasureClient(ErasureClientConfig{
		Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "phx_secret",
		HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL },
	})
	require.NoError(t, err)

	result, err := client.RequestPersonDeletion(context.Background(), "42", "us", []string{"clerk-user", "legacy-uuid"})
	require.NoError(t, err)
	assert.Equal(t, "/api/projects/42/persons/bulk_delete/", gotPath)
	assert.Equal(t, "Bearer phx_secret", gotAuth)
	assert.JSONEq(t, `{"distinct_ids":["clerk-user","legacy-uuid"],"delete_events":true,"delete_recordings":true,"keep_person":false}`, gotBody)
	assert.True(t, result.EventsQueued)
	assert.True(t, result.RecordingsQueued)
}

func TestErasureClient_RefusesPersistedTargetMismatchBeforeNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{
		Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret",
		HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL },
	})
	require.NoError(t, err)
	for _, target := range [][2]string{{"99", "us"}, {"42", "eu"}, {"42", "unconfigured"}} {
		_, err = client.RequestPersonDeletion(context.Background(), target[0], target[1], []string{"subject"})
		require.ErrorContains(t, err, "target_mismatch")
	}
	assert.Zero(t, requests)
}

func TestErasureClient_RejectsUntrustedOrInsecureHost(t *testing.T) {
	for _, host := range []string{"http://us.posthog.com", "https://evil.example", "https://us.posthog.com.evil.example"} {
		t.Run(host, func(t *testing.T) {
			_, err := NewErasureClient(ErasureClientConfig{Host: host, ProjectID: "42", PersonalAPIKey: "secret"})
			require.ErrorContains(t, err, "allowlisted HTTPS host")
		})
	}
}

func TestErasureClient_FailsClosedOnRedirectOversizedAndMalformedResponses(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://eu.posthog.com/", http.StatusFound)
		}, "redirect"},
		{"oversized", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(strings.Repeat("x", 70<<10)))
		}, "response too large"},
		{"malformed", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"events_queued_for_deletion":false}`))
		}, "invalid deletion response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tt.handler)
			defer server.Close()
			client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
			require.NoError(t, err)
			_, err = client.RequestPersonDeletion(context.Background(), "42", "us", []string{"subject"})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestErasureClient_ClassifiesRetryAfterWithoutLeakingBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "999999")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("identity secret@example.com"))
	}))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
	require.NoError(t, err)
	_, err = client.RequestPersonDeletion(context.Background(), "42", "us", []string{"subject"})
	var providerErr *ErasureProviderError
	require.ErrorAs(t, err, &providerErr)
	assert.Equal(t, "rate_limited", providerErr.Class)
	assert.Equal(t, 24*time.Hour, providerErr.RetryAfter)
	assert.NotContains(t, err.Error(), "secret@example.com")
}

func TestErasureClient_ReturnsOfficialZeroMatch202ForCrashReconciliation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"persons_found":0,"persons_deleted":0,"events_queued_for_deletion":false,"recordings_queued_for_deletion":false,"deletion_errors":[]}`))
	}))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
	require.NoError(t, err)
	result, err := client.RequestPersonDeletion(context.Background(), "42", "us", []string{"already-deleted"})
	require.NoError(t, err)
	assert.Zero(t, result.PersonsFound)
	assert.False(t, result.EventsQueued)
}

func TestErasureClient_ResolvesAndDeduplicatesPersonUUIDsBeforeDeletion(t *testing.T) {
	var queries []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"uuid":"11111111-1111-4111-8111-111111111111"}],"next":null,"previous":null}`))
	}))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
	require.NoError(t, err)
	resolution, err := client.ResolvePersonUUIDs(context.Background(), "42", "us", []string{"clerk/a+b", "legacy"})
	require.NoError(t, err)
	assert.Equal(t, []string{"11111111-1111-4111-8111-111111111111"}, resolution.PersonUUIDs)
	assert.False(t, resolution.HasUnresolvedIdentities)
	assert.ElementsMatch(t, []string{"distinct_id=clerk%2Fa%2Bb", "distinct_id=legacy"}, queries)
}

func TestErasureClient_ResolutionPersistsMixedEventOnlySignal(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(`{"results":[{"uuid":"11111111-1111-4111-8111-111111111111"}],"next":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[],"next":null}`))
	}))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
	require.NoError(t, err)
	resolution, err := client.ResolvePersonUUIDs(context.Background(), "42", "us", []string{"person", "event-only"})
	require.NoError(t, err)
	assert.Len(t, resolution.PersonUUIDs, 1)
	assert.True(t, resolution.HasUnresolvedIdentities)
}

func TestErasureClient_StopsSequentialResolutionAfterPhaseCancellation(t *testing.T) {
	requests := 0
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		cancel()
		_, _ = w.Write([]byte(`{"results":[{"uuid":"11111111-1111-4111-8111-111111111111"}],"next":null}`))
	}))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
	require.NoError(t, err)

	_, err = client.ResolvePersonUUIDs(ctx, "42", "us", []string{"first", "second"})
	require.Error(t, err)
	assert.Equal(t, 1, requests)
}

func TestErasureClient_DeletionStatusPaginatesAndUsesRelevantAcceptedRecord(t *testing.T) {
	page := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		if page == 1 {
			_, _ = w.Write([]byte(`{"next":"/api/projects/42/persons/deletion_status/?page=2","results":[{"person_uuid":"11111111-1111-4111-8111-111111111111","created_at":"2026-09-14T10:00:00Z","status":"completed","delete_verified_at":"2026-09-14T10:01:00Z"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"next":null,"results":[{"person_uuid":"11111111-1111-4111-8111-111111111111","created_at":"2026-09-14T12:01:00Z","status":"completed","delete_verified_at":"2026-09-14T12:02:00Z"}]}`))
	}))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
	require.NoError(t, err)
	complete, err := client.PersonEventDeletionComplete(context.Background(), "42", "us", []string{"11111111-1111-4111-8111-111111111111"}, time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, complete)
	assert.Equal(t, 2, page)
}

func TestErasureClient_DeletionStatusDoesNotUseOldCompletionForCurrentPendingRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"next":null,"results":[
			{"person_uuid":"11111111-1111-4111-8111-111111111111","created_at":"2026-09-14T10:00:00Z","status":"completed","delete_verified_at":"2026-09-14T10:01:00Z"},
			{"person_uuid":"11111111-1111-4111-8111-111111111111","created_at":"2026-09-14T12:01:00Z","status":"pending","delete_verified_at":null}
		]}`))
	}))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
	require.NoError(t, err)

	observation, err := client.PersonEventDeletionStatus(
		context.Background(), "42", "us", []string{"11111111-1111-4111-8111-111111111111"},
		map[string]*time.Time{"11111111-1111-4111-8111-111111111111": nil},
		time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 14, 12, 2, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	assert.True(t, observation.Found)
	assert.False(t, observation.Complete)
}

func TestErasureClient_PollsDocumentedDeletionStatusByPersonUUID(t *testing.T) {
	var query string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"count":1,"next":null,"previous":null,"results":[{"person_uuid":"11111111-1111-4111-8111-111111111111","created_at":"2026-09-14T12:00:00Z","status":"completed","delete_verified_at":"2026-09-14T13:00:00Z"}]}`))
	}))
	defer server.Close()
	client, err := NewErasureClient(ErasureClientConfig{Host: "https://us.posthog.com", ProjectID: "42", PersonalAPIKey: "secret", HTTPClient: server.Client(), ResolveHost: func(string) string { return server.URL }})
	require.NoError(t, err)
	complete, err := client.PersonEventDeletionComplete(context.Background(), "42", "us", []string{"11111111-1111-4111-8111-111111111111"}, time.Date(2026, 9, 14, 11, 59, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, complete)
	assert.Contains(t, query, "person_uuid=11111111-1111-4111-8111-111111111111")
	assert.Contains(t, query, "status=all")
}
