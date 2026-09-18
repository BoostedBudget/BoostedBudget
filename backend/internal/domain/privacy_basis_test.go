package domain

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// privacyFieldBasis records, for one bool field of PrivacySettings, the GDPR lawful basis it
// rests on and the code site that enforces the user's choice. A field with no reader is not
// silently acceptable: its enforcementSite must say so and point at the tracked removal.
type privacyFieldBasis struct {
	basis           string
	enforcementSite string
}

var privacyFieldBases = map[string]privacyFieldBasis{
	"GDPRNecessaryConsent": {
		basis:           "Contract (Art. 6(1)(b)) — required for app functionality",
		enforcementSite: "domain/privacy.go PrivacySettings.Validate refuses a false value; the field is not user-toggleable",
	},
	"GDPRAnalyticsConsent": {
		basis:           "Consent (Art. 6(1)(a))",
		enforcementSite: "integration/posthog/privacy_consent.go HasAnalyticsConsent → Gateway drops events fail-closed",
	},
	"GDPRInteractionCaptureConsent": {
		basis:           "Consent (Art. 6(1)(a))",
		enforcementSite: "Gateway consent read + frontend session-recording gate; domain/privacy.go Validate enforces the analytics precondition",
	},
	"MarketingEmailsEnabled": {
		basis:           "Consent (Art. 6(1)(a))",
		enforcementSite: "Persisted preference toggled in Profile → Privacy; no backend sender reads the column yet",
	},
	"ProductUpdatesEnabled": {
		basis:           "Consent (Art. 6(1)(a))",
		enforcementSite: "Persisted preference toggled in Profile → Privacy; no backend sender reads the column yet",
	},
	"SecurityAlertsEnabled": {
		basis:           "Legitimate interest (Art. 6(1)(f)) — account and security notices",
		enforcementSite: "Persisted preference toggled in Profile → Privacy; no backend sender reads the column yet",
	},
	"DataSharingEnabled": {
		basis:           "Consent (Art. 6(1)(a))",
		enforcementSite: "NONE — reader-less; removal tracked in BoostedBudget/BoostedBudget#1687",
	},
	"IncludeInAggregateStats": {
		basis:           "Legitimate interest (Art. 6(1)(f)), opt-out",
		enforcementSite: "repository/savings_impact_repository.go GetAggregateSavingsStats filters include_in_aggregate_stats = true",
	},
	"IdentifiedDiagnosticsEnabled": {
		basis:           "Legitimate interest (Art. 6(1)(f)), opt-out",
		enforcementSite: "services/privacy_service.go emits the post-commit $unset when the flag flips true→false",
	},
}

// retainedStaleCookieKeys lists consent keys deliberately kept although they no longer map to any
// PrivacySettings field. This is not the reflection map: the reflection covers struct bool fields,
// and a cookie key is not one, so it must not be smuggled in as a fake entry.
var retainedStaleCookieKeys = []struct {
	key    string
	reason string
}{
	{
		key: "advertising",
		reason: "retained-stale: vestigial cookie key kept for COOKIE_CONSENT_VERSION '2.0' compatibility; " +
			"the server field gdpr_advertising_consent was dropped in migration 000214 (#1683). Owner: privacy.",
	},
}

// TestEveryBoolPrivacyFieldDeclaresItsBasisAndEnforcementSite reflects over every bool field of
// PrivacySettings. Keying on the GDPR*Consent fields alone would miss identified_diagnostics_enabled
// and include_in_aggregate_stats, so the field set is derived, never hand-listed.
func TestEveryBoolPrivacyFieldDeclaresItsBasisAndEnforcementSite(t *testing.T) {
	typ := reflect.TypeOf(PrivacySettings{})

	checked := 0
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Bool {
			continue
		}
		checked++
		entry, ok := privacyFieldBases[field.Name]
		if !assert.True(t, ok, "bool field PrivacySettings.%s has no {basis, enforcementSite} entry", field.Name) {
			continue
		}
		assert.NotEmpty(t, entry.basis, "PrivacySettings.%s: basis is empty", field.Name)
		assert.NotEmpty(t, entry.enforcementSite, "PrivacySettings.%s: enforcementSite is empty", field.Name)
	}
	require.GreaterOrEqual(t, checked, 9, "reflection found fewer bool fields than expected — the guard went blind")
}

// TestPrivacyBasisMapNamesNoRemovedField fails closed in the other direction: a map entry left
// behind for a field that was renamed or dropped is dead documentation, not coverage.
func TestPrivacyBasisMapNamesNoRemovedField(t *testing.T) {
	typ := reflect.TypeOf(PrivacySettings{})
	for name := range privacyFieldBases {
		_, ok := typ.FieldByName(name)
		assert.True(t, ok, "basis entry %q names a field PrivacySettings no longer has", name)
	}
}

// TestDataSharingBasisEntryCitesTheRemovalIssue pins the reader-less field to its filed removal,
// so "NONE" cannot drift into an unattributed gap.
func TestDataSharingBasisEntryCitesTheRemovalIssue(t *testing.T) {
	entry, ok := privacyFieldBases["DataSharingEnabled"]
	require.True(t, ok)
	assert.Contains(t, entry.enforcementSite, "#1687")
}

// TestRetainedStaleCookieKeyLivesOutsideTheReflectionMap pins the vestigial cookie key to its own
// list, and out of the struct-field map where it has no field to key on.
func TestRetainedStaleCookieKeyLivesOutsideTheReflectionMap(t *testing.T) {
	require.Len(t, retainedStaleCookieKeys, 1)
	assert.Equal(t, "advertising", retainedStaleCookieKeys[0].key)
	assert.Contains(t, retainedStaleCookieKeys[0].reason, "retained-stale")
	_, insideMap := privacyFieldBases["advertising"]
	assert.False(t, insideMap, "the retained-stale cookie key must not appear in the reflection map")
}
