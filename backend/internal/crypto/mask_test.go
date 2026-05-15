package crypto_test

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/stretchr/testify/assert"
)

func TestMaskAccountNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"full account number", "123456789012", "****9012"},
		{"exactly 4 chars", "1234", "****"},
		{"3 chars", "123", "***"},
		{"1 char", "1", "*"},
		{"empty", "", ""},
		{"8 chars", "12345678", "****5678"},
		{"with letters", "ACC12345678", "****5678"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := crypto.MaskAccountNumber(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskAccountNumberPtr(t *testing.T) {
	result := crypto.MaskAccountNumberPtr(nil)
	assert.Nil(t, result)

	value := "123456789012"
	result = crypto.MaskAccountNumberPtr(&value)
	assert.NotNil(t, result)
	assert.Equal(t, "****9012", *result)
}

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal email", "user@example.com", "u***@example.com"},
		{"short local", "a@example.com", "a***@example.com"},
		{"long local", "longusername@example.com", "l***@example.com"},
		{"no at sign", "notanemail", "***"},
		{"at sign first", "@example.com", "***"},
		{"empty", "", "***"},
		{"gmail", "john.doe@gmail.com", "j***@gmail.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := crypto.MaskEmail(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal address", "123 Main St", "*** Main St"},
		{"apartment", "456 Oak Ave Apt 2B", "*** Oak Ave Apt 2B"},
		{"no space", "NoSpaceAddress", "***"},
		{"empty", "", ""},
		{"single word", "12345", "***"},
		{"leading space", " 123 Main", "***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := crypto.MaskAddress(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskAddressPtr(t *testing.T) {
	result := crypto.MaskAddressPtr(nil)
	assert.Nil(t, result)

	value := "123 Main St"
	result = crypto.MaskAddressPtr(&value)
	assert.NotNil(t, result)
	assert.Equal(t, "*** Main St", *result)
}
