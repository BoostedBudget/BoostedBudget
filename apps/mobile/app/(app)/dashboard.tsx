import { useMobileAuth } from '../../src/auth/mobile-auth'
import { Button, Screen, ScreenHeader, Stack, Text } from '../../src/components/ui'

/**
 * The minimal guarded placeholder at `/dashboard`.
 *
 * It is deliberately NOT `app/(app)/index.tsx`: a group `index` would resolve to `/` and collide
 * with the public home route. The real product surface is owned by #1655. The sign-out control is
 * rendered here so the guarded surface has one; its behaviour is wired in todo 20.
 */
export default function DashboardScreen() {
  const { userId } = useMobileAuth()

  return (
    <Screen testID="dashboard-screen" header={<ScreenHeader title="Dashboard" />} scrollable>
      <Stack gap={4} className="p-bb-6">
        <Text variant="heading">You are signed in</Text>
        <Text testID="dashboard-identity" tone="muted">
          {userId ?? 'Signed in'}
        </Text>
        <Button testID="dashboard-sign-out" label="Sign out" variant="outline" fullWidth />
      </Stack>
    </Screen>
  )
}