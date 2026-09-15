// Package revenuecat provides server-side subscription authority operations.
package revenuecat

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
	"sync"
	"time"
)

const maxResponseBytes = 4 << 20

type Config struct {
	BaseURL      string
	ProjectID    string
	SecretAPIKey string
	GrantAPIKey  string
	Environment  string
	HTTPClient   *http.Client
}

type Client struct {
	base                                              *url.URL
	projectID, secretAPIKey, grantAPIKey, environment string
	http                                              *http.Client
	catalogMu                                         sync.Mutex
	catalog                                           *entitlementCatalogSnapshot
}

type entitlementCatalogSnapshot struct {
	lookup  map[string]string
	updated time.Time
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.revenuecat.com"
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("invalid RevenueCat API base URL")
	}
	if cfg.ProjectID == "" || cfg.SecretAPIKey == "" {
		return nil, errors.New("RevenueCat project and secret API key are required")
	}
	if cfg.GrantAPIKey == "" {
		cfg.GrantAPIKey = cfg.SecretAPIKey
	}
	if cfg.Environment == "" {
		cfg.Environment = "production"
	}
	if cfg.Environment != "production" && cfg.Environment != "sandbox" {
		return nil, errors.New("invalid RevenueCat environment")
	}
	hc := http.Client{Timeout: 15 * time.Second}
	if cfg.HTTPClient != nil {
		hc = *cfg.HTTPClient
		if hc.Timeout <= 0 {
			hc.Timeout = 15 * time.Second
		}
	}
	// Never forward authorization or a mutation through a provider redirect.
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: base, projectID: cfg.ProjectID, secretAPIKey: cfg.SecretAPIKey, grantAPIKey: cfg.GrantAPIKey, environment: cfg.Environment, http: &hc}, nil
}

// HTTPError intentionally excludes provider bodies and URLs, which may contain PII.
type HTTPError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("RevenueCat HTTP status %d", e.StatusCode) }

type Entitlement struct {
	ID        string     `json:"id"`
	LookupKey string     `json:"lookup_key"`
	ExpiresAt *time.Time `json:"expires_at"`
}
type Subscription struct {
	ID                          string     `json:"id"`
	ProductID                   string     `json:"product_id"`
	StoreSubscriptionIdentifier string     `json:"store_subscription_identifier"`
	Status                      string     `json:"status"`
	Store                       string     `json:"store"`
	Environment                 string     `json:"environment"`
	AutoRenewalStatus           string     `json:"auto_renewal_status"`
	CurrentPeriodEndsAt         *time.Time `json:"current_period_ends_at"`
	EndsAt                      *time.Time `json:"ends_at"`
	// ManagementURL is RevenueCat's self-service link for this subscription. The v2
	// Subscription object populates it for Paddle (and App Store/Play/Amazon/Web
	// Billing) subscriptions; it is empty when RevenueCat has not resolved one.
	ManagementURL string `json:"management_url"`
}
type Snapshot struct {
	Entitlements  []Entitlement
	Subscriptions []Subscription
}

func (c *Client) projectPath() string { return "/v2/projects/" + url.PathEscape(c.projectID) }
func (c *Client) customerPath(id string) string {
	return c.projectPath() + "/customers/" + url.PathEscape(id)
}

// EnsureCustomer establishes the canonical Clerk identity before a card-free grant.
// A conflict is confirmed by reading that exact customer; unrelated failures remain errors.
func (c *Client) EnsureCustomer(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("RevenueCat customer ID is required")
	}
	body, err := json.Marshal(map[string]string{"id": id})
	if err != nil {
		return err
	}
	_, err = c.request(ctx, http.MethodPost, c.projectPath()+"/customers", c.secretAPIKey, body)
	var status *HTTPError
	if !errors.As(err, &status) || status.StatusCode != http.StatusConflict {
		return err
	}
	raw, err := c.request(ctx, http.MethodGet, c.customerPath(id), c.secretAPIKey, nil)
	if err != nil {
		return err
	}
	var customer struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &customer) != nil || customer.ID != id {
		return errors.New("RevenueCat customer conflict could not be confirmed")
	}
	return nil
}

func (c *Client) FetchSnapshot(ctx context.Context, id string) (Snapshot, error) {
	es, err := c.FetchEntitlements(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	ss, err := c.FetchSubscriptions(ctx, id)
	return Snapshot{Entitlements: es, Subscriptions: ss}, err
}

// entitlementCatalog caches only a complete validated catalog, coalescing its
// lower-rate-limit project configuration calls across customer refreshes.
func (c *Client) entitlementCatalog(ctx context.Context, previous *entitlementCatalogSnapshot) (*entitlementCatalogSnapshot, error) {
	c.catalogMu.Lock()
	defer c.catalogMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// If another caller already replaced the observed immutable snapshot, its
	// reload satisfies this caller too, even when the opaque ID remains unknown.
	if c.catalog != nil && time.Since(c.catalog.updated) < 15*time.Minute && (previous == nil || previous != c.catalog) {
		return c.catalog, nil
	}
	catalog, err := c.list(ctx, c.projectPath()+"/entitlements")
	if err != nil {
		return nil, err
	}
	lookup := make(map[string]string, len(catalog))
	for _, raw := range catalog {
		var e struct {
			ID        string `json:"id"`
			LookupKey string `json:"lookup_key"`
		}
		if json.Unmarshal(raw, &e) != nil || e.ID == "" || e.LookupKey == "" {
			return nil, errors.New("invalid RevenueCat entitlement catalog")
		}
		if _, ok := lookup[e.ID]; ok {
			return nil, errors.New("duplicate RevenueCat catalog entitlement")
		}
		lookup[e.ID] = e.LookupKey
	}
	c.catalog = &entitlementCatalogSnapshot{lookup: lookup, updated: time.Now()}
	return c.catalog, nil
}

// FetchEntitlements resolves opaque entitlement IDs through the project catalog.
// An incomplete or malformed response is never an empty grant set.
func (c *Client) FetchEntitlements(ctx context.Context, id string) ([]Entitlement, error) {
	if id == "" {
		return nil, errors.New("RevenueCat customer ID is required")
	}
	catalog, err := c.entitlementCatalog(ctx, nil)
	if err != nil {
		return nil, err
	}
	items, err := c.list(ctx, c.customerPath(id)+"/active_entitlements")
	if err != nil {
		return nil, err
	}
	result := make([]Entitlement, 0, len(items))
	seen := map[string]bool{}
	refreshed := false
	for _, raw := range items {
		var e struct {
			ID        string          `json:"entitlement_id"`
			ExpiresAt json.RawMessage `json:"expires_at"`
		}
		if json.Unmarshal(raw, &e) != nil || e.ID == "" || seen[e.ID] {
			return nil, errors.New("invalid RevenueCat active entitlement")
		}
		if catalog.lookup[e.ID] == "" && !refreshed {
			catalog, err = c.entitlementCatalog(ctx, catalog)
			if err != nil {
				return nil, err
			}
			refreshed = true
		}
		if catalog.lookup[e.ID] == "" {
			return nil, errors.New("invalid RevenueCat active entitlement")
		}
		expiry, err := millis(e.ExpiresAt)
		if err != nil {
			return nil, err
		}
		seen[e.ID] = true
		result = append(result, Entitlement{ID: e.ID, LookupKey: catalog.lookup[e.ID], ExpiresAt: expiry})
	}
	return result, nil
}

// FetchSubscriptions is separate so billing outages need not discard access.
// StoreSubscriptionIdentifier is provider-reported metadata; callers must resolve
// the actual Paddle subscription before issuing Paddle mutations.
func (c *Client) FetchSubscriptions(ctx context.Context, id string) ([]Subscription, error) {
	if id == "" {
		return nil, errors.New("RevenueCat customer ID is required")
	}
	items, err := c.list(ctx, c.customerPath(id)+"/subscriptions")
	if err != nil {
		return nil, err
	}
	result := make([]Subscription, 0, len(items))
	for _, raw := range items {
		var wire struct {
			ID                          string          `json:"id"`
			ProductID                   string          `json:"product_id"`
			StoreSubscriptionIdentifier string          `json:"store_subscription_identifier"`
			Status                      string          `json:"status"`
			Store                       string          `json:"store"`
			Environment                 string          `json:"environment"`
			AutoRenewalStatus           string          `json:"auto_renewal_status"`
			CurrentPeriodEndsAt         json.RawMessage `json:"current_period_ends_at"`
			EndsAt                      json.RawMessage `json:"ends_at"`
			ManagementURL               string          `json:"management_url"`
		}
		if json.Unmarshal(raw, &wire) != nil || wire.ID == "" || wire.Status == "" || wire.Store == "" || wire.Environment == "" {
			return nil, errors.New("invalid RevenueCat subscription")
		}
		period, err := millis(wire.CurrentPeriodEndsAt)
		if err != nil {
			return nil, err
		}
		end, err := millis(wire.EndsAt)
		if err != nil {
			return nil, err
		}
		result = append(result, Subscription{ID: wire.ID, ProductID: wire.ProductID, StoreSubscriptionIdentifier: wire.StoreSubscriptionIdentifier, Status: wire.Status, Store: wire.Store, Environment: wire.Environment, AutoRenewalStatus: wire.AutoRenewalStatus, CurrentPeriodEndsAt: period, EndsAt: end, ManagementURL: wire.ManagementURL})
	}
	return result, nil
}

func millis(raw json.RawMessage) (*time.Time, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	var ms int64
	if len(raw) == 0 || json.Unmarshal(raw, &ms) != nil || ms <= 0 {
		return nil, errors.New("invalid RevenueCat expiry timestamp")
	}
	t := time.UnixMilli(ms).UTC()
	return &t, nil
}

func (c *Client) Grant(ctx context.Context, id, lookup string, end *time.Time) error {
	if id == "" || lookup == "" {
		return errors.New("RevenueCat customer and entitlement are required")
	}
	body := map[string]any{"duration": "lifetime"}
	if end != nil {
		if end.UnixMilli() <= 0 {
			return errors.New("invalid RevenueCat grant expiry")
		}
		body = map[string]any{"end_time_ms": end.UnixMilli()}
	}
	data, _ := json.Marshal(body)
	_, err := c.request(ctx, http.MethodPost, promotionalPath(id, lookup)+"/promotional", c.grantAPIKey, data)
	return err
}
func (c *Client) Revoke(ctx context.Context, id, lookup string) error {
	if id == "" || lookup == "" {
		return errors.New("RevenueCat customer and entitlement are required")
	}
	_, err := c.request(ctx, http.MethodPost, promotionalPath(id, lookup)+"/revoke_promotionals", c.grantAPIKey, nil)
	return err
}
func promotionalPath(id, lookup string) string {
	return "/v1/subscribers/" + url.PathEscape(id) + "/entitlements/" + url.PathEscape(lookup)
}
func (c *Client) DeleteCustomer(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("RevenueCat customer ID is required")
	}
	_, err := c.request(ctx, http.MethodDelete, c.customerPath(id), c.secretAPIKey, nil)
	return err
}

// CustomerExists confirms asynchronous deletion through the canonical customer
// resource. An empty entitlement list does not prove that its PII was erased.
func (c *Client) CustomerExists(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, errors.New("RevenueCat customer ID is required")
	}
	raw, err := c.request(ctx, http.MethodGet, c.customerPath(id), c.secretAPIKey, nil)
	var status *HTTPError
	if errors.As(err, &status) && status.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var customer struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &customer) != nil || customer.ID != id {
		return false, errors.New("RevenueCat customer readback identity mismatch")
	}
	return true, nil
}

func (c *Client) list(ctx context.Context, path string) ([]json.RawMessage, error) {
	next := path
	filtered := strings.HasSuffix(path, "/subscriptions")
	if filtered {
		next += "?environment=" + c.environment
	}
	seen := map[string]bool{}
	items := make([]json.RawMessage, 0)
	for next != "" {
		if len(seen) >= 1000 || seen[next] {
			return nil, errors.New("RevenueCat pagination loop or limit")
		}
		seen[next] = true
		data, err := c.request(ctx, http.MethodGet, next, c.secretAPIKey, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Object   string            `json:"object"`
			Items    []json.RawMessage `json:"items"`
			NextPage *string           `json:"next_page"`
		}
		if json.Unmarshal(data, &page) != nil || page.Object != "list" || page.Items == nil {
			return nil, errors.New("invalid RevenueCat list response")
		}
		items = append(items, page.Items...)
		next = ""
		if page.NextPage != nil && *page.NextPage != "" {
			u, err := url.Parse(*page.NextPage)
			if err != nil {
				return nil, errors.New("invalid RevenueCat pagination URL")
			}
			if u.IsAbs() {
				if u.Scheme != c.base.Scheme || u.Host != c.base.Host {
					return nil, errors.New("unsafe RevenueCat pagination origin")
				}
			} else if u.Host != "" {
				return nil, errors.New("unsafe RevenueCat pagination origin")
			}
			if u.User != nil || u.Fragment != "" || u.EscapedPath() != path {
				return nil, errors.New("unsafe RevenueCat pagination endpoint")
			}
			next = u.RequestURI()
			if filtered {
				query := u.Query()
				query.Set("environment", c.environment)
				u.RawQuery = query.Encode()
				next = u.RequestURI()
			}
		}
	}
	return items, nil
}

func (c *Client) request(ctx context.Context, method, path, key string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.base.String(), "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid RevenueCat request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("RevenueCat transport failure")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("RevenueCat response read failed")
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("RevenueCat response exceeds size limit")
	}
	return data, nil
}
func retryAfter(value string) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		if d := time.Until(date); d > 0 {
			return d
		}
	}
	return 0
}
