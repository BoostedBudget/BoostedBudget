package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCreditLiability_Validate_Success verifies that a valid CreditLiability passes validation.
func TestCreditLiability_Validate_Success(t *testing.T) {
	cl := CreditLiability{
		UserID:    "user-123",
		AccountID: "account-456",
	}
	require.NoError(t, cl.Validate())
}

// TestCreditLiability_Validate_MissingUserID verifies that a missing UserID fails validation.
func TestCreditLiability_Validate_MissingUserID(t *testing.T) {
	cl := CreditLiability{
		AccountID: "account-456",
	}
	err := cl.Validate()
	require.Error(t, err)
	require.EqualError(t, err, "user_id is required")
}

// TestCreditLiability_Validate_MissingAccountID verifies that a missing AccountID fails validation.
func TestCreditLiability_Validate_MissingAccountID(t *testing.T) {
	cl := CreditLiability{
		UserID: "user-123",
	}
	err := cl.Validate()
	require.Error(t, err)
	require.EqualError(t, err, "account_id is required")
}

// TestMortgageLiability_Validate_Success verifies that a valid MortgageLiability passes validation.
func TestMortgageLiability_Validate_Success(t *testing.T) {
	ml := MortgageLiability{
		UserID:    "user-123",
		AccountID: "account-456",
	}
	require.NoError(t, ml.Validate())
}

// TestMortgageLiability_Validate_MissingUserID verifies that a missing UserID fails validation.
func TestMortgageLiability_Validate_MissingUserID(t *testing.T) {
	ml := MortgageLiability{
		AccountID: "account-456",
	}
	err := ml.Validate()
	require.Error(t, err)
	require.EqualError(t, err, "user_id is required")
}

// TestMortgageLiability_Validate_MissingAccountID verifies that a missing AccountID fails validation.
func TestMortgageLiability_Validate_MissingAccountID(t *testing.T) {
	ml := MortgageLiability{
		UserID: "user-123",
	}
	err := ml.Validate()
	require.Error(t, err)
	require.EqualError(t, err, "account_id is required")
}

// TestStudentLoanLiability_Validate_Success verifies that a valid StudentLoanLiability passes validation.
func TestStudentLoanLiability_Validate_Success(t *testing.T) {
	sl := StudentLoanLiability{
		UserID:    "user-123",
		AccountID: "account-456",
	}
	require.NoError(t, sl.Validate())
}

// TestStudentLoanLiability_Validate_MissingUserID verifies that a missing UserID fails validation.
func TestStudentLoanLiability_Validate_MissingUserID(t *testing.T) {
	sl := StudentLoanLiability{
		AccountID: "account-456",
	}
	err := sl.Validate()
	require.Error(t, err)
	require.EqualError(t, err, "user_id is required")
}

// TestStudentLoanLiability_Validate_MissingAccountID verifies that a missing AccountID fails validation.
func TestStudentLoanLiability_Validate_MissingAccountID(t *testing.T) {
	sl := StudentLoanLiability{
		UserID: "user-123",
	}
	err := sl.Validate()
	require.Error(t, err)
	require.EqualError(t, err, "account_id is required")
}

// TestLiabilitySyncResult_Counts verifies that the LiabilitySyncResult struct is correctly populated.
func TestLiabilitySyncResult_Counts(t *testing.T) {
	result := LiabilitySyncResult{
		CreditProcessed:       3,
		MortgageProcessed:     1,
		StudentLoansProcessed: 2,
		AccountsProcessed:     6,
	}
	require.Equal(t, 3, result.CreditProcessed)
	require.Equal(t, 1, result.MortgageProcessed)
	require.Equal(t, 2, result.StudentLoansProcessed)
	require.Equal(t, 6, result.AccountsProcessed)
}
