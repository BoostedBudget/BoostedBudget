package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEveryNamespaceHasNonZeroTTL(t *testing.T) {
	for ns, cfg := range namespaceRegistry {
		assert.True(t, cfg.TTL > 0, "namespace %s must have a non-zero TTL", ns)
	}
}

func TestEntitlementNamespace_IsHardDisabled(t *testing.T) {
	cfg, ok := lookupNamespace(NamespaceEntitlement)
	assert.True(t, ok)
	assert.True(t, cfg.HardDisabled)
}

// TestNamespaces_EnumeratesEveryRegisteredNamespace pins namespaces() (control.go's poller uses
// this shape to build the list it polls) against the registry it is meant to mirror.
func TestNamespaces_EnumeratesEveryRegisteredNamespace(t *testing.T) {
	got := namespaces()
	assert.Len(t, got, len(namespaceRegistry))
	for _, ns := range got {
		_, ok := lookupNamespace(ns)
		assert.True(t, ok, "namespaces() returned %s, which is not in the registry", ns)
	}
}
