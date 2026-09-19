package config

import (
	"fmt"
	"os"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestTierEntitlementAlignment guards the alignment between the tier catalogue
// (config/subscription_tiers.yaml) and the RevenueCat entitlement-key grammar
// (domain.RevenueCatKey, internal/domain/revenuecat.go).
//
// NOTE (scope): this guard is an ADDITION beyond #1652's stated scope. #1652 centralises access
// on RevenueCat and makes the tier catalogue the single source of truth for tiers; this test
// pins the seam between the two — a drift there would either strand a catalogue tier with no
// entitlement key RevenueCat can grant (forward), or let RevenueCat resolve an entitlement to a
// tier the catalogue does not define (converse).
//
// It deliberately reads the RAW YAML rather than going through LoadTierConfig: validate() rejects
// any non-canonical tier name before the alignment check could run, which would make the guard
// untestable (a bogus catalogue tier would fail validation, not alignment). Reading the file
// directly means the assertion below is what actually fails on catalogue/grammar drift.
//
// It reads the catalogue through the in-package symlink testdata/tier_catalogue.yaml rather than
// the direct ../../../config path, and that indirection is load-bearing: the catalogue is outside
// the Go module root (backend/), and go's test cache only rechecks opened files inside the module
// root, so a direct read lets a catalogue-only edit be served from cache as a stale PASS.
//
// The check lives in package config (importing domain) — never the reverse — so no import cycle
// is introduced: domain must never import config.
func TestTierEntitlementAlignment(t *testing.T) {
	raw, err := os.ReadFile("testdata/tier_catalogue.yaml")
	require.NoError(t, err, "could not read the tier catalogue")

	var catalogue struct {
		Tiers map[string]domain.SubscriptionTier `yaml:"tiers"`
		Trial TrialConfig                        `yaml:"trial"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &catalogue), "could not parse the tier catalogue")
	require.NotEmpty(t, catalogue.Tiers, "no tiers in the catalogue — a vacuous pass here would read as protection")

	// ── Forward: every catalogue tier must carry a valid paid_/comp_ entitlement key ──────────
	for name := range catalogue.Tiers {
		for _, source := range []string{"paid", "comp"} {
			key := source + "_" + name
			tier, gotSource, _, ok := domain.RevenueCatKey(key)
			require.Truef(t, ok,
				"catalogue tier %q has no valid RevenueCat entitlement key %q", name, key)
			assert.Equalf(t, name, tier,
				"%q must resolve to tier %q", key, name)
			assert.Equalf(t, source, gotSource,
				"%q must resolve with source %q", key, source)
		}
	}

	// ── Trial coverage: the configured trial tier must have a valid trial_ key ────────────────
	trialKey := "trial_" + string(catalogue.Trial.Tier)
	trialTier, trialSource, _, ok := domain.RevenueCatKey(trialKey)
	require.Truef(t, ok,
		"trial.tier %q has no valid RevenueCat entitlement key %q", catalogue.Trial.Tier, trialKey)
	assert.Equal(t, string(catalogue.Trial.Tier), trialTier, "%q must resolve to the configured trial tier", trialKey)
	assert.Equal(t, "trial", trialSource, "%q must resolve with source \"trial\"", trialKey)

	// ── Converse: no tier RevenueCatKey recognises may be absent from the catalogue ───────────
	//
	// RevenueCatKey's recognised tier set is a hardcoded switch, so it can only be enumerated by
	// probing candidates. The universe below is the catalogue's own tiers plus every plausible
	// household seat count and premium — bounded, but broad enough to catch a tier being added
	// to (or dropped from) the domain switch without a matching catalogue change.
	candidates := map[string]struct{}{}
	for name := range catalogue.Tiers {
		candidates[name] = struct{}{}
	}
	candidates[string(domain.TierPremium)] = struct{}{}
	for seats := 1; seats <= 10; seats++ {
		candidates[fmt.Sprintf("household_%d", seats)] = struct{}{}
	}

	recognised := map[string]struct{}{}
	for candidate := range candidates {
		paidTier, _, _, paidOK := domain.RevenueCatKey("paid_" + candidate)
		compTier, _, _, compOK := domain.RevenueCatKey("comp_" + candidate)
		if paidOK && compOK && paidTier == candidate && compTier == candidate {
			recognised[candidate] = struct{}{}
		}
	}

	catalogueTiers := make(map[string]struct{}, len(catalogue.Tiers))
	for name := range catalogue.Tiers {
		catalogueTiers[name] = struct{}{}
	}

	for tier := range recognised {
		assert.Containsf(t, catalogueTiers, tier,
			"domain.RevenueCatKey recognises tier %q, but it is absent from config/subscription_tiers.yaml", tier)
	}
	for tier := range catalogueTiers {
		assert.Containsf(t, recognised, tier,
			"config/subscription_tiers.yaml declares tier %q, but domain.RevenueCatKey does not recognise it", tier)
	}
}
