package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAllocationOptions_Defaults verifies that a zero-value AllocationOptions
// represents the investment-only, security-type-grouped, assets-only default.
// This is the backward-compatible behavior — no params → same as today.
func TestAllocationOptions_Defaults(t *testing.T) {
	opts := AllocationOptions{}
	assert.Equal(t, AllocationGroupBySecurityType, opts.EffectiveGroupBy())
	assert.Equal(t, AllocationBasisAssets, opts.EffectiveBasis())
	assert.False(t, opts.IncludesCash(), "default must NOT include cash accounts")
	assert.False(t, opts.IncludesLiabilities(), "default must NOT include liabilities")
}

// TestAllocationOptions_IncludeCash verifies that Include=["cash"] adds depository accounts.
func TestAllocationOptions_IncludeCash(t *testing.T) {
	opts := AllocationOptions{Include: []string{"cash"}}
	assert.True(t, opts.IncludesCash())
	assert.False(t, opts.IncludesLiabilities())
}

// TestAllocationOptions_IncludeLiabilities verifies that Include=["liabilities"] adds liabilities.
func TestAllocationOptions_IncludeLiabilities(t *testing.T) {
	opts := AllocationOptions{Include: []string{"liabilities"}}
	assert.False(t, opts.IncludesCash())
	assert.True(t, opts.IncludesLiabilities())
}

// TestAllocationOptions_IncludeBoth verifies cash+liabilities together.
func TestAllocationOptions_IncludeBoth(t *testing.T) {
	opts := AllocationOptions{Include: []string{"cash", "liabilities"}}
	assert.True(t, opts.IncludesCash())
	assert.True(t, opts.IncludesLiabilities())
}

// TestAllocationOptions_ValidGroupByValues verifies that each documented group_by value
// is accepted without error.
func TestAllocationOptions_ValidGroupByValues(t *testing.T) {
	for _, v := range []string{"security_type", "asset_class", "account"} {
		opts := AllocationOptions{GroupBy: v}
		assert.Equal(t, v, opts.EffectiveGroupBy(), "group_by=%q should be preserved", v)
	}
}

// TestAllocationOptions_ValidBasisValues verifies that "assets" and "net" are accepted.
func TestAllocationOptions_ValidBasisValues(t *testing.T) {
	for _, v := range []string{"assets", "net"} {
		opts := AllocationOptions{Basis: v}
		assert.Equal(t, v, opts.EffectiveBasis(), "basis=%q should be preserved", v)
	}
}
