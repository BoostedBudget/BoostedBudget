package posthog

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1652 Todo 13 (i) delivery, (ii) PII and (iii) consent — one consolidated, table-driven
// guard over every backend event this plan introduced or re-homed.
//
// WHY THIS EXISTS ALONGSIDE THE PER-CAPTURE TESTS. Todos 6-12 added a real-Gateway assertion at
// each emission site (`internal/services/revenuecat_analytics_test.go`, the C9 chain test, the
// household projection test, ...). Those prove each *path* emits. This table proves the *contract*
// for every event in one place: that a generated contract for the event name exists, is
// consent-gated, validates the exact property set the plan specifies, delivers it to the transport
// under the canonical subject, and can never carry the forbidden identifier/reason set. A wrong
// catalog entry, a renamed event, or a re-introduced PII key fails here even when no per-path test
// happens to exercise that event.
//
// It drives the REAL Gateway + AuthorityResolver (the `recordingTransport`/`stubAuthority` harness
// from gateway_test.go), not a mock: a mock would bypass contract validation and property
// stripping, which is the entire surface under test.
//
// The four Paddle events emitted through `internal/queue/jobs/subscription_webhook.go` are covered
// by their own table below (Paddle is pre-#1652 but the plan's PII guard names them explicitly), and
// end-to-end from the real worker in
// `internal/queue/jobs/subscription_webhook_paddle_pii_guard_test.go`.

// forbiddenDeliveryKeys is the identifier/reason set no #1652 event may carry. It deliberately
// omits "tier": the household projection and the admin-override events legitimately carry a bounded
// tier, and tier is a bounded product dimension rather than an identifier.
var forbiddenDeliveryKeys = []string{
	"reason", "actor_id", "email", "amount", "account_id", "transaction_id",
	"item_id", "customer_id", "plaid_item_id", "household_id", "user_id",
}

// forbiddenKeysIn returns the forbidden keys present in a transport-boundary property map. It is
// the single implementation the guards and their meta-test share, so proving it detects a leaked
// key proves the guards that call it would fail on one.
func forbiddenKeysIn(props map[string]interface{}) []string {
	var found []string
	for _, forbidden := range forbiddenDeliveryKeys {
		if _, ok := props[forbidden]; ok {
			found = append(found, forbidden)
		}
	}
	return found
}

type deliveryCase struct {
	name      string
	payload   TypedEvent
	wantEvent EventName
	wantProps map[string]interface{}
}

// deliveryCases enumerates every backend event the #1652 plan introduced or re-homed:
// the 13 catalog entries introduced by #1652, the reused `household_over_capacity_entered`
// (promoted to an active bounded contract by this plan), and the two `_server` admin-override
// events re-homed by this plan. 16 events in total.
func deliveryCases() []deliveryCase {
	return []deliveryCase{
		{
			name:      "revenuecat_operation_queued",
			payload:   RevenueCatOperationQueuedPayload{Class: RevenueCatOperationQueuedClassComp, Entitlement: RevenueCatOperationQueuedEntitlementCompPremium},
			wantEvent: EventRevenueCatOperationQueued,
			wantProps: map[string]interface{}{"class": "comp", "entitlement": "comp_premium", "revoke": false},
		},
		{
			name:      "revenuecat_operation_applied",
			payload:   RevenueCatOperationAppliedPayload{Class: RevenueCatOperationAppliedClassComp, Entitlement: RevenueCatOperationAppliedEntitlementCompPremium},
			wantEvent: EventRevenueCatOperationApplied,
			wantProps: map[string]interface{}{"class": "comp", "entitlement": "comp_premium", "revoke": false},
		},
		{
			name:      "revenuecat_operation_failed",
			payload:   RevenueCatOperationFailedPayload{Class: RevenueCatOperationFailedClassTrial, Entitlement: RevenueCatOperationFailedEntitlementTrialPremium},
			wantEvent: EventRevenueCatOperationFailed,
			wantProps: map[string]interface{}{"class": "trial", "entitlement": "trial_premium", "revoke": false},
		},
		{
			name:      "revenuecat_webhook_access_changed",
			payload:   RevenueCatWebhookAccessChangedPayload{EventType: RevenueCatWebhookAccessChangedEventTypeRENEWAL, Revoked: false},
			wantEvent: EventRevenueCatWebhookAccessChanged,
			wantProps: map[string]interface{}{"event_type": "RENEWAL", "revoked": false},
		},
		{
			name:      "revenuecat_access_inactive",
			payload:   RevenueCatAccessInactivePayload{PendingOperations: 0},
			wantEvent: EventRevenueCatAccessInactive,
			wantProps: map[string]interface{}{"pending_operations": int64(0)},
		},
		{
			name:      "revenuecat_retention_armed",
			payload:   RevenueCatRetentionArmedPayload{DeferredForPendingOperations: false},
			wantEvent: EventRevenueCatRetentionArmed,
			wantProps: map[string]interface{}{"deferred_for_pending_operations": false},
		},
		{
			name:      "revenuecat_plaid_cleanup_disconnected",
			payload:   RevenueCatPlaidCleanupDisconnectedPayload{MarkerCleared: true},
			wantEvent: EventRevenueCatPlaidCleanupDisconnected,
			wantProps: map[string]interface{}{"marker_cleared": true},
		},
		{
			name:      "revenuecat_comp_expired",
			payload:   RevenueCatCompExpiredPayload{Entitlement: RevenueCatCompExpiredEntitlementCompPremium},
			wantEvent: EventRevenueCatCompExpired,
			wantProps: map[string]interface{}{"entitlement": "comp_premium"},
		},
		{
			name:      "revenuecat_paid_grant_lapsed",
			payload:   RevenueCatPaidGrantLapsedPayload{Entitlement: RevenueCatPaidGrantLapsedEntitlementPaidPremium},
			wantEvent: EventRevenueCatPaidGrantLapsed,
			wantProps: map[string]interface{}{"entitlement": "paid_premium"},
		},
		{
			name:      "revenuecat_trial_started",
			payload:   RevenueCatTrialStartedPayload{Entitlement: RevenueCatTrialStartedEntitlementTrialPremium},
			wantEvent: EventRevenueCatTrialStarted,
			wantProps: map[string]interface{}{"entitlement": "trial_premium"},
		},
		{
			name:      "revenuecat_trial_ended",
			payload:   RevenueCatTrialEndedPayload{Entitlement: RevenueCatTrialEndedEntitlementTrialPremium, Converted: true},
			wantEvent: EventRevenueCatTrialEnded,
			wantProps: map[string]interface{}{"entitlement": "trial_premium", "converted": true},
		},
		{
			name:      "revenuecat_access_revoked",
			payload:   RevenueCatAccessRevokedPayload{Entitlement: RevenueCatAccessRevokedEntitlementPaidPremium, Source: RevenueCatAccessRevokedSourceRefund},
			wantEvent: EventRevenueCatAccessRevoked,
			wantProps: map[string]interface{}{"entitlement": "paid_premium", "source": "refund"},
		},
		{
			name:      "household_entitlement_projected",
			payload:   HouseholdEntitlementProjectedPayload{Tier: HouseholdEntitlementProjectedTierHousehold2, SeatCount: 2, SeatsIncluded: 2},
			wantEvent: EventHouseholdEntitlementProjected,
			wantProps: map[string]interface{}{"tier": "household_2", "seat_count": int64(2), "seats_included": int64(2)},
		},
		{
			name:      "household_over_capacity_entered",
			payload:   HouseholdOverCapacityEnteredPayload{ActiveCount: 4, SeatLimit: 3},
			wantEvent: EventHouseholdOverCapacityEntered,
			wantProps: map[string]interface{}{"active_count": int64(4), "seat_limit": int64(3)},
		},
		{
			name:      "admin_subscription_override_applied_server",
			payload:   AdminSubscriptionOverrideAppliedServerPayload{Tier: AdminSubscriptionOverrideAppliedServerTierPremium, HadExpiry: false},
			wantEvent: EventAdminSubscriptionOverrideAppliedServer,
			wantProps: map[string]interface{}{"tier": "premium", "had_expiry": false},
		},
		{
			name:      "admin_subscription_override_removed_server",
			payload:   AdminSubscriptionOverrideRemovedServerPayload{Tier: AdminSubscriptionOverrideRemovedServerTierHousehold3, HadExpiry: true},
			wantEvent: EventAdminSubscriptionOverrideRemovedServer,
			wantProps: map[string]interface{}{"tier": "household_3", "had_expiry": true},
		},
	}
}

// TestEveryRevenueCatLifecycleEventDeliversExactlyItsBoundedContract is the delivery + PII guard.
// For every event it drives the REAL Gateway + AuthorityResolver to a capturing transport and
// asserts the event name, the exact normalized property set, and the absence of every forbidden
// key. An event whose catalog contract is missing, deprecated, operational, or mis-specified never
// reaches the transport, so the `require.Len` fails.
func TestEveryRevenueCatLifecycleEventDeliversExactlyItsBoundedContract(t *testing.T) {
	for _, tc := range deliveryCases() {
		t.Run(tc.name, func(t *testing.T) {
			transport := &recordingTransport{}
			gateway := testGateway(transport, stubAuthority{subject: "canonical-subject", granted: true})

			gateway.CaptureEvent(context.Background(), "internal-user", tc.payload)

			require.Len(t, transport.captures, 1, "the event must survive the real Gateway contract")
			captured := transport.captures[0]
			assert.Equal(t, "canonical-subject", captured.distinctID)
			assert.Equal(t, string(tc.wantEvent), captured.event)
			assert.Equal(t, tc.wantProps, captured.properties, "exact property set")
			assert.Empty(t, forbiddenKeysIn(captured.properties), "forbidden key reached the transport")
		})
	}
}

// TestEveryRevenueCatLifecycleEventIsConsentGatedByTheGatewayAuthority is the consent guard. The
// #1652 events are gateway-routed, so the legacy `GDPRClient`/`ConsentChecker` matrix in
// `services/revenuecat_analytics_test.go` does NOT cover them. Each event is driven with an
// opted-out authority and a failing authority; neither may reach the transport.
func TestEveryRevenueCatLifecycleEventIsConsentGatedByTheGatewayAuthority(t *testing.T) {
	for _, tc := range deliveryCases() {
		t.Run(tc.name+"/opted_out", func(t *testing.T) {
			transport := &recordingTransport{}
			gateway := testGateway(transport, stubAuthority{})

			gateway.CaptureEvent(context.Background(), "internal-user", tc.payload)

			assert.Empty(t, transport.captures, "an opted-out user's event must never reach the transport")
		})
		t.Run(tc.name+"/authority_unavailable", func(t *testing.T) {
			transport := &recordingTransport{}
			gateway := testGateway(transport, stubAuthority{err: errors.New("privacy unavailable")})

			gateway.CaptureEvent(context.Background(), "internal-user", tc.payload)

			assert.Empty(t, transport.captures, "a failed consent lookup must fail closed")
		})
	}
}

// paddleEmitterCases carries the EXACT property maps the four Paddle producers pass to
// `Capture(...)` in `internal/queue/jobs/subscription_webhook.go`:
//
//   - subscription_created  → {tier, status, subscription_id}   (subscription_webhook.go:754)
//   - subscription_cancelled→ nil                                (subscription_webhook.go:967)
//   - payment_succeeded     → {amount, currency, transaction_id} (subscription_webhook.go:1415)
//   - payment_failed        → {transaction_id}                   (subscription_webhook.go:1458)
//
// The `subscription_id`/`amount`/`transaction_id` keys are exactly what the forbidden set names,
// so this table proves the Gateway strips them before delivery. The worker-side twin in the jobs
// package proves the same thing from the real emitter rather than from this copy.
var paddleEmitterCases = []struct {
	name  string
	event EventName
	props map[string]interface{}
}{
	{"subscription_created", EventSubscriptionCreated, map[string]interface{}{"tier": "premium", "status": "active", "subscription_id": "sub_123"}},
	{"subscription_cancelled", EventSubscriptionCancelled, nil},
	{"payment_succeeded", EventPaymentSucceeded, map[string]interface{}{"amount": 899, "currency": "USD", "transaction_id": "txn_123"}},
	{"payment_failed", EventPaymentFailed, map[string]interface{}{"transaction_id": "txn_123"}},
}

// TestPaddleWebhookEventsReachTheTransportWithoutTheirIdentifierProperties covers the four Paddle
// events the plan's PII guard names explicitly. They are emitted through the legacy `Capture`
// adapter, so the guard captures what the transport actually receives and asserts no forbidden key
// survives — and, because their contracts are legacy/unreviewed, that the map is stripped empty.
func TestPaddleWebhookEventsReachTheTransportWithoutTheirIdentifierProperties(t *testing.T) {
	for _, tc := range paddleEmitterCases {
		t.Run(tc.name, func(t *testing.T) {
			transport := &recordingTransport{}
			gateway := testGateway(transport, stubAuthority{subject: "canonical-subject", granted: true})

			gateway.Capture("internal-user", string(tc.event), tc.props)

			require.Len(t, transport.captures, 1, "the Paddle event must still deliver")
			captured := transport.captures[0]
			assert.Equal(t, string(tc.event), captured.event)
			assert.Empty(t, forbiddenKeysIn(captured.properties), "forbidden key reached the transport")
			assert.Empty(t, captured.properties,
				"a legacy/unreviewed Paddle contract must strip every property, including amount/transaction_id")
		})
	}
}

// TestDeliveryGuardDetectsAForbiddenKey is the failure QA for the delivery/PII guard. It proves the
// shared detection helper reports a leaked key and that the REAL Gateway drops an event carrying
// one, so the guard above cannot pass by accident when a forbidden property is introduced.
func TestDeliveryGuardDetectsAForbiddenKey(t *testing.T) {
	assert.Equal(t, []string{"reason"}, forbiddenKeysIn(map[string]interface{}{"reason": "admin-only explanation"}))
	assert.Equal(t, []string{"amount", "transaction_id"},
		forbiddenKeysIn(map[string]interface{}{"amount": 899, "transaction_id": "txn_123", "currency": "USD"}))
	assert.Empty(t, forbiddenKeysIn(map[string]interface{}{"sync_kind": "initial"}))

	transport := &recordingTransport{}
	gateway := testGateway(transport, stubAuthority{subject: "canonical-subject", granted: true})
	// financial_data_sync_completed has a REVIEWED contract, so an extra key is rejected outright.
	gateway.Capture("internal-user", string(EventFinancialDataSyncCompleted), map[string]interface{}{
		"sync_kind": "initial",
		"reason":    "admin-only explanation",
	})
	assert.Empty(t, transport.captures, "a forbidden key must never reach the transport")
}
