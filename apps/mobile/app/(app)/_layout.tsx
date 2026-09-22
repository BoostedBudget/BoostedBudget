import { Redirect, Stack } from 'expo-router'
import { useMobileAuth } from '../../src/auth/mobile-auth'

/**
 * The guarded application group.
 *
 * It is deliberately a plain `Stack` — never the `.Protected` variant (open iOS defect
 * expo/expo#37305). The guard is the adapter plus `<Redirect>`: while the adapter has not loaded
 * it renders nothing (no premature redirect), a signed-out or inert adapter is sent to the
 * sign-in route, and only a signed-in user reaches the group's screens.
 */
export default function AppLayout() {
  const { isLoaded, isSignedIn } = useMobileAuth()

  if (!isLoaded) return null

  if (!isSignedIn) return <Redirect href="/(auth)/sign-in" />

  return <Stack screenOptions={{ headerBackTitle: 'Back' }} />
}