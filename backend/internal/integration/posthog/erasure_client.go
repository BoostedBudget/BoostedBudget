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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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

type ErasurePersonResolution struct {
	PersonUUIDs             []string
	HasUnresolvedIdentities bool
}

func (c *ErasureClient) ResolvePersonUUIDs(ctx context.Context, projectID, region string, distinctIDs []string) (ErasurePersonResolution, error) {
	if err := c.checkTarget(projectID, region); err != nil {
		return ErasurePersonResolution{}, err
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(distinctIDs))
	hasUnresolved := false
	for _, distinctID := range distinctIDs {
		if err := ctx.Err(); err != nil {
			return ErasurePersonResolution{}, &ErasureProviderError{Class: "transport"}
		}
		query := url.Values{"distinct_id": []string{distinctID}}
		var page struct {
			Results []struct {
				UUID string `json:"uuid"`
			} `json:"results"`
			Next *string `json:"next"`
		}
		if err := c.getJSON(ctx, "/persons/?"+query.Encode(), &page); err != nil {
			return ErasurePersonResolution{}, err
		}
		if page.Next != nil || len(page.Results) > 1 {
			return ErasurePersonResolution{}, &ErasureProviderError{Class: "invalid_response"}
		}
		if len(page.Results) == 0 {
			hasUnresolved = true
		}
		for _, person := range page.Results {
			if _, err := uuid.Parse(person.UUID); err != nil {
				return ErasurePersonResolution{}, &ErasureProviderError{Class: "invalid_response"}
			}
			if !seen[person.UUID] {
				seen[person.UUID] = true
				result = append(result, person.UUID)
			}
		}
	}
	return ErasurePersonResolution{PersonUUIDs: result, HasUnresolvedIdentities: hasUnresolved}, nil
}

type ErasureDeletionObservation struct {
	Found    bool
	Complete bool
}

type erasureDeletionRecord struct {
	CreatedAt      time.Time
	Status         string
	DeleteVerified *string
}

func (c *ErasureClient) CapturePersonEventDeletionBaseline(ctx context.Context, projectID, region string, personUUIDs []string) (map[string]*time.Time, error) {
	if err := c.checkTarget(projectID, region); err != nil {
		return nil, err
	}
	baseline := make(map[string]*time.Time, len(personUUIDs))
	for _, personUUID := range personUUIDs {
		records, err := c.personDeletionRecords(ctx, personUUID)
		if err != nil {
			return nil, err
		}
		if len(records) > 0 {
			created := records[0].CreatedAt
			baseline[personUUID] = &created
		} else {
			baseline[personUUID] = nil
		}
	}
	return baseline, nil
}

func (c *ErasureClient) PersonEventDeletionStatus(ctx context.Context, projectID, region string, personUUIDs []string, baseline map[string]*time.Time, submittedAt, acceptedAt time.Time) (ErasureDeletionObservation, error) {
	if err := c.checkTarget(projectID, region); err != nil {
		return ErasureDeletionObservation{}, err
	}
	allComplete := true
	for _, personUUID := range personUUIDs {
		records, err := c.personDeletionRecords(ctx, personUUID)
		if err != nil {
			return ErasureDeletionObservation{}, err
		}
		relevant := relevantDeletionRecord(records, baseline[personUUID], submittedAt, acceptedAt)
		if relevant == nil {
			return ErasureDeletionObservation{Found: false}, nil
		}
		if relevant.Status != "completed" || relevant.DeleteVerified == nil {
			if relevant.Status != "pending" && relevant.Status != "completed" {
				return ErasureDeletionObservation{}, &ErasureProviderError{Class: "invalid_response"}
			}
			allComplete = false
			continue
		}
		if _, err := time.Parse(time.RFC3339, *relevant.DeleteVerified); err != nil {
			return ErasureDeletionObservation{}, &ErasureProviderError{Class: "invalid_response"}
		}
	}
	return ErasureDeletionObservation{Found: true, Complete: allComplete}, nil
}

func relevantDeletionRecord(records []erasureDeletionRecord, baseline *time.Time, submittedAt, acceptedAt time.Time) *erasureDeletionRecord {
	if baseline != nil {
		for i := range records {
			if records[i].CreatedAt.Equal(*baseline) {
				return &records[i]
			}
		}
		return nil
	}

	earliestCreatedAt := submittedAt.Add(-time.Minute)
	latestCreatedAt := acceptedAt.Add(time.Minute)
	for i := range records {
		createdAt := records[i].CreatedAt
		if !createdAt.Before(earliestCreatedAt) && !createdAt.After(latestCreatedAt) {
			return &records[i]
		}
	}
	return nil
}

func (c *ErasureClient) PersonEventDeletionComplete(ctx context.Context, projectID, region string, personUUIDs []string, submittedAt time.Time) (bool, error) {
	status, err := c.PersonEventDeletionStatus(ctx, projectID, region, personUUIDs, map[string]*time.Time{}, submittedAt, submittedAt.Add(5*time.Minute))
	return status.Found && status.Complete, err
}

func (c *ErasureClient) personDeletionRecords(ctx context.Context, personUUID string) ([]erasureDeletionRecord, error) {
	if _, err := uuid.Parse(personUUID); err != nil {
		return nil, &ErasureProviderError{Class: "invalid_correlation"}
	}
	query := url.Values{"person_uuid": []string{personUUID}, "status": []string{"all"}}
	nextPath := "/persons/deletion_status/?" + query.Encode()
	var records []erasureDeletionRecord
	for nextPath != "" {
		var page struct {
			Next    *string `json:"next"`
			Results []struct {
				PersonUUID     string  `json:"person_uuid"`
				Status         string  `json:"status"`
				CreatedAt      string  `json:"created_at"`
				DeleteVerified *string `json:"delete_verified_at"`
			} `json:"results"`
		}
		if err := c.getJSON(ctx, nextPath, &page); err != nil {
			return nil, err
		}
		for _, candidate := range page.Results {
			if candidate.PersonUUID != personUUID {
				return nil, &ErasureProviderError{Class: "invalid_response"}
			}
			created, err := time.Parse(time.RFC3339, candidate.CreatedAt)
			if err != nil {
				return nil, &ErasureProviderError{Class: "invalid_response"}
			}
			records = append(records, erasureDeletionRecord{
				CreatedAt:      created,
				Status:         candidate.Status,
				DeleteVerified: candidate.DeleteVerified,
			})
		}
		nextPath = ""
		if page.Next != nil {
			var err error
			nextPath, err = c.deletionStatusNextPath(*page.Next)
			if err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(records, func(a, b erasureDeletionRecord) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return records, nil
}

func (c *ErasureClient) deletionStatusNextPath(next string) (string, error) {
	parsed, err := url.Parse(next)
	if err != nil {
		return "", &ErasureProviderError{Class: "invalid_response"}
	}
	if parsed.IsAbs() && (parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), c.privateHost)) {
		return "", &ErasureProviderError{Class: "invalid_response"}
	}
	prefix := "/api/projects/" + url.PathEscape(c.projectID)
	if !strings.HasPrefix(parsed.Path, prefix+"/persons/deletion_status/") {
		return "", &ErasureProviderError{Class: "invalid_response"}
	}
	nextPath := strings.TrimPrefix(parsed.EscapedPath(), prefix)
	if parsed.RawQuery != "" {
		nextPath += "?" + parsed.RawQuery
	}
	return nextPath, nil
}

func (c *ErasureClient) getJSON(ctx context.Context, path string, destination any) error {
	endpoint := fmt.Sprintf("%s/api/projects/%s%s", c.baseURL, url.PathEscape(c.projectID), path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &ErasureProviderError{Class: "request"}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return &ErasureProviderError{Class: "transport"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, erasureResponseLimit+1))
	if err != nil || len(raw) > erasureResponseLimit {
		return &ErasureProviderError{Class: "invalid_response"}
	}
	if resp.StatusCode != http.StatusOK {
		return &ErasureProviderError{Class: classifyHTTPStatus(resp.StatusCode), RetryAfter: boundedRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return &ErasureProviderError{Class: "invalid_response"}
	}
	return nil
}

func (c *ErasureClient) checkTarget(projectID, region string) error {
	if projectID != c.projectID || region != c.region {
		return &ErasureProviderError{Class: "target_mismatch"}
	}
	return nil
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
	baseURL     string
	projectID   string
	region      string
	apiKey      string
	http        *http.Client
	privateHost string
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
	return &ErasureClient{
		baseURL:     baseURL,
		projectID:   cfg.ProjectID,
		region:      ErasureRegionForHost(cfg.Host),
		apiKey:      cfg.PersonalAPIKey,
		http:        httpClient,
		privateHost: strings.ToLower(parsed.Hostname()),
	}, nil
}

func (c *ErasureClient) RequestPersonDeletion(ctx context.Context, projectID, region string, distinctIDs []string) (ErasureDeleteResult, error) {
	if err := c.checkTarget(projectID, region); err != nil {
		return ErasureDeleteResult{}, err
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
		class := classifyHTTPStatus(resp.StatusCode)
		return ErasureDeleteResult{}, &ErasureProviderError{Class: class, RetryAfter: boundedRetryAfter(resp.Header.Get("Retry-After"))}
	}
	var decoded struct {
		PersonsFound     int               `json:"persons_found"`
		PersonsDeleted   int               `json:"persons_deleted"`
		EventsQueued     *bool             `json:"events_queued_for_deletion"`
		RecordingsQueued *bool             `json:"recordings_queued_for_deletion"`
		DeletionErrors   []json.RawMessage `json:"deletion_errors"`
	}
	err = json.Unmarshal(raw, &decoded)
	hasQueueFlags := decoded.EventsQueued != nil && decoded.RecordingsQueued != nil
	if err != nil || !hasQueueFlags || len(decoded.DeletionErrors) != 0 {
		return ErasureDeleteResult{}, fmt.Errorf("invalid deletion response from PostHog")
	}
	zeroMatch := decoded.PersonsFound == 0 && decoded.PersonsDeleted == 0
	if !zeroMatch && (!*decoded.EventsQueued || !*decoded.RecordingsQueued) {
		return ErasureDeleteResult{}, fmt.Errorf("invalid deletion response from PostHog")
	}
	return ErasureDeleteResult{decoded.PersonsFound, decoded.PersonsDeleted, *decoded.EventsQueued, *decoded.RecordingsQueued}, nil
}

func classifyHTTPStatus(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status >= 500:
		return "provider_unavailable"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "auth"
	default:
		return "provider_rejected"
	}
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
