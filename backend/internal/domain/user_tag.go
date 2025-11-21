package domain

import (
	"errors"
	"regexp"
	"time"
)

// UserTag represents a user-defined tag for transaction organization.
// Tags are FREE for all subscription tiers (Issue #345).
type UserTag struct {
	ID         string    `json:"id" db:"id"`
	UserID     string    `json:"user_id" db:"user_id"`
	Name       string    `json:"name" db:"name"`
	Color      *string   `json:"color,omitempty" db:"color"` // Hex color like #FF5733
	UsageCount int       `json:"usage_count" db:"usage_count"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// Validate checks that the UserTag has all required fields and valid values.
func (t *UserTag) Validate() error {
	if t.UserID == "" {
		return errors.New("user_id is required")
	}

	if t.Name == "" {
		return errors.New("name is required")
	}

	if len(t.Name) > 100 {
		return errors.New("name must be 100 characters or less")
	}

	// Validate color format if provided (hex color)
	if t.Color != nil && *t.Color != "" {
		if !isValidHexColor(*t.Color) {
			return errors.New("color must be a valid hex color (e.g., #FF5733)")
		}
	}

	return nil
}

// CreateUserTagRequest represents the request to create a new user tag.
type CreateUserTagRequest struct {
	Name  string  `json:"name" validate:"required,max=100"`
	Color *string `json:"color,omitempty" validate:"omitempty,hexcolor"`
}

// UpdateUserTagRequest represents the request to update a user tag.
type UpdateUserTagRequest struct {
	Name  *string `json:"name,omitempty" validate:"omitempty,max=100"`
	Color *string `json:"color,omitempty" validate:"omitempty,hexcolor"`
}

// isValidHexColor validates a hex color string (e.g., #FF5733 or #FFF)
func isValidHexColor(color string) bool {
	hexColorRegex := regexp.MustCompile(`^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$`)
	return hexColorRegex.MatchString(color)
}

// Tag-related errors
var (
	ErrTagNotFound      = errors.New("tag not found")
	ErrTagAlreadyExists = errors.New("tag with this name already exists")
	ErrTagLimitExceeded = errors.New("tag limit exceeded for subscription tier")
	ErrInvalidTagName   = errors.New("invalid tag name")
	ErrInvalidTagColor  = errors.New("invalid tag color")
)
