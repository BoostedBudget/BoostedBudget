import '../global.css'

import { QueryClientProvider } from '@tanstack/react-query'
import { Stack, ThemeProvider } from 'expo-router'
import { StatusBar } from 'expo-status-bar'
import { VariableContextProvider } from 'nativewind'
import { ReactNode, useEffect, useMemo, useState } from 'react'
import { Appearance, Platform } from 'react-native'
import { AuthProviders } from '../src/auth/AuthProviders'
import { MobileAuthProvider } from '../src/auth/mobile-auth'
import { mobileTokenCache } from '../src/auth/token-cache'
import { createMobileQueryClient, initializeMobileRuntime } from '../src/bootstrap/runtime'
import { getNavigationAppearance } from '../src/theme/navigation'
import { applyWebThemeVariables, themeVariables } from '../src/theme/runtime'
import { useTheme } from '../src/theme/ThemeProvider'

// Captured at module scope so the runtime is configured exactly once, before any provider mounts.
// `apiUrlConfigured` is intentionally not destructured: nothing in this layout consumes it, and an
// unused binding would fail `@typescript-eslint/no-unused-vars`.
const { authAvailable, clerkPublishableKey } = initializeMobileRuntime({
  EXPO_PUBLIC_API_URL: process.env.EXPO_PUBLIC_API_URL,
  EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY: process.env.EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY,
})

export function MobileAppProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createMobileQueryClient)

  return (
    <QueryClientProvider client={queryClient}>
      <MobileAuthProvider
        authAvailable={authAvailable}
        publishableKey={clerkPublishableKey}
        tokenCache={mobileTokenCache}
      >
        <AuthProviders>{children}</AuthProviders>
      </MobileAuthProvider>
    </QueryClientProvider>
  )
}

export function ThemedApp({ children }: { children: ReactNode }) {
  const { resolvedTheme, hydrated } = useTheme()
  const variables = useMemo(() => themeVariables(resolvedTheme), [resolvedTheme])

  // Both writes are gated on `hydrated`. Before hydration `resolvedTheme` is derived from the
  // still-`system` preference, so writing it would clobber the pre-paint bootstrap's persisted
  // theme (a dark -> light -> dark flash on web) and briefly set the wrong native scheme.
  useEffect(() => {
    if (!hydrated) return
    if (Platform.OS !== 'web') {
      Appearance.setColorScheme(resolvedTheme)
    }
  }, [hydrated, resolvedTheme])

  useEffect(() => {
    if (!hydrated) return
    if (Platform.OS === 'web') {
      applyWebThemeVariables(resolvedTheme, variables, globalThis.document.documentElement)
    }
  }, [hydrated, resolvedTheme, variables])

  const { theme, statusBarStyle } = getNavigationAppearance(resolvedTheme)

  return (
    <ThemeProvider value={theme}>
      <StatusBar style={statusBarStyle} />
      {Platform.OS === 'web' ? (
        children
      ) : (
        <VariableContextProvider value={variables}>{children}</VariableContextProvider>
      )}
    </ThemeProvider>
  )
}

export default function RootLayout() {
  return (
    <MobileAppProviders>
      <ThemedApp>
        <Stack screenOptions={{ headerBackTitle: 'Back' }}>
          <Stack.Screen name="index" options={{ title: 'BoostedBudget' }} />
          <Stack.Screen name="readiness" options={{ title: 'App readiness' }} />
          <Stack.Screen name="gallery" options={{ title: 'Primitive gallery' }} />
        </Stack>
      </ThemedApp>
    </MobileAppProviders>
  )
}