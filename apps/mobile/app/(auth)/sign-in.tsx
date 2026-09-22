import { router } from 'expo-router'
import { useState } from 'react'
import { useMobileSignIn } from '../../src/auth/mobile-auth'
import { navigateToClerkDestination } from '../../src/auth/navigation'
import {
  Alert,
  Button,
  FormField,
  Inline,
  Screen,
  ScreenHeader,
  Stack,
  Text,
  TextField,
} from '../../src/components/ui'

/** Shown when an error object carries no user-safe message. Never a raw error dump. */
const GENERIC_ERROR_MESSAGE = 'Something went wrong. Please try again.'

/**
 * Extracts a user-facing message from a Clerk error envelope without ever stringifying an unknown
 * object. Clerk resolves `{ errors: [{ message }] }` for API failures; a thrown error is usually a
 * plain `Error` with a `message`.
 */
function describeAuthError(error: unknown): string {
  if (typeof error === 'string' && error.trim().length > 0) return error
  if (error !== null && typeof error === 'object') {
    const record = error as { message?: unknown; errors?: unknown }
    const first = Array.isArray(record.errors)
      ? (record.errors[0] as { message?: unknown } | undefined)
      : undefined
    const candidate = first?.message ?? record.message
    if (typeof candidate === 'string' && candidate.trim().length > 0) return candidate
  }
  return GENERIC_ERROR_MESSAGE
}

/**
 * The email/password sign-in screen, backed by the auth-adapter seam.
 *
 * It consumes `useMobileSignIn()` and never a Clerk hook. A successful `signIn.password(...)` is
 * completed through the adapter's `finalize`, which owns Clerk's navigate contract, the
 * `session.currentTask` diversion, and the finalize error envelope. In inert mode (`signIn` is
 * `null`) the real form markup renders disabled with an accessible notice.
 *
 * The Google/Apple buttons are rendered but their SSO behaviour is wired by todo 15.
 */
export default function SignInScreen() {
  const { isLoaded, signIn, finalize } = useMobileSignIn()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const unavailable = signIn === null
  const controlsDisabled = unavailable || !isLoaded

  async function handleSubmit() {
    // Defensive: the submit control is disabled whenever `signIn` is null or the adapter is not
    // loaded, but the guard keeps the handler total if it is ever invoked another way.
    if (!signIn || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const { error: passwordError } = await signIn.password({ password, identifier: email })
      if (passwordError) {
        setError(describeAuthError(passwordError))
        return
      }

      const { error: finalizeError } = await finalize({
        destination: '/dashboard',
        navigate: navigateToClerkDestination,
        onSessionTask: () => router.replace('/session-task'),
      })
      if (finalizeError) setError(describeAuthError(finalizeError))
    } catch (thrown) {
      setError(describeAuthError(thrown))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Screen testID="sign-in-screen" header={<ScreenHeader title="Sign in" />} scrollable>
      <Stack gap={4} className="p-bb-6">
        <Text variant="heading">Welcome back</Text>
        <Text tone="muted">Sign in to continue to BoostedBudget.</Text>

        {unavailable ? (
          <Alert testID="sign-in-unavailable" variant="info">
            sign-in is unavailable in this build
          </Alert>
        ) : null}

        {error ? (
          <Alert testID="sign-in-error" variant="danger" title="Sign-in failed">
            {error}
          </Alert>
        ) : null}

        <FormField
          label="Email"
          required
          disabled={controlsDisabled}
          testID="sign-in-email-field"
        >
          <TextField
            testID="sign-in-email"
            value={email}
            onChangeText={setEmail}
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="email-address"
            autoComplete="email"
            textContentType="emailAddress"
            disabled={controlsDisabled}
          />
        </FormField>

        <FormField
          label="Password"
          required
          disabled={controlsDisabled}
          testID="sign-in-password-field"
        >
          <TextField
            testID="sign-in-password"
            value={password}
            onChangeText={setPassword}
            secureTextEntry
            autoCapitalize="none"
            autoComplete="current-password"
            textContentType="password"
            disabled={controlsDisabled}
          />
        </FormField>

        <Button
          testID="sign-in-submit"
          label="Sign in"
          onPress={handleSubmit}
          loading={submitting}
          disabled={controlsDisabled}
          fullWidth
        />

        <Inline gap={4} justify="center" wrap>
          <Button
            testID="sign-in-sign-up-link"
            variant="link"
            label="Create an account"
            onPress={() => router.push('/sign-up')}
            disabled={controlsDisabled}
          />
          <Button
            testID="sign-in-forgot-password-link"
            variant="link"
            label="Forgot password?"
            onPress={() => router.push('/forgot-password')}
            disabled={controlsDisabled}
          />
        </Inline>

        <Text variant="caption" tone="muted">
          Or continue with
        </Text>
        <Stack gap={2}>
          <Button
            testID="sign-in-google"
            variant="outline"
            label="Continue with Google"
            disabled={controlsDisabled}
            fullWidth
          />
          <Button
            testID="sign-in-apple"
            variant="outline"
            label="Continue with Apple"
            disabled={controlsDisabled}
            fullWidth
          />
        </Stack>
      </Stack>
    </Screen>
  )
}
