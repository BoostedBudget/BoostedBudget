package services

// Issue #1626 — the forward-looking guard for the UUID-label defect class.
//
// The reported bug was ONE symptom of a mechanism that existed in SIX places: an analytics read
// groups by a synthetic key (`custom:<uuid>` for a custom override) and something hands that key
// straight to PFCPrimaryDisplayName, which title-cases anything the registry does not recognise.
// The UUID then IS the category's name, on screen and in the API.
//
// Fixing the six call sites does not stop a seventh. Every one of them looked reasonable in
// isolation, none had a test that fed a custom token, and the whole suite stayed green throughout —
// so review and coverage both demonstrably failed to catch this class. Hence a mechanical guard:
// resolveCategoryDisplay is the single choke point, and this fails the moment a new caller bypasses
// it with a grouping key.
//
// It deliberately allows pfcDisplayName and PFCPrimaryDisplayName on values that are NOT grouping
// keys — a PFC enum from the registry, a parent primary, a budget category's own enum — because
// those cannot be custom tokens. The test therefore matches on the ARGUMENT, not the function.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// groupingKeyArgs name expressions that carry an analytics GROUPING KEY — the values that can be a
// `custom:<uuid>` token. Passing one of these to the PFC registry is the defect.
var groupingKeyArgs = []string{
	"row.Category",
	"c.Category",
	"r.Category",
	"category", // the request param on GetCategoryTotalForPeriod
	"tc",       // the top-category loop variable in GetSpendingTrends
}

func TestNoAnalyticsReadResolvesAGroupingKeyThroughThePFCRegistry(t *testing.T) {
	const file = "analytics_service.go"
	src, err := os.ReadFile(file)
	require.NoError(t, err)

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	require.NoError(t, err)

	banned := map[string]bool{"PFCPrimaryDisplayName": true, "pfcDisplayName": true}

	var offenders []string
	ast.Inspect(parsed, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || !banned[ident.Name] || len(call.Args) != 1 {
			return true
		}
		arg := strings.TrimSpace(string(src[fset.Position(call.Args[0].Pos()).Offset:fset.Position(call.Args[0].End()).Offset]))
		for _, key := range groupingKeyArgs {
			if arg == key {
				offenders = append(offenders,
					file+":"+itoa(fset.Position(call.Pos()).Line)+"  "+ident.Name+"("+arg+")")
			}
		}
		return true
	})

	require.Empty(t, offenders,
		"a grouping key was passed to the PFC registry (#1626). For a custom-categorised row that "+
			"key is `custom:<uuid>`, the registry misses it, and enumToTitleCase turns the UUID into "+
			"the category's NAME — on screen and in the API. Route it through "+
			"resolveCategoryDisplay(key, storedDisplay) instead, which short-circuits custom tokens "+
			"before the registry ever sees them. Offenders:\n  %s",
		strings.Join(offenders, "\n  "))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
