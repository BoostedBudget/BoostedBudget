package revenuecat

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrWebhookVerification = errors.New("RevenueCat webhook verification failed")

// VerifyWebhook requires both configured Authorization and HMAC verification.
// Pass the exact unparsed body and X-RevenueCat-Webhook-Signature header.
func VerifyWebhook(body []byte, authorization, signature, expectedAuthorization, signingSecret string, now time.Time) error {
	if expectedAuthorization == "" || signingSecret == "" || subtle.ConstantTimeCompare([]byte(authorization), []byte(expectedAuthorization)) != 1 {
		return ErrWebhookVerification
	}
	parts := map[string]string{}
	for _, part := range strings.Split(signature, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		_, duplicate := parts[key]
		if !ok || duplicate {
			return ErrWebhookVerification
		}
		parts[key] = value
	}
	stamp, err := strconv.ParseInt(parts["t"], 10, 64)
	if err != nil || stamp < now.Unix()-300 || stamp > now.Unix()+300 {
		return ErrWebhookVerification
	}
	expected, err := hex.DecodeString(parts["v1"])
	if err != nil || len(expected) != sha256.Size {
		return ErrWebhookVerification
	}
	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write([]byte(parts["t"] + "."))
	_, _ = mac.Write(body)
	if !hmac.Equal(expected, mac.Sum(nil)) {
		return ErrWebhookVerification
	}
	return nil
}
