package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// #1631. A Postgres DATE column held in Go as time.Time and serialized directly emits
// "2026-01-02T00:00:00Z", which shifts back a day for every user west of UTC once a local-time
// formatter touches it. The fix used throughout this codebase is the DISPLAY SIBLING: the
// time.Time keeps its db tag and becomes json:"-" so scans, SQL binds and keyset cursors are
// untouched, and a string sibling formatted at the DB edge is what reaches clients.
//
// That pattern has one failure mode, and it is silent. A construction site that sets the
// time.Time and forgets the sibling emits `"date": ""` — a present key with an empty value.
// Nothing fails: the struct compiles, the scan works, the JSON is valid, and the client renders
// "Invalid Date" or nothing at all. It surfaces as a user-visible blank, not as a test failure.
//
// This guard removes the silence. It discovers every (time.Time json:"-", XDisplay) pair in
// internal/domain by parsing the source, then requires that any composite literal anywhere under
// internal/ which sets the time.Time field ALSO sets its display sibling.
//
// ⚠ What it does NOT check, stated so nobody reads more assurance into a green run than is here:
//   - Separate assignment (`resp.StartDate = t` on its own line) is not a composite literal and
//     is not seen. Composite literals are how every current site is written.
//   - It does not check the sibling's VALUE. A site passing the wrong format, or a different
//     day, satisfies this guard. Correctness of the value is the per-struct tests' job.
//   - A literal that sets NEITHER field is fine by design — a zero value with no display is a
//     legitimate empty response.
//
// Two shapes are legitimately exempt, and both are recognised rather than allowlisted away:
//   - CONSTRUCT-THEN-POPULATE. The dominant read-path pattern builds the struct and then calls
//     its Populate*Display() method a few lines later (transaction_repository.go's unified feed,
//     NewBankingActivityItem, every budget/savings-goal repository read). A literal inside a
//     function that calls such a method on any receiver is accepted.
//   - INGESTION-PATH literals, which are built to be INSERTed and never serialized from that
//     value; the display is derived on the way back out by the scan finalizer. These cannot be
//     told apart from response construction by the AST, so they are allowlisted BY SITE WITH A
//     REASON, the same way this package's history-delete guards work. An allowlist entry is a
//     claim a human made and nothing verifies its truth — re-read the code before trusting one.
func TestEveryDateDisplaySiblingIsPopulatedWhereItsTimeIsSet(t *testing.T) {
	violations, pairCount, err := findDisplaySiblingViolations("../domain", "..", ingestionPathLiterals())
	if err != nil {
		t.Fatalf("scanning for display-sibling violations: %v", err)
	}
	// A zero pair count means the discovery half broke (a renamed suffix, a moved directory), and
	// a guard that silently measures nothing is worse than no guard — it reads as a pass forever.
	if pairCount == 0 {
		t.Fatal("found no (time.Time json:\"-\", *Display) field pairs in ../domain. Either the " +
			"pattern was renamed or this scan is broken; refusing to pass without checking anything.")
	}
	if len(violations) > 0 {
		t.Fatal(renderDisplaySiblingViolations(violations))
	}
}

// The guard's own meta-test: it constructs the violation the guard exists to catch and proves the
// guard fails on it, then proves each exemption is real rather than a blanket pass. Without this,
// a refactor that quietly stopped detecting anything would look identical to a clean tree.
func TestDisplaySiblingGuardCatchesTheViolationItDescribes(t *testing.T) {
	domainSrc := `package domain

import "time"

type Thing struct {
	Date        time.Time ` + "`" + `json:"-"` + "`" + `
	DateDisplay string    ` + "`" + `json:"date"` + "`" + `
}

func (x *Thing) PopulateDateDisplay() { x.DateDisplay = x.Date.Format(time.DateOnly) }
`
	cases := []struct {
		name       string
		callerSrc  string
		allowlist  map[string]string
		wantIssues int
	}{
		{
			name: "bare literal setting the time and not the display is caught",
			callerSrc: `package caller

func build() *Thing { return &Thing{Date: now()} }
`,
			wantIssues: 1,
		},
		{
			name: "literal setting both fields is clean",
			callerSrc: `package caller

func build() *Thing { return &Thing{Date: now(), DateDisplay: "2026-01-02"} }
`,
			wantIssues: 0,
		},
		{
			name: "construct-then-populate in the same function is clean",
			callerSrc: `package caller

func build() *Thing {
	t := &Thing{Date: now()}
	t.PopulateDateDisplay()
	return t
}
`,
			wantIssues: 0,
		},
		{
			name: "a literal setting NEITHER field is clean",
			callerSrc: `package caller

func build() *Thing { return &Thing{} }
`,
			wantIssues: 0,
		},
		{
			name: "an allowlisted ingestion site is exempt",
			callerSrc: `package caller

func build() *Thing { return &Thing{Date: now()} }
`,
			allowlist:  map[string]string{"caller.go:Thing": "built for insert only"},
			wantIssues: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dom := filepath.Join(root, "domain")
			if err := os.MkdirAll(dom, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dom, "thing.go"), []byte(domainSrc), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "caller.go"), []byte(tc.callerSrc), 0o600); err != nil {
				t.Fatal(err)
			}

			violations, pairCount, err := findDisplaySiblingViolations(dom, root, tc.allowlist)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if pairCount != 1 {
				t.Fatalf("expected to discover 1 field pair, got %d — the DISCOVERY half is broken, "+
					"which would make the whole guard pass vacuously", pairCount)
			}
			if len(violations) != tc.wantIssues {
				t.Fatalf("expected %d violation(s), got %d: %v", tc.wantIssues, len(violations), violations)
			}
		})
	}
}

type displaySiblingViolation struct{ pos, structName, timeField, displayField string }

func (v displaySiblingViolation) String() string {
	return v.pos + ": " + v.structName + "." + v.timeField
}

// ingestionPathLiterals lists construction sites that build one of these structs to be WRITTEN to
// the database rather than serialized from that value; the display is derived on the way back out
// by the repository scan. The AST cannot tell these apart from response construction, so they are
// allowlisted by site with a reason — the same shape as this package's history-delete guards, and
// with the same caveat: an entry is a claim a human made, and nothing here verifies it is true.
func ingestionPathLiterals() map[string]string {
	return map[string]string{
		"investment_sync_service.go:InvestmentTransaction": "built from a Plaid payload for UpsertBatch; " +
			"the row is serialized only after being read back, where the repository scan populates DateDisplay.",
		"plaid_mapper.go:Transaction": "maps a Plaid transaction for insert; " +
			"every read path back out calls PopulateDateDisplay (transaction_repository.go).",
	}
}

func renderDisplaySiblingViolations(violations []displaySiblingViolation) string {
	sort.Slice(violations, func(i, j int) bool { return violations[i].pos < violations[j].pos })
	var b strings.Builder
	b.WriteString("a DATE-shaped time.Time is set without its display sibling, which serializes as an EMPTY STRING:\n")
	for _, v := range violations {
		b.WriteString("  " + v.pos + ": " + v.structName + " sets " + v.timeField +
			" but not " + v.displayField + "\n")
	}
	b.WriteString("\nThe time.Time carries json:\"-\", so the sibling is the ONLY field that reaches the\n" +
		"client. Either set it in the literal, call the struct's Populate*Display() method in the\n" +
		"same function, or — if this is an insert-only path — allowlist it in ingestionPathLiterals\n" +
		"with a reason.\n")
	return b.String()
}

func findDisplaySiblingViolations(domainDir, scanRoot string, allowlist map[string]string) ([]displaySiblingViolation, int, error) {

	fset := token.NewFileSet()
	pairs, err := discoverDisplaySiblingPairs(fset, domainDir)
	if err != nil {
		return nil, 0, err
	}

	var violations []displaySiblingViolation

	err = filepath.Walk(scanRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Generated and vendored trees are not hand-written construction sites.
			if base := info.Name(); base == "testdata" || base == "vendor" || base == "mocks" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// A file that does not parse is a compile error the compiler will report with a far
			// better message than this guard would.
			return nil //nolint:nilerr
		}
		// Functions in this file that call a Populate*Display() method somewhere in their body.
		// A literal inside one of those is the construct-then-populate pattern, not a defect.
		populating := map[*ast.FuncDecl]bool{}
		for _, decl := range file.Decls {
			fn, isFn := decl.(*ast.FuncDecl)
			if !isFn || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel &&
					strings.HasPrefix(sel.Sel.Name, "Populate") &&
					strings.HasSuffix(sel.Sel.Name, "Display") {
					populating[fn] = true
				}
				return true
			})
		}

		ast.Inspect(file, func(n ast.Node) bool {
			// Enter each function separately so a literal can be attributed to its owner.
			if fn, isFn := n.(*ast.FuncDecl); isFn {
				if populating[fn] {
					return false // whole function exempt: it populates displays itself
				}
				return true
			}
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name := compositeLitTypeName(lit)
			fieldPairs, tracked := pairs[name]
			if !tracked {
				return true
			}
			set := map[string]bool{}
			for _, elt := range lit.Elts {
				kv, isKV := elt.(*ast.KeyValueExpr)
				if !isKV {
					continue
				}
				if id, isID := kv.Key.(*ast.Ident); isID {
					set[id.Name] = true
				}
			}
			if _, exempt := allowlist[filepath.Base(path)+":"+name]; exempt {
				return true
			}
			for _, fp := range fieldPairs {
				if set[fp.timeField] && !set[fp.displayField] {
					violations = append(violations, displaySiblingViolation{
						pos:          fset.Position(lit.Pos()).String(),
						structName:   name,
						timeField:    fp.timeField,
						displayField: fp.displayField,
					})
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	return violations, len(pairs), nil
}

type displaySiblingPair struct{ timeField, displayField string }

// discoverDisplaySiblingPairs parses the domain package and returns, per struct name, every
// time.Time field tagged json:"-" that has a same-named XDisplay sibling. Derived from the source
// rather than hand-listed so a newly converted field is covered the moment it is written.
func discoverDisplaySiblingPairs(fset *token.FileSet, dir string) (map[string][]displaySiblingPair, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]displaySiblingPair{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			return nil, perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, isStruct := ts.Type.(*ast.StructType)
			if !isStruct {
				return true
			}
			// Field names present on this struct, so a sibling is only claimed when it exists.
			present := map[string]bool{}
			for _, f := range st.Fields.List {
				for _, nm := range f.Names {
					present[nm.Name] = true
				}
			}
			for _, f := range st.Fields.List {
				if f.Tag == nil || !strings.Contains(f.Tag.Value, `json:"-"`) {
					continue
				}
				if !isTimeTime(f.Type) {
					continue
				}
				for _, nm := range f.Names {
					sibling := nm.Name + "Display"
					if present[sibling] {
						out[ts.Name.Name] = append(out[ts.Name.Name], displaySiblingPair{nm.Name, sibling})
					}
				}
			}
			return true
		})
	}
	return out, nil
}

func isTimeTime(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "time" && sel.Sel.Name == "Time"
}

// compositeLitTypeName returns the bare type name a composite literal constructs, for the forms
// this codebase writes: `domain.X{}`, `X{}`, `&domain.X{}`, and the elided element type inside a
// slice literal (`[]domain.X{{...}}`), which yields "" and is simply not tracked.
func compositeLitTypeName(lit *ast.CompositeLit) string {
	switch t := lit.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}
