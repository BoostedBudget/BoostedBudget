package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBalanceSnapshot_SnapshotAt_JSONSerialization(t *testing.T) {
	// Test that SnapshotAt field serializes to "snapshot_at" in JSON
	now := time.Now().UTC()
	balance := 100.50
	snapshot := BalanceSnapshot{
		ID:             "test-id",
		AccountID:      "account-id",
		UserID:         "user-id",
		CurrentBalance: &balance,
		CurrencyCode:   "USD",
		SnapshotAt:     now, // This should fail until we rename the field
		Source:         SnapshotSourcePlaidWebhook,
		CreatedAt:      now,
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Failed to marshal snapshot: %v", err)
	}

	// Check that JSON contains "snapshot_at" not "snapshot_date"
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"snapshot_at"`) {
		t.Errorf("JSON should contain 'snapshot_at' field, got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"snapshot_date"`) {
		t.Errorf("JSON should not contain 'snapshot_date' field, got: %s", jsonStr)
	}
}

func TestBalanceSnapshot_Validate_SnapshotAt(t *testing.T) {
	balance := 100.50

	// Test with zero SnapshotAt - should fail validation
	snapshot := BalanceSnapshot{
		UserID:         "user-id",
		AccountID:      "account-id",
		CurrencyCode:   "USD",
		CurrentBalance: &balance,
		Source:         SnapshotSourcePlaidWebhook,
		// SnapshotAt is zero - should fail
	}

	err := snapshot.Validate()
	if err == nil {
		t.Error("Expected validation error for zero SnapshotAt")
	}
	if err != nil && err.Error() != "snapshot_at is required" {
		t.Errorf("Expected error message 'snapshot_at is required', got: %v", err)
	}

	// Test with valid SnapshotAt
	snapshot.SnapshotAt = time.Now().UTC()
	err = snapshot.Validate()
	if err != nil {
		t.Errorf("Expected no error with valid SnapshotAt, got: %v", err)
	}
}

func TestBalanceSnapshot_TimestampPrecision(t *testing.T) {
	// Test that SnapshotAt preserves timestamp precision (not truncated to date)
	// This is important for multiple snapshots per day
	balance := 100.50

	time1 := time.Date(2024, 11, 18, 10, 30, 45, 123456789, time.UTC)
	time2 := time.Date(2024, 11, 18, 14, 15, 30, 987654321, time.UTC)

	snapshot1 := BalanceSnapshot{
		ID:             "id-1",
		AccountID:      "account-id",
		UserID:         "user-id",
		CurrentBalance: &balance,
		CurrencyCode:   "USD",
		SnapshotAt:     time1,
		Source:         SnapshotSourcePlaidWebhook,
		CreatedAt:      time1,
	}

	snapshot2 := BalanceSnapshot{
		ID:             "id-2",
		AccountID:      "account-id",
		UserID:         "user-id",
		CurrentBalance: &balance,
		CurrencyCode:   "USD",
		SnapshotAt:     time2,
		Source:         SnapshotSourcePlaidWebhook,
		CreatedAt:      time2,
	}

	// Both should have different SnapshotAt values (same day, different times)
	if snapshot1.SnapshotAt.Equal(snapshot2.SnapshotAt) {
		t.Error("SnapshotAt should preserve different times on same day")
	}

	// Verify times are preserved exactly
	if !snapshot1.SnapshotAt.Equal(time1) {
		t.Errorf("SnapshotAt should equal time1, got %v", snapshot1.SnapshotAt)
	}
	if !snapshot2.SnapshotAt.Equal(time2) {
		t.Errorf("SnapshotAt should equal time2, got %v", snapshot2.SnapshotAt)
	}
}


// TestValidate_AcceptsTheDailyCaptureSource is #1626 Phase 3 Task G's T12.
//
// Validate() hard-rejects any source outside its constant set. Nothing calls it on the
// production write path today, which makes it a landmine rather than a blocker: the periodic
// gap-filler writes a FOURTH provenance, and the day anything starts validating a snapshot the
// new rows would be the only ones rejected. The constant and the validator are extended
// together for that reason.
func TestValidate_AcceptsTheDailyCaptureSource(t *testing.T) {
	balance := 123.45
	snapshot := &BalanceSnapshot{
		UserID:         "user-1",
		AccountID:      "acct-1",
		CurrencyCode:   "USD",
		SnapshotAt:     time.Now().UTC(),
		Source:         SnapshotSourceDailyCapture,
		CurrentBalance: &balance,
	}

	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() rejected the periodic gap-filler's own provenance: %v", err)
	}
}

// TestValidate_StillRejectsAnUnknownSource is the other half: extending the set must not
// degrade it into "accept anything". Without this, deleting the validSources map entirely
// would leave the test above green.
func TestValidate_StillRejectsAnUnknownSource(t *testing.T) {
	balance := 1.0
	snapshot := &BalanceSnapshot{
		UserID:         "user-1",
		AccountID:      "acct-1",
		CurrencyCode:   "USD",
		SnapshotAt:     time.Now().UTC(),
		Source:         "periodic_local",
		CurrentBalance: &balance,
	}

	if err := snapshot.Validate(); err == nil {
		t.Fatal("Validate() accepted an unknown source — the constant set is the whole point")
	}
}
