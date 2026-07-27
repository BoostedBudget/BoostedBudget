/**
 * Location probe test utility
 *
 * Issue #1437 (react-router v7 -> v8 merge): the global `mock.module('react-router', ...)`
 * that used to stub `useNavigate` cannot work once `react-router-dom` and `react-router`
 * are the same package — mocking it makes bun re-instantiate the module, so a test's
 * `MemoryRouter` and the hooks under test end up bound to different context objects.
 *
 * The fix is to assert real navigation instead of a mocked function: render a small
 * probe component as a sibling of the routed UI (inside the same `MemoryRouter`) that
 * records `useLocation()` on every render.
 *
 * `LocationProbe` is created by a factory (rather than declared at module scope) so that
 * `eslint-plugin-react-hooks`'s `react-hooks/immutability` rule — which flags components
 * mutating a variable declared outside their own function scope — doesn't flag the
 * mutation of `locationRef.current`. The mutated variable is scoped inside
 * `createLocationHarness()`, not at the module's top level.
 *
 * @example
 * const { LocationProbe, locationRef, currentUrl, navigationCount, reset } = createLocationHarness()
 *
 * function renderPage() {
 *   return render(
 *     <MemoryRouter>
 *       <LocationProbe />
 *       <PageUnderTest />
 *     </MemoryRouter>
 *   )
 * }
 *
 * beforeEach(() => reset())
 *
 * it('navigates on click', () => {
 *   renderPage()
 *   fireEvent.click(screen.getByRole('button'))
 *   expect(locationRef.current.pathname).toBe('/expected')
 * })
 */
import { useLocation } from 'react-router'

export interface ProbedLocation {
  pathname: string
  search: string
  state: unknown
}

export interface LocationHarness {
  /** Mutable box holding the most recently observed location. */
  locationRef: { current: ProbedLocation }
  /** Render as a sibling of the routed UI, inside the same MemoryRouter, to observe navigation. */
  LocationProbe: () => null
  /** `pathname + search` of the most recently observed location. */
  currentUrl: () => string
  /**
   * Number of push-navigations observed since the last reset() (the initial mount's
   * location is not counted). Mirrors what `mockNavigate.toHaveBeenCalledTimes(n)` used
   * to assert, since MemoryRouter mints a fresh history `key` per push-navigation.
   */
  navigationCount: () => number
  /** Clears recorded state; call from `beforeEach`. */
  reset: () => void
}

function initialLocation(): ProbedLocation {
  return { pathname: '', search: '', state: null }
}

export function createLocationHarness(): LocationHarness {
  const locationRef: { current: ProbedLocation } = { current: initialLocation() }
  const navigationKeys: string[] = []

  function LocationProbe() {
    const loc = useLocation()
    locationRef.current = { pathname: loc.pathname, search: loc.search, state: loc.state }
    if (navigationKeys.at(-1) !== loc.key) navigationKeys.push(loc.key)
    return null
  }

  return {
    locationRef,
    LocationProbe,
    currentUrl: () => locationRef.current.pathname + locationRef.current.search,
    navigationCount: () => Math.max(0, navigationKeys.length - 1),
    reset: () => {
      locationRef.current = initialLocation()
      navigationKeys.length = 0
    },
  }
}
