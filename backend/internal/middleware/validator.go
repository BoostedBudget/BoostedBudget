package middleware

import (
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// CustomValidator wraps go-playground/validator to implement Echo's Validator interface
type CustomValidator struct {
	validator *validator.Validate
}

// NewValidator creates a new CustomValidator instance
func NewValidator() *CustomValidator {
	return &CustomValidator{
		validator: validator.New(),
	}
}

// Validate implements echo.Validator interface
func (cv *CustomValidator) Validate(i interface{}) error {
	if err := cv.validator.Struct(i); err != nil {
		// Return validation errors in a format Echo can understand
		return echo.NewHTTPError(400, err.Error())
	}
	return nil
}
