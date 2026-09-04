// Package mockassert holds assertions about testify mocks that testify itself gets wrong.
//
// Issue #1569 task 19. It exists for one function, and that function exists because of one defect
// class that this issue hit repeatedly.
package mockassert

import (
	"testing"

	"github.com/stretchr/testify/mock"
)

// NoCallTo fails if the mock recorded any call to the named method, whatever its arguments.
//
// ⚠ WHY NOT testify's AssertNotCalled. `m.AssertNotCalled(t, "Method")` with NO argument matchers
// asserts that the method was never called with an EMPTY argument list. For a method that takes
// arguments — which is almost all of them — that is a condition that can never be true, so the
// assertion can never fail. It passes while the call is happening.
//
// It is not a hypothetical either. #1569 found 363 of them in this tree. A reviewer rewrote every
// one mechanically and no test newly failed, so nothing was masked AT THAT MOMENT — but the form
// goes live the instant someone adds a `.Maybe()` or a looser expectation nearby, which is exactly
// how it was found. A test that cannot fail is worse than no test, because it is counted.
//
// The correct testify spelling is `AssertNotCalled(t, "Method", mock.Anything, mock.Anything, ...)`
// with one matcher per parameter — which is silently wrong again the day the signature gains a
// parameter. NoCallTo matches on the method name, which is what these assertions actually mean.
//
// TestNoNewNoMatcherAssertNotCalled (same package) stops new no-matcher calls being added.
func NoCallTo(t *testing.T, m *mock.Mock, method string) {
	t.Helper()
	if args, called := firstCallTo(m, method); called {
		t.Fatalf("expected no call to %s, but it was called with %v", method, args)
	}
}

// firstCallTo is the decision NoCallTo makes, separated so that it can be tested without a
// *testing.T that has to be failed on purpose. Returns the arguments of the first recorded call to
// the named method, and whether there was one.
func firstCallTo(m *mock.Mock, method string) (mock.Arguments, bool) {
	for _, c := range m.Calls {
		if c.Method == method {
			return c.Arguments, true
		}
	}
	return nil, false
}
