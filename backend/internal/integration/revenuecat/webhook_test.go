package revenuecat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassifyAccessChangeIsBoundedAndFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		eventType string
		want      AccessChange
		ok        bool
	}{
		{name: "initial purchase grants", eventType: "INITIAL_PURCHASE", want: AccessChange{EventType: "INITIAL_PURCHASE"}, ok: true},
		{name: "renewal grants", eventType: "RENEWAL", want: AccessChange{EventType: "RENEWAL"}, ok: true},
		{name: "non renewing purchase grants", eventType: "NON_RENEWING_PURCHASE", want: AccessChange{EventType: "NON_RENEWING_PURCHASE"}, ok: true},
		{name: "product change grants", eventType: "PRODUCT_CHANGE", want: AccessChange{EventType: "PRODUCT_CHANGE"}, ok: true},
		{name: "subscription extended grants", eventType: "SUBSCRIPTION_EXTENDED", want: AccessChange{EventType: "SUBSCRIPTION_EXTENDED"}, ok: true},
		{name: "temporary entitlement grant grants", eventType: "TEMPORARY_ENTITLEMENT_GRANT", want: AccessChange{EventType: "TEMPORARY_ENTITLEMENT_GRANT"}, ok: true},
		{name: "expiration revokes", eventType: "EXPIRATION", want: AccessChange{EventType: "EXPIRATION", Revoked: true}, ok: true},
		{name: "subscription paused revokes", eventType: "SUBSCRIPTION_PAUSED", want: AccessChange{EventType: "SUBSCRIPTION_PAUSED", Revoked: true}, ok: true},
		{name: "test delivery changes nothing", eventType: "TEST"},
		{name: "cancellation leaves entitlement in place", eventType: "CANCELLATION"},
		{name: "billing issue leaves entitlement in place", eventType: "BILLING_ISSUE"},
		{name: "uncancellation only toggles renewal", eventType: "UNCANCELLATION"},
		{name: "transfer changes ownership not access", eventType: "TRANSFER"},
		{name: "unknown type is fail closed", eventType: "SOMETHING_NEW"},
		{name: "empty type is fail closed", eventType: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClassifyAccessChange(tc.eventType)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestVerifyWebhook(t *testing.T) {
	now := time.Unix(1800000000, 0)
	body := []byte("{ \"event\": {} }")
	sign := func(ts int64) string {
		stamp := strconv.FormatInt(ts, 10)
		mac := hmac.New(sha256.New, []byte("signing"))
		mac.Write([]byte(stamp + "."))
		mac.Write(body)
		return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	}
	for _, tc := range []struct {
		name, auth, signature, secret string
		body                          []byte
		valid                         bool
	}{
		{"valid", "Bearer auth", sign(now.Unix()), "signing", body, true},
		{"bad auth", "Bearer bad", sign(now.Unix()), "signing", body, false},
		{"missing signature", "Bearer auth", "", "signing", body, false},
		{"missing secret", "Bearer auth", sign(now.Unix()), "", body, false},
		{"old", "Bearer auth", sign(now.Unix() - 301), "signing", body, false},
		{"future", "Bearer auth", sign(now.Unix() + 301), "signing", body, false},
		{"changed body", "Bearer auth", sign(now.Unix()), "signing", []byte(`{"event":{}}`), false},
		{"malformed", "Bearer auth", "t=x,v1=x", "signing", body, false},
		{"duplicate timestamp", "Bearer auth", sign(now.Unix()) + ",t=1800000000", "signing", body, false},
		{"empty duplicate timestamp", "Bearer auth", "t=," + sign(now.Unix()), "signing", body, false},
		{"empty duplicate signature", "Bearer auth", "v1=," + sign(now.Unix()), "signing", body, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyWebhook(tc.body, tc.auth, tc.signature, "Bearer auth", tc.secret, now)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected verification: %v", err)
			}
		})
	}
	if err := VerifyWebhook(body, "", sign(now.Unix()), "", "signing", now); err == nil {
		t.Fatal("empty configured auth accepted")
	}
}
