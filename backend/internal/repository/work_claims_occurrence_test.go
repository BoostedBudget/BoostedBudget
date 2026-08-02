package repository

// work_claims_occurrence_test.go — unit tests for OccurrenceResourceID and
// FormatOccurrenceTimestamp, the single shared helper every recurring work_claims FLOW action
// (plaid_disconnect_past_due, past_due_grace_cancel, subscription_cancelled_cleanup,
// retention_notice_30d, retention_notice_7d) must use to build its composite resource_id.
//
// Issue #1413 P5: work_claims is UNIQUE (resource_type, resource_id, action), and nothing ever
// deletes or resets a row. A claim keyed on the bare userID identifies WHO and WHAT, but never
// WHICH OCCURRENCE — so once a recurring flow completes once, it can never run again for that
// user, silently, because ClaimAlreadyCompleted correctly maps to success. These tests pin the
// helper's two properties: SAME occurrence -> byte-identical key (so a retry collides, which is
// the whole point of the claim); DIFFERENT occurrence -> different key (so a genuinely new
// occurrence gets a fresh, unclaimed row). They also pin the NULL-discriminator decision: refuse
// with an error rather than silently collapsing to the bare userID, which would silently
// reintroduce the exact bug this helper exists to close.

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOccurrenceResourceID_SameInputsProduceByteIdenticalKey(t *testing.T) {
	userID := "3b241101-e2bb-4255-8caf-4136c566a962"
	discriminator := "2026-01-15T00:00:00.000000000Z"

	first, err := OccurrenceResourceID(userID, discriminator)
	require.NoError(t, err)

	second, err := OccurrenceResourceID(userID, discriminator)
	require.NoError(t, err)

	assert.Equal(t, first, second, "the same (userID, discriminator) pair must always build the identical resource_id — this is what makes a retry of the SAME occurrence collide with itself")
}

func TestOccurrenceResourceID_DifferentDiscriminatorProducesDifferentKey(t *testing.T) {
	userID := "3b241101-e2bb-4255-8caf-4136c566a962"

	occurrence1, err := OccurrenceResourceID(userID, "2026-01-15T00:00:00.000000000Z")
	require.NoError(t, err)

	occurrence2, err := OccurrenceResourceID(userID, "2026-06-01T00:00:00.000000000Z")
	require.NoError(t, err)

	assert.NotEqual(t, occurrence1, occurrence2, "a genuinely new occurrence (a new dunning cycle, a new retention cycle, a new cancellation event) must get a DIFFERENT resource_id, or it would collide with the prior occurrence and stay permanently blocked")
}

func TestOccurrenceResourceID_DifferentUserSameDiscriminatorProducesDifferentKey(t *testing.T) {
	discriminator := "2026-01-15T00:00:00.000000000Z"

	forUserA, err := OccurrenceResourceID("user-a", discriminator)
	require.NoError(t, err)

	forUserB, err := OccurrenceResourceID("user-b", discriminator)
	require.NoError(t, err)

	assert.NotEqual(t, forUserA, forUserB, "two different users must never share a resource_id even if their occurrence discriminators happen to match")
}

// TestOccurrenceResourceID_EmptyDiscriminatorIsRefused pins the NULL-discriminator decision
// required by the #1413 P5 brief: a composite key that silently collapsed to the bare userID on
// a missing discriminator (e.g. past_due_started_at unset, or a mis-wired caller) would silently
// restore the exact permanent-block bug this helper exists to close. Refuse loudly instead.
func TestOccurrenceResourceID_EmptyDiscriminatorIsRefused(t *testing.T) {
	resourceID, err := OccurrenceResourceID("user-a", "")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingOccurrenceDiscriminator), "an empty discriminator must be reported via the sentinel error so callers can identify it, not a bare ad-hoc error")
	assert.Empty(t, resourceID, "a rejected call must not return a usable resource_id")
}

// TestOccurrenceResourceID_NeverCollapsesToBareUserID is the falsifiability-critical test: even
// with an empty discriminator, the returned string (when there IS one, i.e. on the error path
// there is none — assert directly that the bare userID is never handed back as a "key").
func TestOccurrenceResourceID_NeverCollapsesToBareUserID(t *testing.T) {
	userID := "3b241101-e2bb-4255-8caf-4136c566a962"

	resourceID, err := OccurrenceResourceID(userID, "")
	require.Error(t, err)
	assert.NotEqual(t, userID, resourceID, "on a missing discriminator the helper must never fall back to the bare userID as if it were a valid composite key")
}

func TestFormatOccurrenceTimestamp_NilReturnsEmptyString(t *testing.T) {
	got := FormatOccurrenceTimestamp(nil)
	assert.Empty(t, got, "a nil discriminator timestamp must format to empty, which OccurrenceResourceID then rejects rather than silently building a bare-userID key")
}

// TestFormatOccurrenceTimestamp_DeterministicAcrossDifferentlyConstructedValues proves the
// "fixed UTC layout, not String()" requirement: two time.Time values that represent the exact
// same instant, but were constructed differently (one via time.Now()-style monotonic reading,
// one via a plain UTC construction as if freshly scanned back from Postgres), must format to the
// byte-identical string. A key that differs by formatting is the same bug as no discriminator at
// all — it would fail to collide on a retry of the SAME occurrence.
func TestFormatOccurrenceTimestamp_DeterministicAcrossDifferentlyConstructedValues(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// Same instant, two different representations: one in a non-UTC zone (as a naive t.String()
	// would render with a zone abbreviation and offset), one already normalized to UTC (as a
	// value freshly scanned back from a Postgres timestamptz column typically would be).
	inEastern := time.Date(2026, 1, 14, 19, 0, 0, 123456000, loc)
	inUTC := inEastern.UTC()

	formattedEastern := FormatOccurrenceTimestamp(&inEastern)
	formattedUTC := FormatOccurrenceTimestamp(&inUTC)

	assert.Equal(t, formattedUTC, formattedEastern, "the same instant must format identically regardless of which zone/representation the caller happened to construct the time.Time in")
	assert.NotContains(t, formattedEastern, "EST", "the format must not leak a zone abbreviation the way time.Time.String() can")
	assert.NotContains(t, formattedEastern, "EDT", "the format must not leak a zone abbreviation the way time.Time.String() can")
}

// TestFormatOccurrenceTimestamp_DifferentInstantsProduceDifferentStrings is the flip side of
// determinism: it must actually distinguish genuinely different occurrences.
func TestFormatOccurrenceTimestamp_DifferentInstantsProduceDifferentStrings(t *testing.T) {
	t1 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	assert.NotEqual(t, FormatOccurrenceTimestamp(&t1), FormatOccurrenceTimestamp(&t2))
}

// TestFormatOccurrenceTimestamp_FixedWidthRegardlessOfSubSecondPrecision proves the "fixed
// 9-digit, zero-padded" requirement: RFC3339Nano-style formatting trims trailing zero
// sub-second digits, which would make a whole-second timestamp and a nanosecond-precision
// timestamp format to DIFFERENT-length strings even when a caller expects them to be
// comparable. Fixed width means the same instant always formats to a string of the same shape.
func TestFormatOccurrenceTimestamp_FixedWidthRegardlessOfSubSecondPrecision(t *testing.T) {
	wholeSecond := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	withNanos := time.Date(2026, 1, 15, 12, 0, 0, 123456789, time.UTC)

	formattedWhole := FormatOccurrenceTimestamp(&wholeSecond)
	formattedNanos := FormatOccurrenceTimestamp(&withNanos)

	assert.Len(t, formattedNanos, len(formattedWhole), "fixed-width formatting means a whole-second and a nanosecond-precision instant produce same-length strings")
	assert.NotEqual(t, formattedWhole, formattedNanos, "different sub-second components must still be distinguished")
}
