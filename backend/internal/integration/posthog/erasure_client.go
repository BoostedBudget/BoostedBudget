package posthog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	erasureRequestTimeout = 10 * time.Second
	erasureResponseLimit  = 64 << 10
	maxRetryAfter         = 24 * time.Hour
)

var allowedPostHogPrivateHosts = map[string]bool{
	"us.posthog.com": true,
	"eu.posthog.com": true,
}

func ErasureRegionForHost(host string) string {
	parsed, err := url.Parse(host)
	if err != nil {
		return "unconfigured"
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "us.posthog.com":
		return "us"
	case "eu.posthog.com":
		return "eu"
	default:
		return "unconfigured"
	}
}

type ErasureClientConfig struct {
	Host           string
	ProjectID      string
	PersonalAPIKey string
	HTTPClient     *http.Client
	// ResolveHost exists solely so httptest can exercise the wire contract without contacting
	// PostHog. Production never sets it; the configured Host is still validated first.
	ResolveHost func(string) string
}

type ErasureDeleteResult struct {
	PersonsFound     int
	PersonsDeleted   int
	EventsQueued     bool
	RecordingsQueued bool
}

type ErasureProviderError struct {
	Class      string
	RetryAfter time.Duration
}

func (e *ErasureProviderError) Error() string { return "PostHog erasure request failed: " + e.Class }

type ErasureClient struct {
	baseURL   string
	projectID string
	region    string
	apiKey    string
	http      *http.Client
}

func NewErasureClient(cfg ErasureClientConfig) (*ErasureClient, error) {
	parsed, err := url.Parse(cfg.Host)
	if err != nil || parsed.Scheme != "https" || parsed.Path != "" || parsed.RawQuery != "" || !allowedPostHogPrivateHosts[strings.ToLower(parsed.Hostname())] {
		return nil, fmt.Errorf("PostHog erasure requires an allowlisted HTTPS host")
	}
	if strings.TrimSpace(cfg.ProjectID) == "" || strings.TrimSpace(cfg.PersonalAPIKey) == "" {
		return nil, fmt.Errorf("PostHog erasure configuration is incomplete")
	}
	baseURL := strings.TrimRight(cfg.Host, "/")
	if cfg.ResolveHost != nil {
		baseURL = strings.TrimRight(cfg.ResolveHost(cfg.Host), "/")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: erasureRequestTimeout}
	} else {
		clone := *httpClient
		httpClient = &clone
		if httpClient.Timeout == 0 || httpClient.Timeout > erasureRequestTimeout {
			httpClient.Timeout = erasureRequestTimeout
		}
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("PostHog erasure redirect refused")
	}
	return &ErasureClient{baseURL: baseURL, projectID: cfg.ProjectID, region: ErasureRegionForHost(cfg.Host), apiKey: cfg.PersonalAPIKey, http: httpClient}, nil
}

func (c *ErasureClient) RequestPersonDeletion(ctx context.Context, projectID, region string, distinctIDs []string) (ErasureDeleteResult, error) {
	if projectID != c.projectID || region != c.region {
		return ErasureDeleteResult{}, &ErasureProviderError{Class: "target_mismatch"}
	}
	if len(distinctIDs) == 0 || len(distinctIDs) > 1000 {
		return ErasureDeleteResult{}, fmt.Errorf("PostHog erasure requires 1..1000 distinct IDs")
	}
	payload := struct {
		DistinctIDs      []string `json:"distinct_ids"`
		DeleteEvents     bool     `json:"delete_events"`
		DeleteRecordings bool     `json:"delete_recordings"`
		KeepPerson       bool     `json:"keep_person"`
	}{distinctIDs, true, true, false}
	body, err := json.Marshal(payload)
	if err != nil {
		return ErasureDeleteResult{}, fmt.Errorf("encode PostHog erasure request: %w", err)
	}
	endpoint := fmt.Sprintf("%s/api/projects/%s/persons/bulk_delete/", c.baseURL, url.PathEscape(c.projectID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ErasureDeleteResult{}, fmt.Errorf("build PostHog erasure request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "redirect refused") {
			return ErasureDeleteResult{}, &ErasureProviderError{Class: "redirect_refused"}
		}
		return ErasureDeleteResult{}, &ErasureProviderError{Class: "transport"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, erasureResponseLimit+1))
	if err != nil {
		return ErasureDeleteResult{}, &ErasureProviderError{Class: "read_failed"}
	}
	if len(raw) > erasureResponseLimit {
		return ErasureDeleteResult{}, fmt.Errorf("PostHog erasure response too large")
	}
	if resp.StatusCode != http.StatusAccepted {
		class := "provider_rejected"
		if resp.StatusCode == http.StatusTooManyRequests {
			class = "rate_limited"
		} else if resp.StatusCode >= 500 {
			class = "provider_unavailable"
		} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			class = "auth"
		}
		return ErasureDeleteResult{}, &ErasureProviderError{Class: class, RetryAfter: boundedRetryAfter(resp.Header.Get("Retry-After"))}
	}
	var decoded struct {
		PersonsFound     int               `json:"persons_found"`
		PersonsDeleted   int               `json:"persons_deleted"`
		EventsQueued     *bool             `json:"events_queued_for_deletion"`
		RecordingsQueued *bool             `json:"recordings_queued_for_deletion"`
		DeletionErrors   []json.RawMessage `json:"deletion_errors"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.EventsQueued == nil || decoded.RecordingsQueued == nil || !*decoded.EventsQueued || !*decoded.RecordingsQueued || len(decoded.DeletionErrors) != 0 {
		return ErasureDeleteResult{}, fmt.Errorf("invalid deletion response from PostHog")
	}
	return ErasureDeleteResult{decoded.PersonsFound, decoded.PersonsDeleted, *decoded.EventsQueued, *decoded.RecordingsQueued}, nil
}

func boundedRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	d := time.Duration(seconds) * time.Second
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
