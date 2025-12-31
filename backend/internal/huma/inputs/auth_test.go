package inputs_test

import (
	"context"
	"crypto/tls"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHumaContext implements huma.Context for testing
type mockHumaContext struct {
	ctx     context.Context
	headers map[string]string
}

func newMockHumaContext(ctx context.Context) *mockHumaContext {
	return &mockHumaContext{
		ctx:     ctx,
		headers: make(map[string]string),
	}
}

// Implement huma.Context interface
func (m *mockHumaContext) Operation() *huma.Operation {
	return &huma.Operation{}
}

func (m *mockHumaContext) Context() context.Context {
	return m.ctx
}

func (m *mockHumaContext) Method() string {
	return http.MethodGet
}

func (m *mockHumaContext) Host() string {
	return "localhost"
}

func (m *mockHumaContext) URL() url.URL {
	return url.URL{Path: "/test"}
}

func (m *mockHumaContext) Param(name string) string {
	return ""
}

func (m *mockHumaContext) Query(name string) string {
	return ""
}

func (m *mockHumaContext) Header(name string) string {
	return m.headers[name]
}

func (m *mockHumaContext) EachHeader(cb func(name, value string)) {
	for k, v := range m.headers {
		cb(k, v)
	}
}

func (m *mockHumaContext) BodyReader() io.Reader {
	return nil
}

func (m *mockHumaContext) TLS() *tls.ConnectionState {
	return nil
}

func (m *mockHumaContext) Version() huma.ProtoVersion {
	return huma.ProtoVersion{Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
}

func (m *mockHumaContext) RemoteAddr() string {
	return "127.0.0.1:12345"
}

func (m *mockHumaContext) GetMultipartForm() (*multipart.Form, error) {
	return nil, nil
}

func (m *mockHumaContext) SetReadDeadline(deadline time.Time) error {
	return nil
}

func (m *mockHumaContext) SetStatus(code int) {}

func (m *mockHumaContext) Status() int {
	return 200
}

func (m *mockHumaContext) AppendHeader(name, value string) {}

func (m *mockHumaContext) SetHeader(name, value string) {}

func (m *mockHumaContext) BodyWriter() io.Writer {
	return io.Discard
}

// TestAuthenticatedInput_Resolve_Success tests that UserID is extracted from context
func TestAuthenticatedInput_Resolve_Success(t *testing.T) {
	// Arrange
	expectedUserID := "user-uuid-12345"
	ctx := context.WithValue(context.Background(), inputs.UserIDKey, expectedUserID)
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	assert.Empty(t, errs)
	assert.Equal(t, expectedUserID, input.UserID)
}

// TestAuthenticatedInput_Resolve_MissingUserID tests that error is returned when user_id is missing
func TestAuthenticatedInput_Resolve_MissingUserID(t *testing.T) {
	// Arrange
	ctx := context.Background() // No user_id in context
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "not authenticated")
}

// TestAuthenticatedInput_Resolve_EmptyUserID tests that error is returned for empty user_id
func TestAuthenticatedInput_Resolve_EmptyUserID(t *testing.T) {
	// Arrange
	ctx := context.WithValue(context.Background(), inputs.UserIDKey, "")
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "not authenticated")
}

// TestAuthenticatedInput_Resolve_WrongType tests that error is returned for wrong type in context
func TestAuthenticatedInput_Resolve_WrongType(t *testing.T) {
	// Arrange
	ctx := context.WithValue(context.Background(), inputs.UserIDKey, 12345) // int instead of string
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "not authenticated")
}

// TestAuthenticatedWithEmailInput_Resolve_Success tests that both UserID and Email are extracted
func TestAuthenticatedWithEmailInput_Resolve_Success(t *testing.T) {
	// Arrange
	expectedUserID := "user-uuid-12345"
	expectedEmail := "test@example.com"

	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"email": expectedEmail,
		},
	}

	ctx := context.WithValue(context.Background(), inputs.UserIDKey, expectedUserID)
	ctx = context.WithValue(ctx, inputs.ClerkClaimsKey, claims)
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedWithEmailInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	assert.Empty(t, errs)
	assert.Equal(t, expectedUserID, input.UserID)
	assert.Equal(t, expectedEmail, input.Email)
}

// TestAuthenticatedWithEmailInput_Resolve_MissingUserID tests inheritance from AuthenticatedInput
func TestAuthenticatedWithEmailInput_Resolve_MissingUserID(t *testing.T) {
	// Arrange
	ctx := context.Background() // No user_id in context
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedWithEmailInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "not authenticated")
}

// TestAuthenticatedWithEmailInput_Resolve_MissingClaims tests that error is returned when claims are missing
func TestAuthenticatedWithEmailInput_Resolve_MissingClaims(t *testing.T) {
	// Arrange
	expectedUserID := "user-uuid-12345"
	ctx := context.WithValue(context.Background(), inputs.UserIDKey, expectedUserID)
	// No claims in context
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedWithEmailInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "Claims not found")
}

// TestAuthenticatedWithEmailInput_Resolve_MissingEmail tests that error is returned when email is missing from claims
func TestAuthenticatedWithEmailInput_Resolve_MissingEmail(t *testing.T) {
	// Arrange
	expectedUserID := "user-uuid-12345"

	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"other_field": "value", // No email
		},
	}

	ctx := context.WithValue(context.Background(), inputs.UserIDKey, expectedUserID)
	ctx = context.WithValue(ctx, inputs.ClerkClaimsKey, claims)
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedWithEmailInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "Email not found")
}

// TestAuthenticatedWithEmailInput_Resolve_NilCustomClaims tests handling of nil custom claims
func TestAuthenticatedWithEmailInput_Resolve_NilCustomClaims(t *testing.T) {
	// Arrange
	expectedUserID := "user-uuid-12345"

	claims := &clerk.SessionClaims{
		Custom: nil, // Nil custom claims
	}

	ctx := context.WithValue(context.Background(), inputs.UserIDKey, expectedUserID)
	ctx = context.WithValue(ctx, inputs.ClerkClaimsKey, claims)
	humaCtx := newMockHumaContext(ctx)

	input := &inputs.AuthenticatedWithEmailInput{}

	// Act
	errs := input.Resolve(humaCtx)

	// Assert
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "Email not found")
}

// TestContextKeys tests that context keys are properly typed to avoid collisions
func TestContextKeys(t *testing.T) {
	// Verify keys are the expected values
	assert.Equal(t, inputs.ContextKey("user_id"), inputs.UserIDKey)
	assert.Equal(t, inputs.ContextKey("clerk_user_id"), inputs.ClerkUserIDKey)
	assert.Equal(t, inputs.ContextKey("clerk_claims"), inputs.ClerkClaimsKey)

	// Verify keys are unique
	assert.NotEqual(t, inputs.UserIDKey, inputs.ClerkUserIDKey)
	assert.NotEqual(t, inputs.UserIDKey, inputs.ClerkClaimsKey)
	assert.NotEqual(t, inputs.ClerkUserIDKey, inputs.ClerkClaimsKey)
}

// TestContextKeys_NoCollisionWithStringKey tests that typed keys don't collide with string keys
func TestContextKeys_NoCollisionWithStringKey(t *testing.T) {
	// Using typed key
	ctx1 := context.WithValue(context.Background(), inputs.UserIDKey, "typed-value")

	// Using plain string key (would be a mistake)
	ctx2 := context.WithValue(ctx1, "user_id", "string-value")

	// Verify they don't interfere with each other
	typedValue, ok := ctx2.Value(inputs.UserIDKey).(string)
	assert.True(t, ok)
	assert.Equal(t, "typed-value", typedValue)

	stringValue, ok := ctx2.Value("user_id").(string)
	assert.True(t, ok)
	assert.Equal(t, "string-value", stringValue)
}
