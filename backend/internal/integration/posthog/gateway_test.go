package posthog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	posthoggo "github.com/posthog/posthog-go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingTransport struct {
	captures []capturedEvent
	closed   bool
}

type closeFailingClient struct{}

func (closeFailingClient) Enqueue(posthoggo.Message) error { return nil }
func (closeFailingClient) Close() error                    { return errors.New("flush failed") }

type capturedEvent struct {
	distinctID string
	event      string
	properties map[string]interface{}
}

func (r *recordingTransport) Capture(distinctID, event string, properties map[string]interface{}) {
	r.captures = append(r.captures, capturedEvent{distinctID: distinctID, event: event, properties: properties})
}
func (r *recordingTransport) Identify(string, map[string]interface{}) {}
func (r *recordingTransport) Close()                                  { r.closed = true }

type stubAuthority struct {
	subject string
	granted bool
	err     error
}

func (s stubAuthority) WithConsentedSubject(_ context.Context, _ string, emit func(string)) (bool, error) {
	if s.err != nil || !s.granted {
		return false, s.err
	}
	emit(s.subject)
	return true, nil
}

func testGateway(transport Analytics, authority stubAuthority) *Gateway {
	return NewGateway(transport, authority, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestGatewayCaptureEventUsesCanonicalClerkSubject(t *testing.T) {
	transport := &recordingTransport{}
	gateway := testGateway(transport, stubAuthority{subject: "user_clerk", granted: true})
	before := testutil.ToFloat64(analyticsEventsTotal.WithLabelValues(string(EventFinancialDataSyncCompleted)))

	gateway.CaptureEvent(context.Background(), "internal-user", FinancialDataSyncCompletedPayload{SyncKind: FinancialDataSyncCompletedSyncKindInitial})

	require.Len(t, transport.captures, 1)
	assert.Equal(t, "user_clerk", transport.captures[0].distinctID)
	assert.Equal(t, string(EventFinancialDataSyncCompleted), transport.captures[0].event)
	assert.Equal(t, map[string]interface{}{"sync_kind": "initial"}, transport.captures[0].properties)
	after := testutil.ToFloat64(analyticsEventsTotal.WithLabelValues(string(EventFinancialDataSyncCompleted)))
	assert.Equal(t, before+1, after)
}

func TestGatewayFailsClosedForConsentAndIdentityFailures(t *testing.T) {
	cases := []struct {
		name      string
		authority stubAuthority
	}{
		{name: "missing or withdrawn consent", authority: stubAuthority{}},
		{name: "authority lookup failed", authority: stubAuthority{err: errors.New("db unavailable")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := &recordingTransport{}
			gateway := NewGateway(transport, tc.authority, slog.Default())
			gateway.CaptureEvent(context.Background(), "internal-user", FinancialDataSyncCompletedPayload{SyncKind: FinancialDataSyncCompletedSyncKindInitial})
			assert.Empty(t, transport.captures)
		})
	}
}

func TestGatewayRejectsInvalidContractAndNestedPII(t *testing.T) {
	transport := &recordingTransport{}
	gateway := testGateway(transport, stubAuthority{subject: "user_clerk", granted: true})

	gateway.CaptureEvent(context.Background(), "internal-user", FinancialDataSyncCompletedPayload{SyncKind: FinancialDataSyncCompletedSyncKind("bogus")})
	gateway.Capture("internal-user", string(EventFinancialDataSyncCompleted), map[string]interface{}{
		"sync_kind": "initial",
		"nested":    []interface{}{map[string]interface{}{"Email": "secret@example.com"}},
	})

	assert.Empty(t, transport.captures)
}

func TestValidatePropertiesRejectsNestedPIIWithCaseAndCollectionVariants(t *testing.T) {
	contract := EventContract{Properties: map[string]PropertyContract{
		"safe": {Kind: "string"},
	}}
	for _, properties := range []map[string]interface{}{
		{"safe": "ok", "nested": map[string]interface{}{"accountID": "secret"}},
		{"safe": "ok", "nested": []interface{}{map[string]interface{}{"Merchant_Name": "secret"}}},
		{"safe": "ok", "nested": map[interface{}]interface{}{"Email": "secret"}},
	} {
		_, err := validateProperties(contract, properties)
		require.Error(t, err)
	}
	assert.True(t, containsNestedPII(map[string]string{"accountID": "secret"}))
	assert.True(t, containsNestedPII([]map[string]interface{}{{"queryString": "secret"}}))
	for _, key := range []string{"EMAIL", "userID", "accessTOKEN", "nestedUserID"} {
		assert.True(t, containsNestedPII(map[string]interface{}{key: "secret"}), key)
	}
}

func TestGatewayTypedNilPayloadFailsClosed(t *testing.T) {
	transport := &recordingTransport{}
	gateway := testGateway(transport, stubAuthority{subject: "user_clerk", granted: true})
	var event *FinancialDataSyncCompletedPayload
	assert.NotPanics(t, func() { gateway.CaptureEvent(context.Background(), "internal-user", event) })
	assert.Empty(t, transport.captures)
}

func TestGatewayRejectsInactiveContractStatuses(t *testing.T) {
	contract := generatedEventContracts[EventFinancialDataSyncCompleted]
	defer func() { generatedEventContracts[EventFinancialDataSyncCompleted] = contract }()
	transport := &recordingTransport{}
	gateway := testGateway(transport, stubAuthority{subject: "user_clerk", granted: true})
	for _, status := range []string{"future", "deprecated"} {
		updated := contract
		updated.Status = status
		generatedEventContracts[EventFinancialDataSyncCompleted] = updated
		gateway.CaptureEvent(context.Background(), "internal-user", FinancialDataSyncCompletedPayload{SyncKind: FinancialDataSyncCompletedSyncKindInitial})
	}
	assert.Empty(t, transport.captures)
}

func TestValidatePropertyValueEnforcesNumericBounds(t *testing.T) {
	minimum, maximum := float64(0), float64(25)
	contract := PropertyContract{Kind: "integer", Minimum: minimum, Maximum: maximum}
	_, err := validatePropertyValue(contract, 26)
	require.Error(t, err)
	value, err := validatePropertyValue(contract, 25)
	require.NoError(t, err)
	assert.Equal(t, int64(25), value)
}

func TestGatewayLegacyCaptureDropsUnreviewedPropertiesButStillGatesConsent(t *testing.T) {
	transport := &recordingTransport{}
	gateway := testGateway(transport, stubAuthority{subject: "user_clerk", granted: true})

	gateway.Capture("internal-user", string(EventTransactionRecategorized), map[string]interface{}{
		"transaction_id": "private",
		"amount":         125,
	})

	require.Len(t, transport.captures, 1)
	assert.Empty(t, transport.captures[0].properties)
}

func TestGatewayNilAndCloseAreSafe(t *testing.T) {
	gateway := NewGateway(nil, nil, nil)
	assert.NotPanics(t, func() {
		gateway.CaptureEvent(context.Background(), "user", FinancialDataSyncCompletedPayload{SyncKind: FinancialDataSyncCompletedSyncKindInitial})
		gateway.Capture("user", string(EventTransactionRecategorized), nil)
		gateway.Identify("user", nil)
		gateway.Close()
	})

	transport := &recordingTransport{}
	gateway = testGateway(transport, stubAuthority{subject: "user_clerk", granted: true})
	gateway.Close()
	assert.True(t, transport.closed)
}

func TestDeliveryCallbackRecordsBoundedFailureWithoutPayload(t *testing.T) {
	before := testutil.ToFloat64(analyticsDeliveryFailuresTotal.WithLabelValues("delivery"))
	deliveryCallback{}.Failure(posthoggo.APIMessage(map[string]interface{}{"properties": map[string]interface{}{"email": "secret@example.com"}}), errors.New("network failed"))
	after := testutil.ToFloat64(analyticsDeliveryFailuresTotal.WithLabelValues("delivery"))
	assert.Equal(t, before+1, after)
}

func TestClientCloseRecordsShutdownFailure(t *testing.T) {
	before := testutil.ToFloat64(analyticsDeliveryFailuresTotal.WithLabelValues("shutdown"))
	client := &Client{inner: closeFailingClient{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	client.Close()
	after := testutil.ToFloat64(analyticsDeliveryFailuresTotal.WithLabelValues("shutdown"))
	assert.Equal(t, before+1, after)
}
