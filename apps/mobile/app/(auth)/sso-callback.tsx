import { router } from 'expo-router'
import { Button, Screen, ScreenHeader, Spinner, Stack, Text } from '../../src/components/ui'

/**
 * The OAuth redirect target.
 *
 * `useMobileSSO().startSSOFlow` opens the provider in an auth session and resolves when the
 * browser returns to the app scheme (`boostedbudget://sso-callback`, see `app.json`). On a warm
 * start the session resolves in place and this route is never shown; on a cold start the OS opens
 * the app here instead. There is no session handle to complete from this route, so it renders a
 * calm in-progress state with a manual way back rather than guessing at auth state — `isSignedIn`
 * is not updated synchronously and is unreliable as a guard.
 */
export default function SSOCallbackScreen() {
  return (
    <Screen testID="sso-callback-screen" header={<ScreenHeader title="Signing in" />}>
      <Stack gap={4} className="p-bb-6">
        <Spinner testID="sso-callback-spinner" label="Completing sign-in" />
        <Text variant="heading">Completing sign-in</Text>
        <Text tone="muted">
          Finishing the sign-in you started. If this screen does not advance, return to sign in and
          try again.
        </Text>
        <Button
          testID="sso-callback-back"
          variant="link"
          label="Back to sign in"
          onPress={() => router.replace('/sign-in')}
          fullWidth
        />
      </Stack>
    </Screen>
  )
}