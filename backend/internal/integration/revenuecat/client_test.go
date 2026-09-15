package revenuecat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnsureCustomerCreatesOrConfirmsExisting(t *testing.T) {
	for _, status := range []int{201, 409, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					if r.URL.Path != "/v2/projects/proj/customers" {
						t.Error(r.URL.Path)
					}
					var body map[string]string
					if json.NewDecoder(r.Body).Decode(&body) != nil || body["id"] != "clerk_id" {
						t.Error("wrong customer payload")
					}
					w.WriteHeader(status)
					return
				}
				if r.Method != "GET" || r.URL.Path != "/v2/projects/proj/customers/clerk_id" {
					t.Error("wrong conflict confirmation")
				}
				_, _ = fmt.Fprint(w, `{"id":"clerk_id"}`)
			})
			err := c.EnsureCustomer(context.Background(), "clerk_id")
			if (status == 429) != (err != nil) {
				t.Fatalf("status %d error %v", status, err)
			}
		})
	}
}
func TestEntitlementCatalogCoalescedAndRefreshesUnknownID(t *testing.T) {
	var catalogCalls atomic.Int32
	var changed atomic.Bool
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/entitlements") {
			catalogCalls.Add(1)
			if changed.Load() {
				_, _ = fmt.Fprint(w, `{"object":"list","items":[{"id":"two","lookup_key":"paid_premium"}],"next_page":null}`)
			} else {
				_, _ = fmt.Fprint(w, `{"object":"list","items":[{"id":"one","lookup_key":"paid_premium"}],"next_page":null}`)
			}
			return
		}
		id := "one"
		if changed.Load() {
			id = "two"
		}
		_, _ = fmt.Fprintf(w, `{"object":"list","items":[{"entitlement_id":%q,"expires_at":null}],"next_page":null}`, id)
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.FetchEntitlements(context.Background(), "user"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if catalogCalls.Load() != 1 {
		t.Fatalf("catalog fetched %d times", catalogCalls.Load())
	}
	changed.Store(true)
	if _, err := c.FetchEntitlements(context.Background(), "user"); err != nil {
		t.Fatal(err)
	}
	if catalogCalls.Load() != 2 {
		t.Fatalf("unknown id did not refresh catalog: %d", catalogCalls.Load())
	}
}

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := NewClient(Config{BaseURL: server.URL, ProjectID: "proj", SecretAPIKey: "secret", GrantAPIKey: "grant", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEntitlementCatalogCoalescesConcurrentUnknownIDs(t *testing.T) {
	var changed atomic.Bool
	var catalogCalls, arrived atomic.Int32
	release := make(chan struct{})
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		id := "old"
		if changed.Load() {
			id = "new"
		}
		if strings.HasSuffix(r.URL.Path, "/entitlements") {
			catalogCalls.Add(1)
			_, _ = fmt.Fprintf(w, `{"object":"list","items":[{"id":%q,"lookup_key":"paid_premium"}],"next_page":null}`, id)
			return
		}
		if changed.Load() {
			if arrived.Add(1) == 8 {
				close(release)
			}
			<-release
		}
		_, _ = fmt.Fprintf(w, `{"object":"list","items":[{"entitlement_id":%q,"expires_at":null}],"next_page":null}`, id)
	})
	if _, err := c.FetchEntitlements(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	changed.Store(true)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.FetchEntitlements(context.Background(), "u"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if catalogCalls.Load() != 2 {
		t.Fatalf("expected one coalesced reload, got %d total calls", catalogCalls.Load())
	}
}

func TestFetchEntitlementsResolvesEveryPage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing key")
		}
		switch r.URL.Path {
		case "/v2/projects/proj/entitlements":
			if r.URL.Query().Get("starting_after") == "one" {
				_, _ = fmt.Fprint(w, `{"object":"list","items":[{"id":"two","lookup_key":"comp_household_5"}],"next_page":null}`)
			} else {
				_, _ = fmt.Fprint(w, `{"object":"list","items":[{"id":"one","lookup_key":"paid_premium"}],"next_page":"/v2/projects/proj/entitlements?starting_after=one"}`)
			}
		case "/v2/projects/proj/customers/user_1/active_entitlements":
			if r.URL.Query().Get("starting_after") == "one" {
				_, _ = fmt.Fprint(w, `{"object":"list","items":[{"entitlement_id":"two","expires_at":null}],"next_page":null}`)
			} else {
				_, _ = fmt.Fprint(w, `{"object":"list","items":[{"entitlement_id":"one","expires_at":1800000000123}],"next_page":"/v2/projects/proj/customers/user_1/active_entitlements?starting_after=one"}`)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	got, err := c.FetchEntitlements(context.Background(), "user_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].LookupKey != "paid_premium" || got[0].ExpiresAt.UnixMilli() != 1800000000123 || got[1].ExpiresAt != nil {
		t.Fatalf("wrong entitlements: %+v", got)
	}
}

func TestPaginationAndMalformedDataFailClosed(t *testing.T) {
	for _, body := range []string{
		`{"object":"list","items":[],"next_page":"https://evil.example/steal"}`,
		`{"object":"list","items":[],"next_page":"/v2/projects/other/entitlements"}`,
		`{"object":"list","items":[],"next_page":"/v2/projects/proj/entitlements"}`,
		`{"object":"list","items":[],"next_page":"/v2/projects/proj/customers/another/subscriptions"}`,
		`{"object":"list","items":[{"entitlement_id":"unknown","expires_at":null}]}`,
		`{"object":"list","items":[{"entitlement_id":"one"}]}`,
		`{"object":"list","items":[{"entitlement_id":"one","expires_at":"bad"}]}`,
		`{"object":"list","items":[{"entitlement_id":"one","expires_at":0}]}`,
		`{}`, `null`, `{"object":"list","items":null}`, `{"object":"list","items":[]} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/entitlements") {
					_, _ = fmt.Fprint(w, `{"object":"list","items":[{"id":"one","lookup_key":"paid_premium"}],"next_page":null}`)
					return
				}
				_, _ = fmt.Fprint(w, body)
			})
			if _, err := c.FetchEntitlements(context.Background(), "user_1"); err == nil {
				t.Fatal("accepted unsafe response")
			}
		})
	}
}

func TestHTTPFailuresAndCancellation(t *testing.T) {
	for _, status := range []int{404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "12")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, "secret customer information")
			})
			_, err := c.FetchSubscriptions(context.Background(), "user_1")
			var he *HTTPError
			if !errors.As(err, &he) || he.StatusCode != status || he.RetryAfter != 12*time.Second {
				t.Fatalf("wrong error: %v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("exposed response body")
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("canceled request sent") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.FetchSubscriptions(ctx, "user_1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestGrantRevokeDeleteWireRequests(t *testing.T) {
	var requests []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == "DELETE" {
			if r.Header.Get("Authorization") != "Bearer secret" {
				t.Error("wrong deletion key")
			}
			w.WriteHeader(204)
			return
		}
		if r.Header.Get("Authorization") != "Bearer grant" {
			t.Error("wrong grant key")
		}
		if strings.HasSuffix(r.URL.Path, "/promotional") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(requests) == 1 {
				if body["end_time_ms"] != float64(1800000000123) || len(body) != 1 {
					t.Errorf("wrong finite grant %v", body)
				}
			} else if body["duration"] != "lifetime" || len(body) != 1 {
				t.Errorf("wrong permanent grant %v", body)
			}
		}
		w.WriteHeader(201)
		_, _ = fmt.Fprint(w, `{}`)
	})
	end := time.UnixMilli(1800000000123)
	for _, err := range []error{c.Grant(context.Background(), "user_1", "comp_premium", &end), c.Grant(context.Background(), "user_1", "comp_premium", nil), c.Revoke(context.Background(), "user_1", "comp_premium"), c.DeleteCustomer(context.Background(), "user_1")} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"POST /v1/subscribers/user_1/entitlements/comp_premium/promotional", "POST /v1/subscribers/user_1/entitlements/comp_premium/promotional", "POST /v1/subscribers/user_1/entitlements/comp_premium/revoke_promotionals", "DELETE /v2/projects/proj/customers/user_1"}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Fatal(requests)
	}
}

func TestFetchSubscriptionsPreservesProviderReportedIdentifier(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"object":"list","items":[{"id":"sub_rc","product_id":"prod_rc","store_subscription_identifier":"txn_paddle","status":"active","store":"paddle","environment":"production","auto_renewal_status":"will_not_renew","current_period_ends_at":1800000000123,"ends_at":null}],"next_page":null}`)
	})
	subs, err := c.FetchSubscriptions(context.Background(), "user_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].StoreSubscriptionIdentifier != "txn_paddle" || subs[0].ID != "sub_rc" || subs[0].CurrentPeriodEndsAt.UnixMilli() != 1800000000123 {
		t.Fatalf("wrong billing: %+v", subs)
	}
}

func TestFetchSubscriptionsReadsManagementURL(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"object":"list","items":[{"id":"sub_rc","product_id":"prod_rc","store_subscription_identifier":"sub_paddle","status":"active","store":"paddle","environment":"production","auto_renewal_status":"will_renew","current_period_ends_at":1800000000123,"ends_at":null,"management_url":"https://paddle.example/manage"}],"next_page":null}`)
	})
	subs, err := c.FetchSubscriptions(context.Background(), "user_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].ManagementURL != "https://paddle.example/manage" {
		t.Fatalf("wrong management url: %+v", subs)
	}
}

func TestCustomerExistsConfirmsOnly404AsAbsent(t *testing.T) {
	for _, tc := range []struct {
		status          int
		body            string
		exists, wantErr bool
	}{
		{200, `{"id":"user_1"}`, true, false}, {404, `{}`, false, false}, {423, `{}`, false, true}, {500, `{}`, false, true}, {200, `{"id":"wrong"}`, false, true},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v2/projects/proj/customers/user_1" {
					t.Errorf("wrong customer readback %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			})
			exists, err := c.CustomerExists(context.Background(), "user_1")
			if (err != nil) != tc.wantErr || exists != tc.exists {
				t.Fatalf("exists=%v error=%v", exists, err)
			}
		})
	}
}

func TestRedirectDoesNotForwardSecret(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if _, err := c.FetchSubscriptions(context.Background(), "user_1"); err == nil {
		t.Fatal("followed redirect")
	}
	if reached {
		t.Fatal("secret forwarded")
	}
}

func TestSubscriptionEnvironmentPersistsAcrossPagination(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("environment") != "sandbox" {
			t.Error("missing environment filter")
		}
		if requests == 1 {
			_, _ = fmt.Fprint(w, `{"object":"list","items":[],"next_page":"/v2/projects/proj/customers/user_1/subscriptions?starting_after=one"}`)
		} else {
			_, _ = fmt.Fprint(w, `{"object":"list","items":[],"next_page":null}`)
		}
	}))
	defer server.Close()
	c, err := NewClient(Config{BaseURL: server.URL, ProjectID: "proj", SecretAPIKey: "key", Environment: "sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchSubscriptions(context.Background(), "user_1"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatal(requests)
	}
	if _, err := NewClient(Config{ProjectID: "proj", SecretAPIKey: "key", Environment: "invalid"}); err == nil {
		t.Fatal("invalid environment accepted")
	}
}
