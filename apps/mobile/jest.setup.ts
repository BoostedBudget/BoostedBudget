import { beforeAll, beforeEach } from '@jest/globals'
import { dirname, resolve } from 'node:path'
import { readFileSync } from 'node:fs'
import tailwindcss from '@tailwindcss/postcss'
import postcss from 'postcss'
import { registerCSS } from 'react-native-css/jest'
import { setColorScheme } from './src/test/color-scheme'

jest.mock('expo-router', () => {
  const Stack = ({ children }: { children?: React.ReactNode }) => children ?? null
  Stack.Screen = () => null
  const fonts = {
    regular: { fontFamily: 'System', fontWeight: '400' },
    medium: { fontFamily: 'System', fontWeight: '500' },
    bold: { fontFamily: 'System', fontWeight: '600' },
    heavy: { fontFamily: 'System', fontWeight: '700' },
  }
  // The router singleton and its hooks. The guarded `(app)` group calls `<Redirect>`, and
  // `src/auth/navigation.ts` plus the auth screens call `router.replace(...)`. The real
  // `expo-router` needs a live navigation container; this mock lets a shell render in jest
  // and lets a test assert navigation by reading `router.replace`.
  const router = {
    push: jest.fn(),
    replace: jest.fn(),
    navigate: jest.fn(),
    back: jest.fn(),
    dismiss: jest.fn(),
    setParams: jest.fn(),
    canGoBack: jest.fn(() => false),
  }
  return {
    DarkTheme: { dark: true, colors: {}, fonts },
    DefaultTheme: { dark: false, colors: {}, fonts },
    Link: ({ children }: { children: React.ReactNode }) => children,
    Redirect: () => null,
    Stack,
    ThemeProvider: jest.fn(({ children }: { children: React.ReactNode }) => children),
    router,
    useRouter: () => router,
    useSegments: () => [] as string[],
    usePathname: () => '/',
    useLocalSearchParams: () => ({} as Record<string, string | string[]>),
  }
})

jest.mock('expo-status-bar', () => ({
  StatusBar: jest.fn(() => null),
}))

// The native AsyncStorage build throws at import time when no native module is
// registered, which is always the case under jest. Use the package's own
// in-memory mock so the theme provider can be exercised deterministically.
jest.mock('@react-native-async-storage/async-storage', () =>
  require('@react-native-async-storage/async-storage/jest/async-storage-mock'),
)

// `expo-secure-store` ships no jest mock (unlike AsyncStorage), and its native module
// throws at import time under jest. Provide an in-memory mock so the Clerk token cache is
// exercisable and a test can force a read rejection.
jest.mock('expo-secure-store', () => {
  const store = new Map<string, string>()
  return {
    AFTER_FIRST_UNLOCK: 0,
    getItemAsync: jest.fn(async (key: string) => store.get(key) ?? null),
    setItemAsync: jest.fn(async (key: string, value: string) => {
      store.set(key, value)
    }),
    deleteItemAsync: jest.fn(async (key: string) => {
      store.delete(key)
    }),
  }
})

// `@clerk/expo/token-cache` builds its cache over `expo-secure-store` at module scope.
// Mirror the real implementation (a failed read deletes the corrupt entry and resolves
// null) so the cache contract is exercised without loading the real module.
jest.mock('@clerk/expo/token-cache', () => {
  const SecureStore = require('expo-secure-store')
  const options = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK }
  return {
    tokenCache: {
      getToken: async (key: string) => {
        try {
          return await SecureStore.getItemAsync(key, options)
        } catch {
          await SecureStore.deleteItemAsync(key, options)
          return null
        }
      },
      saveToken: (key: string, token: string) => SecureStore.setItemAsync(key, token, options),
      clearToken: (key: string) => SecureStore.deleteItemAsync(key, options),
    },
  }
})

// A controllable, type-checked `@clerk/expo` mock. The real module transitively imports
// `@clerk/clerk-js` and expects a live `ClerkProvider`; the unit tier must never instantiate it.
// `jest.hoisted` is unavailable under this jest-expo runtime, so the state is a hoisted `var` the
// factory closes over lazily (read at render time, after the assignment below has run). The
// `satisfies Partial<typeof import('@clerk/expo')>` clause fails typecheck if the real surface
// drops or renames a member this mock provides. Tests drive auth state through the helpers below.
type ClerkExpoModule = typeof import('@clerk/expo')

interface MockSetActiveParams {
  session: string
  navigate: (params: {
    session: { currentTask?: { key: string } }
    decorateUrl: (url: string) => string
  }) => void
}

export interface MockClerkAuthState {
  isLoaded: boolean
  isSignedIn: boolean
  userId: string | null
  getToken: jest.Mock<Promise<string | null>, []>
}

var mockClerkState = {
  auth: {
    isLoaded: true,
    isSignedIn: false,
    userId: null as string | null,
    getToken: jest.fn<Promise<string | null>, []>(async () => null),
  } satisfies MockClerkAuthState,
  clerk: {
    setActive: jest.fn<Promise<void>, [MockSetActiveParams]>(async () => {}),
    signOut: jest.fn<Promise<void>, []>(async () => {}),
    session: null as { currentTask?: { key: string } } | null,
  },
  session: {
    isLoaded: true,
    isSignedIn: false,
    session: null as { currentTask?: { key: string } } | null,
  },
  signIn: { signIn: null as unknown },
  signUp: { signUp: null as unknown },
}

jest.mock('@clerk/expo', () =>
  ({
    ClerkProvider: (({ children }: { children?: React.ReactNode }) =>
      children ?? null) as unknown as ClerkExpoModule['ClerkProvider'],
    useAuth: (() => mockClerkState.auth) as unknown as ClerkExpoModule['useAuth'],
    useClerk: (() => mockClerkState.clerk) as unknown as ClerkExpoModule['useClerk'],
    useSession: (() => mockClerkState.session) as unknown as ClerkExpoModule['useSession'],
    useSignIn: (() => mockClerkState.signIn) as unknown as ClerkExpoModule['useSignIn'],
    useSignUp: (() => mockClerkState.signUp) as unknown as ClerkExpoModule['useSignUp'],
  }) satisfies Partial<ClerkExpoModule>,
)

/** Per-test helper: the mutable mock state, for advanced control (SSO sessions, session tasks). */
export function mockClerk(): typeof mockClerkState {
  return mockClerkState
}

/** Per-test helper: override the mocked Clerk auth state. */
export function setMockClerkAuth(next: Partial<MockClerkAuthState>): void {
  Object.assign(mockClerkState.auth, next)
}

/** Per-test helper: restore the default signed-out Clerk auth state. */
export function resetMockClerkAuth(): void {
  mockClerkState.auth.isLoaded = true
  mockClerkState.auth.isSignedIn = false
  mockClerkState.auth.userId = null
  mockClerkState.auth.getToken.mockReset()
  mockClerkState.auth.getToken.mockResolvedValue(null)
}

const MOBILE_ROOT = dirname(require.resolve('./package.json'))
const GLOBAL_CSS_PATH = resolve(MOBILE_ROOT, 'global.css')

// Probe utilities that no production source references yet. Jest-only: the native build picks
// these up from real component usage, while the probe test must resolve them regardless of what
// Tailwind's content scan happens to see. `base` pins the scan to `apps/mobile` so the compiled
// stylesheet does not depend on the directory jest was launched from.
const PROBE_UTILITIES = '@source inline("bg-background text-foreground p-bb-4");'

let compiledCss: string | undefined

beforeAll(async () => {
  // `registerCSS` compiles plain CSS into `StyleCollection`; it does not expand Tailwind
  // directives (`@import 'tailwindcss'`, `@source`) or package `@import`s, so `global.css` goes
  // through the real Tailwind/PostCSS pipeline first. The Tailwind pass is the expensive half,
  // so it runs once per test file; the react-native-css compile in `beforeEach` is ~17ms.
  const source = `${readFileSync(GLOBAL_CSS_PATH, 'utf8')}\n${PROBE_UTILITIES}\n`
  const { css } = await postcss([tailwindcss({ base: MOBILE_ROOT })]).process(source, {
    from: GLOBAL_CSS_PATH,
  })
  compiledCss = css
})

beforeEach(() => {
  // Resolving `className` in jest needs BOTH halves:
  //   1. the `react-native` -> `react-native-css/components` import rewrite, which only the
  //      jest-scoped `nativewind/babel` preset in `babel.config.js` performs (Metro does it in
  //      its resolver for the native build), and
  //   2. `registerCSS`, which compiles the plain CSS into `StyleCollection` for the rewritten
  //      components to read. `registerCSS` alone is not enough — without (1) the plain
  //      `react-native` components ignore `className` entirely.
  // `react-native-css/jest` clears `StyleCollection` in its own `beforeEach`, registered by the
  // import above and therefore running before this one, so re-register per test.
  if (compiledCss) {
    registerCSS(compiledCss)
  }
  // `react-native-css/jest` resets the source build's `colorScheme` observable, which the runtime
  // the rewritten components use does not read, so a `dark` test leaks into the next one. Reset
  // explicitly; a test renders in dark by calling `setColorScheme('dark')` itself.
  setColorScheme('light')
})
