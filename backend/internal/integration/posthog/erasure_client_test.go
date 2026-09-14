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
