package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserAccount_Validate_Valid(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
	}
	err := ua.Validate()
	require.NoError(t, err)
}

func TestUserAccount_Validate_MissingUserID(t *testing.T) {
	ua := &UserAccount{
		InstitutionID: "inst-456",
	}
	err := ua.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user_id")
}

func TestUserAccount_Validate_MissingInstitutionID(t *testing.T) {
	ua := &UserAccount{
		UserID: "user-123",
	}
	err := ua.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "institution_id")
}

func TestUserAccount_Validate_NicknameTooLong(t *testing.T) {
	long := strings.Repeat("a", 256)
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		Nickname:      strPtr(long),
	}
	err := ua.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nickname")
}

func TestUserAccount_Validate_NicknameMaxOK(t *testing.T) {
	ok := strings.Repeat("a", 255)
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		Nickname:      strPtr(ok),
	}
	err := ua.Validate()
	require.NoError(t, err)
}

func TestUserAccount_Validate_InvalidHexColor(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		CustomColor:   strPtr("notacolor"),
	}
	err := ua.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_color")
}

func TestUserAccount_Validate_ValidHexColor(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		CustomColor:   strPtr("#FF5733"),
	}
	err := ua.Validate()
	require.NoError(t, err)
}

func TestUserAccount_Validate_EmptyHexColorSkipped(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		CustomColor:   strPtr(""),
	}
	err := ua.Validate()
	require.NoError(t, err)
}

func TestUserAccount_Validate_InvalidIcon(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		CustomIcon:    strPtr("not-a-valid-icon-xyz"),
	}
	err := ua.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_icon")
}

func TestUserAccount_Validate_ValidIcon(t *testing.T) {
	var validIcon string
	for k := range ValidAccountIcons {
		if ValidAccountIcons[k] {
			validIcon = k
			break
		}
	}
	require.NotEmpty(t, validIcon, "ValidAccountIcons must have at least one valid entry")
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		CustomIcon:    strPtr(validIcon),
	}
	err := ua.Validate()
	require.NoError(t, err)
}

func TestUserAccount_Validate_EmptyIconSkipped(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		CustomIcon:    strPtr(""),
	}
	err := ua.Validate()
	require.NoError(t, err)
}

func TestUserAccount_Validate_EmojiTooLong(t *testing.T) {
	// 11 bytes of ASCII exceeds 10-char limit
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		CustomEmoji:   strPtr("12345678901"),
	}
	err := ua.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_emoji")
}

func TestUserAccount_Validate_EmojiOK(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		CustomEmoji:   strPtr("😀"),
	}
	err := ua.Validate()
	require.NoError(t, err)
}

func TestUserAccount_Validate_InvalidPrimaryType(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		PrimaryType:   strPtr("unknown-type"),
	}
	err := ua.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "primary_type")
}

func TestUserAccount_Validate_ValidPrimaryTypes(t *testing.T) {
	validTypes := []string{"depository", "credit", "loan", "investment", "brokerage", "other"}
	for _, pt := range validTypes {
		t.Run(pt, func(t *testing.T) {
			ua := &UserAccount{
				UserID:        "user-123",
				InstitutionID: "inst-456",
				PrimaryType:   strPtr(pt),
			}
			err := ua.Validate()
			require.NoError(t, err)
		})
	}
}

func TestUserAccount_Validate_EmptyPrimaryTypeSkipped(t *testing.T) {
	ua := &UserAccount{
		UserID:        "user-123",
		InstitutionID: "inst-456",
		PrimaryType:   strPtr(""),
	}
	err := ua.Validate()
	require.NoError(t, err)
}
