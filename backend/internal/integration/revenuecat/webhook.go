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

// AccessChange describes a webhook delivery whose event type indicates the customer's
// entitlement state changed: a grant was applied or access was revoked.
type AccessChange struct {
	EventType string
	Revoked   bool
}

// ClassifyAccessChange maps a RevenueCat webhook event type onto the access change it signals. It
// is a pure, total function over a bounded vocabulary so a raw payload value can never reach an
// analytics property or a Prometheus label unreviewed; an unrecognised type — or one that does not
// change entitlement — returns ok=false and the caller emits nothing (fail-closed).
//
// The classification is deliberately conservative. CANCELLATION (auto-renew switched off) and
// BILLING_ISSUE (billing grace period) leave the current entitlement in place, UNCANCELLATION only
// toggles renewal, TRANSFER moves ownership, and TEST is a delivery probe — none of them is treated
// as an access change. Refund-driven revocation arrives as CANCELLATION and is instrumented by the
// refund/revocation path, not here.
func ClassifyAccessChange(eventType string) (AccessChange, bool) {
	switch eventType {
	case "INITIAL_PURCHASE", "RENEWAL", "NON_RENEWING_PURCHASE", "PRODUCT_CHANGE", "SUBSCRIPTION_EXTENDED", "TEMPORARY_ENTITLEMENT_GRANT":
		return AccessChange{EventType: eventType}, true
	case "EXPIRATION", "SUBSCRIPTION_PAUSED":
		return AccessChange{EventType: eventType, Revoked: true}, true
	default:
		return AccessChange{}, false
	}
}

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
