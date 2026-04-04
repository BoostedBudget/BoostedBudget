package crypto

import "strings"

// MaskAccountNumber masks all but the last 4 characters of an account number.
//
// Example: "123456789012" → "****9012"
// If the account number is 4 or fewer characters, returns all asterisks of the same length.
func MaskAccountNumber(account string) string {
	if account == "" {
		return account
	}
	if len(account) <= 4 {
		return strings.Repeat("*", len(account))
	}
	return "****" + account[len(account)-4:]
}

// MaskAccountNumberPtr masks a nullable account number, preserving nil.
func MaskAccountNumberPtr(account *string) *string {
	if account == nil {
		return nil
	}
	masked := MaskAccountNumber(*account)
	return &masked
}

// MaskEmail masks an email address, showing only the first character and domain.
//
// Example: "user@example.com" → "u***@example.com"
// If the email has no '@', returns "***".
func MaskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return "***"
	}
	local := email[:at]
	domain := email[at:]
	if len(local) <= 1 {
		return local + "***" + domain
	}
	return string(local[0]) + "***" + domain
}

// MaskAddress masks the street number portion of a street address.
//
// Example: "123 Main St" → "*** Main St"
// If there is no space, returns "***".
func MaskAddress(address string) string {
	if address == "" {
		return address
	}
	spaceIdx := strings.Index(address, " ")
	if spaceIdx <= 0 {
		return "***"
	}
	return "***" + address[spaceIdx:]
}

// MaskAddressPtr masks a nullable address, preserving nil.
func MaskAddressPtr(address *string) *string {
	if address == nil {
		return nil
	}
	masked := MaskAddress(*address)
	return &masked
}
