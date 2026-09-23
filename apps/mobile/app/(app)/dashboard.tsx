import { router } from 'expo-router'
import { useCallback } from 'react'
import { useMobileAuth } from '../../src/auth/mobile-auth'
import { setAuthRecoveryHandler } from '../../src/bootstrap/api-runtime'
import { Button, Screen, ScreenHeader, Stack, Text } from '../../src/components/ui'

/**
 * The minimal guarded placeholder at `/dashboard`.
 *
 * It is deliberately NOT `app/(app)/index.tsx`: a group `index` would resolve to `/` and collide
 * with the public home route. The real product surface is owned by #1655.
 *
 * Sign-out is the session lifecycle (todo 20): the button drives the adapter's `signOut` (which
 * drops Clerk's session, clearing the in-memory user identity and the token it would mint),
 * clears the one-shot 401 recovery handler, and leaves the guarded group.
 *
 * `hasEverAuthenticated` in `@boostedbudget/api-client/client` is NOT reset here — it is only
 * reset by `configureApiClient`, which `initializeMobileRuntime` calls exactly once and which the
 * app must never call again. Clearing the recovery handler is therefore the mechanism that stops a
 * 401 arriving after sign-out from re-triggering recovery; the clear happens BEFORE `signOut` so a
 * response that races the sign-out cannot re-enter it.
 */
export default function DashboardScreen() {
  const { userId, signOut } = useMobileAuth()

  const handleSignOut = useCallback(async () => {
    setAuthRecoveryHandler(null)
    try {
      await signOut()
    } finally {
      router.replace('/(auth)/sign-in')
    }
  }, [signOut])

  return (
    <Screen testID="dashboard-screen" header={<ScreenHeader title="Dashboard" />} scrollable>
      <Stack gap={4} className="p-bb-6">
        <Text variant="heading">You are signed in</Text>
        <Text testID="dashboard-identity" tone="muted">
          {userId ?? 'Signed in'}
        </Text>
        <Button
          testID="dashboard-sign-out"
          label="Sign out"
          variant="outline"
          fullWidth
          onPress={handleSignOut}
        />
      </Stack>
    </Screen>
  )
}