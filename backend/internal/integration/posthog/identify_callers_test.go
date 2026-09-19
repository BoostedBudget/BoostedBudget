package posthog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reservedPropertyWriteEvents are the two PostHog-reserved person-property events. They are not
// ordinary product events: "$set" mutates the person profile and "$identify" creates/merges one.
// Backend producers must never address them by raw string literal through the adapter —
// person-property writes go through typed surfaces (EmitPersonPropertyUnset, the gateway's
// contract-checked CaptureEvent), so a stray literal is an identity write nobody review-gated.
var reservedPropertyWriteEvents = map[string]struct{}{
	"$set":      {},
	"$identify": {},
}

// allowedPropertyWriteCallers is deliberately EMPTY. No backend non-test call site passes either
// reserved literal to Capture/Identify today. An entry is added only for a call site whose raw
// reserved-event write has had an explicit contract review; the empty map is the assertion that
// none has.
var allowedPropertyWriteCallers = map[string]struct{}{}

// propertyWriteCaller positions are the adapter-shaped methods this guard scans for. `Capture`
// takes its event name at argument index 1 (Capture(distinctID, event, properties)). `Identify`
// has no event-name argument at all — the method itself is the sanctioned `$identify` path — so
// it contributes no scannable position and can never be a raw reserved-literal write.
//
// DOCUMENTED RESIDUALS (cannot be seen from a call-site AST, or are outside it by design):
//   - client.go's `Event: "$set"` composite literal in EmitPersonPropertyUnset is a struct
//     literal, not an adapter call site. It is the ONE blessed SDK-level reserved write, and
//     TestOnlyClientAdapterConstructsPostHogSDKMessages pins client.go as the sole importer of
//     the posthog-go SDK, so no other file can construct one.
//   - A variable-built event name (Capture(id, evt, …) where evt is not a literal) is invisible
//     to a literal scan. That residual is mitigated by review of the legacy raw-Capture
//     inventory in TestLegacyRawCaptureProducersArePinned.
var propertyWriteCallerPositions = map[string]int{
	"Capture":  1,
	"Identify": -1,
}

// TestNoUnallowlistedPropertyWriteCaller is G8. It walks every backend non-test .go file and fails
// when a call site passes the string literal "$set" or "$identify" as the event argument of an
// adapter-shaped Capture/Identify method.
func TestNoUnallowlistedPropertyWriteCaller(t *testing.T) {
	backendRoot := filepath.Join("..", "..", "..")

	var offenders []string
	err := filepath.WalkDir(backendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, relErr := filepath.Rel(backendRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		for _, offender := range propertyWriteCallersIn(fset, file) {
			key := rel + ":" + offender
			if _, allowed := allowedPropertyWriteCallers[key]; allowed {
				continue
			}
			offenders = append(offenders, key)
		}
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, offenders,
		`backend call sites write a PostHog-reserved person-property event ("$set"/"$identify") by raw `+
			`string literal; route the write through EmitPersonPropertyUnset or the gateway and add an `+
			`explicit, reviewed allowlist entry if the raw write is truly intended`)
}

// TestPropertyWriteDetectorCatchesReservedLiterals proves the detector fires on the literal form.
// Without it, a detector that matched nothing would look identical to a clean tree.
func TestPropertyWriteDetectorCatchesReservedLiterals(t *testing.T) {
	find := func(source string) []string {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "fixture.go", source, 0)
		require.NoError(t, err)
		return propertyWriteCallersIn(fset, file)
	}

	setLiteral := find(`package fixture
func f(analytics Analytics, id string) {
	analytics.Capture(id, "$set", map[string]interface{}{"name": "x"})
}`)
	identifyLiteral := find(`package fixture
func f(analytics Analytics, id string) {
	analytics.Capture(id, "$identify", nil)
}`)
	variableEvent := find(`package fixture
func f(analytics Analytics, id, event string) {
	analytics.Capture(id, event, nil)
}`)
	typedName := find(`package fixture
func f(analytics Analytics, id string) {
	analytics.Capture(id, EventEmailChanged, nil)
}`)
	identifyCall := find(`package fixture
func f(analytics Analytics, id string) {
	analytics.Identify(id, map[string]interface{}{"name": "x"})
}`)

	assert.Equal(t, []string{`3:Capture(event="$set")`}, setLiteral)
	assert.Equal(t, []string{`3:Capture(event="$identify")`}, identifyLiteral)
	assert.Empty(t, variableEvent)
	assert.Empty(t, typedName)
	assert.Empty(t, identifyCall)
}

func propertyWriteCallersIn(fset *token.FileSet, file *ast.File) []string {
	var offenders []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		eventArg, scanned := propertyWriteCallerPositions[selector.Sel.Name]
		if !scanned || eventArg < 0 || len(call.Args) <= eventArg {
			return true
		}
		literal, ok := call.Args[eventArg].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		if _, reserved := reservedPropertyWriteEvents[value]; !reserved {
			return true
		}
		position := fset.Position(call.Pos())
		offenders = append(offenders,
			strconv.Itoa(position.Line)+`:`+selector.Sel.Name+`(event=`+strconv.Quote(value)+`)`)
		return true
	})
	return offenders
}
