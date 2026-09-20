import '../global.css'

import { QueryClientProvider } from '@tanstack/react-query'
import { Stack, ThemeProvider } from 'expo-router'
import { StatusBar } from 'expo-status-bar'
import { ReactNode, useState } from 'react'
import { useColorScheme } from 'react-native'
import { createMobileQueryClient, initializeMobileRuntime } from '../src/bootstrap/runtime'
import { ToastProvider } from '../src/components/ui/ToastProvider'
import { getNavigationAppearance } from '../src/theme/navigation'
import { ThemeProvider as ThemePreferenceProvider } from '../src/theme/ThemeProvider'

initializeMobileRuntime({ EXPO_PUBLIC_API_URL: process.env.EXPO_PUBLIC_API_URL })

export function MobileAppProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createMobileQueryClient)

  return (
    <QueryClientProvider client={queryClient}>
      <ThemePreferenceProvider>
        <ToastProvider>{children}</ToastProvider>
      </ThemePreferenceProvider>
    </QueryClientProvider>
  )
}

export default function RootLayout() {
  const { theme, statusBarStyle } = getNavigationAppearance(useColorScheme())

  return (
    <MobileAppProviders>
      <ThemeProvider value={theme}>
        <StatusBar style={statusBarStyle} />
        <Stack screenOptions={{ headerBackTitle: 'Back' }}>
          <Stack.Screen name="index" options={{ title: 'BoostedBudget' }} />
          <Stack.Screen name="readiness" options={{ title: 'App readiness' }} />
        </Stack>
      </ThemeProvider>
    </MobileAppProviders>
  )
}
