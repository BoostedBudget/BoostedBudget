// Package operations provides Huma operation handler tests.
// This file tests the central route registration helper.
package operations

import (
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"
)

// TestRegisterAllRoutesForSpec_DoesNotPanic verifies that registering all routes
// for spec generation does not panic even though all service dependencies are nil.
// The function creates zero-value handlers — their methods are never called during
// spec generation, so nil dependencies are safe.
func TestRegisterAllRoutesForSpec_DoesNotPanic(t *testing.T) {
	_, api := humatest.New(t)
	require.NotPanics(t, func() {
		RegisterAllRoutesForSpec(api)
	}, "RegisterAllRoutesForSpec must not panic with a fresh Huma API")
}
