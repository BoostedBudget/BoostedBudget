package revenuecat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

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
