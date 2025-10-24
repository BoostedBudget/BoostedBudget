package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewValidator verifies that NewValidator creates a validator instance
func TestNewValidator(t *testing.T) {
	validator := NewValidator()
	require.NotNil(t, validator, "NewValidator should return non-nil validator")
	require.NotNil(t, validator.validator, "internal validator should be initialized")
}

// testValidStruct is a valid struct for testing validation
type testValidStruct struct {
	Name  string `json:"name" validate:"required"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"gte=0,lte=150"`
}

// testInvalidStruct is an invalid struct for testing validation
type testInvalidStruct struct {
	Name  string `json:"name" validate:"required"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"gte=0,lte=150"`
}

// TestCustomValidator_Validate_ValidStruct tests validation with a valid struct
func TestCustomValidator_Validate_ValidStruct(t *testing.T) {
	validator := NewValidator()

	validData := testValidStruct{
		Name:  "John Doe",
		Email: "john@example.com",
		Age:   30,
	}

	err := validator.Validate(validData)
	assert.NoError(t, err, "validation should pass for valid struct")
}

// TestCustomValidator_Validate_MissingRequiredField tests validation with missing required field
func TestCustomValidator_Validate_MissingRequiredField(t *testing.T) {
	validator := NewValidator()

	invalidData := testValidStruct{
		Name:  "", // Required field is empty
		Email: "john@example.com",
		Age:   30,
	}

	err := validator.Validate(invalidData)
	require.Error(t, err, "validation should fail for missing required field")

	// Should return Echo HTTPError with 400 status
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "error should be Echo HTTPError")
	assert.Equal(t, 400, httpErr.Code, "HTTP error code should be 400")
}

// TestCustomValidator_Validate_InvalidEmail tests validation with invalid email
func TestCustomValidator_Validate_InvalidEmail(t *testing.T) {
	validator := NewValidator()

	invalidData := testValidStruct{
		Name:  "John Doe",
		Email: "not-an-email", // Invalid email format
		Age:   30,
	}

	err := validator.Validate(invalidData)
	require.Error(t, err, "validation should fail for invalid email")

	// Should return Echo HTTPError with 400 status
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "error should be Echo HTTPError")
	assert.Equal(t, 400, httpErr.Code, "HTTP error code should be 400")
}

// TestCustomValidator_Validate_AgeOutOfRange tests validation with age constraint violation
func TestCustomValidator_Validate_AgeOutOfRange(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name string
		age  int
	}{
		{"negative age", -1},
		{"age too high", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalidData := testValidStruct{
				Name:  "John Doe",
				Email: "john@example.com",
				Age:   tt.age,
			}

			err := validator.Validate(invalidData)
			require.Error(t, err, "validation should fail for age constraint violation")

			httpErr, ok := err.(*echo.HTTPError)
			require.True(t, ok, "error should be Echo HTTPError")
			assert.Equal(t, 400, httpErr.Code, "HTTP error code should be 400")
		})
	}
}

// TestCustomValidator_Validate_MultipleErrors tests validation with multiple errors
func TestCustomValidator_Validate_MultipleErrors(t *testing.T) {
	validator := NewValidator()

	invalidData := testValidStruct{
		Name:  "",             // Missing required
		Email: "not-an-email", // Invalid email
		Age:   -5,             // Invalid age
	}

	err := validator.Validate(invalidData)
	require.Error(t, err, "validation should fail when multiple fields are invalid")

	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "error should be Echo HTTPError")
	assert.Equal(t, 400, httpErr.Code, "HTTP error code should be 400")
}

// TestCustomValidator_Validate_PointerStruct tests validation with pointer to struct
func TestCustomValidator_Validate_PointerStruct(t *testing.T) {
	validator := NewValidator()

	validData := &testValidStruct{
		Name:  "John Doe",
		Email: "john@example.com",
		Age:   30,
	}

	err := validator.Validate(validData)
	assert.NoError(t, err, "validation should pass for valid struct pointer")
}

// TestCustomValidator_Validate_NilPointer tests validation with nil pointer
func TestCustomValidator_Validate_NilPointer(t *testing.T) {
	validator := NewValidator()

	var nilData *testValidStruct = nil

	err := validator.Validate(nilData)
	// go-playground/validator handles nil pointers - they don't cause panics
	// but the behavior may vary, so we just ensure no panic occurs
	t.Logf("Validation of nil pointer result: %v", err)
}

// TestCustomValidator_IntegrationWithEcho tests validator integration with Echo context
func TestCustomValidator_IntegrationWithEcho(t *testing.T) {
	e := echo.New()
	e.Validator = NewValidator()

	// Handler that validates request body
	handler := func(c echo.Context) error {
		var req testValidStruct
		if err := c.Bind(&req); err != nil {
			return err
		}
		if err := c.Validate(&req); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	e.POST("/test", handler)

	t.Run("valid request", func(t *testing.T) {
		body := `{"name":"John Doe","email":"john@example.com","age":30}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "should return 200 for valid request")
	})

	t.Run("invalid email", func(t *testing.T) {
		body := `{"name":"John Doe","email":"not-an-email","age":30}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code, "should return 400 for invalid email")
	})

	t.Run("missing required field", func(t *testing.T) {
		body := `{"email":"john@example.com","age":30}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code, "should return 400 for missing required field")
	})

	t.Run("age out of range", func(t *testing.T) {
		body := `{"name":"John Doe","email":"john@example.com","age":200}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code, "should return 400 for age out of range")
	})
}

// TestCustomValidator_EdgeCases tests various edge cases
func TestCustomValidator_EdgeCases(t *testing.T) {
	validator := NewValidator()

	t.Run("empty struct", func(t *testing.T) {
		type emptyStruct struct{}
		err := validator.Validate(emptyStruct{})
		assert.NoError(t, err, "validation should pass for empty struct")
	})

	t.Run("struct with no validation tags", func(t *testing.T) {
		type noTagsStruct struct {
			Name  string
			Email string
		}
		err := validator.Validate(noTagsStruct{Name: "test"})
		assert.NoError(t, err, "validation should pass for struct without tags")
	})

	t.Run("boundary values", func(t *testing.T) {
		tests := []struct {
			name  string
			age   int
			valid bool
		}{
			{"age 0 (min boundary)", 0, true},
			{"age 150 (max boundary)", 150, true},
			{"age -1 (below min)", -1, false},
			{"age 151 (above max)", 151, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				data := testValidStruct{
					Name:  "Test",
					Email: "test@example.com",
					Age:   tt.age,
				}
				err := validator.Validate(data)
				if tt.valid {
					assert.NoError(t, err)
				} else {
					assert.Error(t, err)
				}
			})
		}
	})
}
