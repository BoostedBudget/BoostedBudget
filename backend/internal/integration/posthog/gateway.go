package posthog

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const legacyLookupTimeout = 500 * time.Millisecond

var (
	analyticsEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "boostedbudget_product_analytics_events_total",
		Help: "Backend product analytics events accepted by the consent-aware gateway.",
	}, []string{"event"})
	analyticsDropsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "boostedbudget_product_analytics_drops_total",
		Help: "Backend product analytics events dropped fail-closed, by bounded reason.",
	}, []string{"event", "reason"})
	analyticsDeliveryFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "boostedbudget_product_analytics_delivery_failures_total",
		Help: "PostHog delivery failures by bounded delivery stage.",
	}, []string{"stage"})
	propertyWordBoundary = regexp.MustCompile(`[^a-z0-9]+`)
)

// AuthorityResolver holds a database read lock across the enqueue callback. This makes consent
// withdrawal and event emission serializable: a withdrawal that commits first cannot be emitted
// after, while a withdrawal racing an already-authorized enqueue waits for that enqueue boundary.
type AuthorityResolver interface {
	WithConsentedSubject(ctx context.Context, internalUserID string, emit func(clerkSubject string)) (bool, error)
}

// Gateway is the sole production path from backend product code to PostHog. Legacy Capture exists
// temporarily so the existing generated-name producers can migrate incrementally; it still applies
// current DB consent, canonical identity, contract validation, and property stripping.
//
// Delivery is best effort and at most once from the application's perspective. Analytics failure
// never changes the result of the business operation that produced the event.
type Gateway struct {
	transport Analytics
	authority AuthorityResolver
	logger    *slog.Logger
}

var _ Analytics = (*Gateway)(nil)
var _ ProductAnalytics = (*Gateway)(nil)
var _ GDPRAnalytics = (*Gateway)(nil)

func NewGateway(transport Analytics, authority AuthorityResolver, logger *slog.Logger) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{transport: transport, authority: authority, logger: logger}
}

func (g *Gateway) CaptureEvent(ctx context.Context, internalUserID string, event TypedEvent) {
	if isNilEvent(event) {
		g.drop("unknown", "invalid_contract")
		return
	}
	g.capture(ctx, internalUserID, event.EventName(), event.Properties())
}

// CaptureIfConsented retains the legacy GDPRAnalytics shape while routing through the same
// authoritative gateway. The gateway performs the single current-state consent read.
func (g *Gateway) CaptureIfConsented(ctx context.Context, internalUserID, rawEvent string, properties map[string]interface{}) {
	g.capture(ctx, internalUserID, EventName(rawEvent), properties)
}

// Capture is the consent-aware compatibility adapter for legacy producers. New code must use
// CaptureEvent with a generated payload.
func (g *Gateway) Capture(internalUserID, rawEvent string, properties map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), legacyLookupTimeout)
	defer cancel()
	g.capture(ctx, internalUserID, EventName(rawEvent), properties)
}

func (g *Gateway) capture(ctx context.Context, internalUserID string, event EventName, properties map[string]interface{}) {
	label := boundedEventLabel(event)
	if g == nil || g.transport == nil || g.authority == nil {
		if g != nil {
			g.drop(label, "unconfigured")
		}
		return
	}
	contract, known := generatedEventContracts[event]
	if !known {
		g.drop("unknown", "invalid_contract")
		return
	}
	if contract.Consent != "analytics_required" {
		g.drop(label, "operational_event")
		return
	}
	if contract.Status != "active" && contract.Status != "legacy" {
		g.drop(label, "inactive_contract")
		return
	}
	validated, err := validateProperties(contract, properties)
	if err != nil {
		g.drop(label, "invalid_properties")
		return
	}
	captured := false
	emitted, err := g.authority.WithConsentedSubject(ctx, internalUserID, func(clerkSubject string) {
		if strings.TrimSpace(clerkSubject) != "" {
			g.transport.Capture(clerkSubject, string(event), validated)
			captured = true
		}
	})
	if err != nil {
		g.drop(label, "authority_unavailable")
		return
	}
	if !emitted || !captured {
		g.drop(label, "authority_denied")
		return
	}
	analyticsEventsTotal.WithLabelValues(label).Inc()
}

func (g *Gateway) drop(event, reason string) {
	analyticsDropsTotal.WithLabelValues(event, reason).Inc()
	if g != nil && g.logger != nil {
		g.logger.Debug("product analytics event dropped", "event", event, "reason", reason)
	}
}

// Identify intentionally emits nothing. Browser identity owns person properties; backend callers
// must not create aliases or attach potentially identifying free-form attributes.
func (*Gateway) Identify(string, map[string]interface{}) {}

func (g *Gateway) Close() {
	if g != nil && g.transport != nil {
		g.transport.Close()
	}
}

func boundedEventLabel(event EventName) string {
	if _, ok := generatedEventContracts[event]; ok {
		return string(event)
	}
	return "unknown"
}

func validateProperties(contract EventContract, input map[string]interface{}) (map[string]interface{}, error) {
	if contract.LegacyPropertiesUnreviewed {
		// Legacy schemas explicitly have not been reviewed. Preserve the event transition while
		// refusing to forward any old free-form property, including identifiers and amounts.
		return map[string]interface{}{}, nil
	}
	if containsNestedPII(input) {
		return nil, fmt.Errorf("sensitive nested property key")
	}
	output := make(map[string]interface{}, len(contract.Properties))
	for key := range input {
		if containsPIIKey(key) {
			return nil, fmt.Errorf("sensitive property key")
		}
		if _, ok := contract.Properties[key]; !ok {
			return nil, fmt.Errorf("unknown property")
		}
	}
	for key, schema := range contract.Properties {
		value, ok := input[key]
		if !ok {
			if schema.Required {
				return nil, fmt.Errorf("missing required property")
			}
			continue
		}
		normalized, err := validatePropertyValue(schema, value)
		if err != nil {
			return nil, err
		}
		output[key] = normalized
	}
	return output, nil
}

func validatePropertyValue(schema PropertyContract, value interface{}) (interface{}, error) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil, fmt.Errorf("nil property")
	}
	switch schema.Kind {
	case "string":
		if rv.Kind() != reflect.String {
			return nil, fmt.Errorf("property must be string")
		}
		valueString := rv.String()
		if len(schema.Values) > 0 && !containsString(schema.Values, valueString) {
			return nil, fmt.Errorf("property is outside bounded enum")
		}
		return valueString, nil
	case "boolean":
		if rv.Kind() != reflect.Bool {
			return nil, fmt.Errorf("property must be boolean")
		}
		return rv.Bool(), nil
	case "integer":
		if rv.Kind() < reflect.Int || rv.Kind() > reflect.Int64 {
			return nil, fmt.Errorf("property must be integer")
		}
		number := float64(rv.Int())
		if outsideBounds(number, schema) {
			return nil, fmt.Errorf("integer property outside bounds")
		}
		return rv.Int(), nil
	case "number":
		if rv.Kind() != reflect.Float32 && rv.Kind() != reflect.Float64 {
			return nil, fmt.Errorf("property must be number")
		}
		number := rv.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("property must be finite")
		}
		if outsideBounds(number, schema) {
			return nil, fmt.Errorf("number property outside bounds")
		}
		return number, nil
	default:
		return nil, fmt.Errorf("unknown property type")
	}
}

func outsideBounds(value float64, schema PropertyContract) bool {
	if minimum, ok := numericBound(schema.Minimum); ok && value < minimum {
		return true
	}
	if maximum, ok := numericBound(schema.Maximum); ok && value > maximum {
		return true
	}
	return false
}

func numericBound(value interface{}) (float64, bool) {
	if value == nil {
		return 0, false
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	default:
		return 0, false
	}
}

func containsNestedPII(value interface{}) bool {
	type visit struct {
		typeName string
		pointer  uintptr
	}
	seen := make(map[visit]bool)
	var inspect func(reflect.Value) bool
	inspect = func(rv reflect.Value) bool {
		if !rv.IsValid() {
			return false
		}
		for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return false
			}
			if rv.Kind() == reflect.Pointer {
				key := visit{typeName: rv.Type().String(), pointer: rv.Pointer()}
				if seen[key] {
					return false
				}
				seen[key] = true
			}
			rv = rv.Elem()
		}
		switch rv.Kind() {
		case reflect.Map:
			if rv.IsNil() {
				return false
			}
			key := visit{typeName: rv.Type().String(), pointer: rv.Pointer()}
			if seen[key] {
				return false
			}
			seen[key] = true
			iter := rv.MapRange()
			for iter.Next() {
				if containsPIIKey(fmt.Sprint(iter.Key().Interface())) || inspect(iter.Value()) {
					return true
				}
			}
		case reflect.Slice:
			if rv.IsNil() {
				return false
			}
			key := visit{typeName: rv.Type().String(), pointer: rv.Pointer()}
			if seen[key] {
				return false
			}
			seen[key] = true
			fallthrough
		case reflect.Array:
			for index := 0; index < rv.Len(); index++ {
				if inspect(rv.Index(index)) {
					return true
				}
			}
		case reflect.Struct:
			for index := 0; index < rv.NumField(); index++ {
				field := rv.Type().Field(index)
				if containsPIIKey(field.Name) || (rv.Field(index).CanInterface() && inspect(rv.Field(index))) {
					return true
				}
			}
		}
		return false
	}
	return inspect(reflect.ValueOf(value))
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func containsPIIKey(key string) bool {
	runes := []rune(key)
	var expanded strings.Builder
	for index, r := range runes {
		if index > 0 && unicode.IsUpper(r) &&
			(unicode.IsLower(runes[index-1]) || (index+1 < len(runes) && unicode.IsLower(runes[index+1]))) {
			expanded.WriteByte('_')
		}
		expanded.WriteRune(unicode.ToLower(r))
	}
	words := propertyWordBoundary.Split(expanded.String(), -1)
	for _, word := range words {
		switch word {
		case "amount", "email", "name", "account", "transaction", "institution", "merchant", "description", "url", "query", "token", "invite", "error", "message", "id":
			return true
		}
	}
	return false
}

func isNilEvent(event TypedEvent) bool {
	if event == nil {
		return true
	}
	value := reflect.ValueOf(event)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
