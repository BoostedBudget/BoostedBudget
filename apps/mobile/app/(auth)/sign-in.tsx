import { router } from 'expo-router'
import { useState } from 'react'
import { useMobileAuth, useMobileSignIn, useMobileSSO } from '../../src/auth/mobile-auth'
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
 * The deep-link target the OAuth provider returns to. It mirrors `expo.scheme` in `app.json`
 * (`boostedbudget`) and the `app/(auth)/sso-callback.tsx` route. Clerk requires this exact URL to
 * be registered as an allowed redirect in the Clerk Dashboard — see `apps/mobile/README.md`.
 */
const SSO_REDIRECT_URL = 'boostedbudget://sso-callback'

type SSOStrategy = 'oauth_google' | 'oauth_apple'

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
 * It consumes `useMobileSignIn()`, `useMobileAuth()` and `useMobileSSO()` and never a Clerk hook.
 * A successful `signIn.password(...)` is completed through the adapter's `finalize`, which owns
 * Clerk's navigate contract, the `session.currentTask` diversion, and the finalize error envelope.
 * The Google/Apple buttons run the OAuth flow through `useMobileSSO()` and complete it through
 * `activateSession` when the hook did not self-activate. In inert mode (`signIn` is `null`) the
 * real form markup renders disabled with an accessible notice.
 */
export default function SignInScreen() {
  const { isLoaded, signIn, finalize } = useMobileSignIn()
  const { activateSession } = useMobileAuth()
  const { startSSOFlow } = useMobileSSO()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [ssoStrategy, setSsoStrategy] = useState<SSOStrategy | null>(null)
  const [error, setError] = useState<string | null>(null)

  const unavailable = signIn === null
  const controlsDisabled = unavailable || !isLoaded

  async function handleSSO(strategy: SSOStrategy) {
    if (submitting) return

    setSubmitting(true)
    setSsoStrategy(strategy)
    setError(null)
    try {
      const { createdSessionId, sessionActivated, cancelled, pendingTask } = await startSSOFlow({
        strategy,
        redirectUrl: SSO_REDIRECT_URL,
      })

      // The user dismissed the provider sheet. There is nothing to activate and nothing to
      // report: leave them on the form to collect any missing information.
      if (cancelled) return

      if (pendingTask) {
        navigateToClerkDestination('/session-task')
        return
      }

      if (!sessionActivated && createdSessionId !== null) {
        // The pinned experimental hook self-activates when it can. A non-activated result with a
        // session id means the caller must activate it; a null id means the hook already
        // activated an existing session, so there is nothing to activate.
        const { pendingTask: activationTask } = await activateSession(createdSessionId)
        if (activationTask) {
          navigateToClerkDestination('/session-task')
          return
        }
      }

      navigateToClerkDestination('/dashboard')
    } catch (thrown) {
      setError(describeAuthError(thrown))
    } finally {
      setSubmitting(false)
      setSsoStrategy(null)
    }
  }

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
            onPress={() => handleSSO('oauth_google')}
            loading={ssoStrategy === 'oauth_google'}
            disabled={controlsDisabled || submitting}
            fullWidth
          />
          <Button
            testID="sign-in-apple"
            variant="outline"
            label="Continue with Apple"
            onPress={() => handleSSO('oauth_apple')}
            loading={ssoStrategy === 'oauth_apple'}
            disabled={controlsDisabled || submitting}
            fullWidth
          />
        </Stack>
      </Stack>
    </Screen>
  )
}
