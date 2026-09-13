import '../global.css'

import { QueryClientProvider } from '@tanstack/react-query'
import { Stack } from 'expo-router'
import { StatusBar } from 'expo-status-bar'
import { useState } from 'react'
import { createMobileQueryClient, initializeMobileRuntime } from '../src/bootstrap/runtime'

initializeMobileRuntime({ EXPO_PUBLIC_API_URL: process.env.EXPO_PUBLIC_API_URL })

export default function RootLayout() {
  const [queryClient] = useState(createMobileQueryClient)

  return (
    <QueryClientProvider client={queryClient}>
      <StatusBar style="auto" />
      <Stack screenOptions={{ headerBackTitle: 'Back' }}>
        <Stack.Screen name="index" options={{ title: 'BoostedBudget' }} />
        <Stack.Screen name="readiness" options={{ title: 'App readiness' }} />
      </Stack>
    </QueryClientProvider>
  )
}
