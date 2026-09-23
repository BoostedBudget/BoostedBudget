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

/** The three steps of the email-code password reset. */
type ResetStep = 'request' | 'verify' | 'reset'

/**
 * The forgot-password screen, backed by the auth-adapter seam.
 *
 * It consumes `useMobileSignIn()` and never a Clerk hook. The flow walks three steps:
 * request a reset code by email, verify the code, then set the new password. The request step
 * first sets the collected address as the sign-in identifier (`signIn.create`), because
 * `resetPasswordEmailCode.sendCode()` takes no argument and sends to the identifier already on the
 * resource; the remaining steps are driven by the pinned `signIn.resetPasswordEmailCode` surface
 * (`sendCode`, `verifyCode`, `submitPassword`); the final step completes through the adapter's
 * `finalize`, which owns Clerk's navigate contract, the `session.currentTask` diversion, and the
 * finalize error envelope.
 *
 * In inert mode (`signIn` is `null`) the real form markup renders disabled with an accessible
 * notice, mirroring the sign-in screen.
 */
export default function ForgotPasswordScreen() {
  const { isLoaded, signIn, finalize } = useMobileSignIn()
  const [step, setStep] = useState<ResetStep>('request')
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const unavailable = signIn === null
  const controlsDisabled = unavailable || !isLoaded

  async function handleRequestCode() {
    // Defensive: the submit control is disabled whenever `signIn` is null or the adapter is not
    // loaded, but the guard keeps the handler total if it is ever invoked another way.
    if (!signIn || submitting) return

    if (email.trim().length === 0) {
      setError('Enter the email address for your account.')
      return
    }

    setSubmitting(true)
    setError(null)
    try {
      const identifier = email.trim()
      const { error: createError } = await signIn.create({ identifier })
      if (createError) {
        setError(describeAuthError(createError))
        return
      }
      const { error: sendError } = await signIn.resetPasswordEmailCode.sendCode()
      if (sendError) {
        setError(describeAuthError(sendError))
        return
      }
      setStep('verify')
    } catch (thrown) {
      setError(describeAuthError(thrown))
    } finally {
      setSubmitting(false)
    }
  }

  async function handleVerifyCode() {
    if (!signIn || submitting) return

    const trimmedCode = code.trim()
    if (trimmedCode.length === 0) {
      setError('Enter the verification code we emailed you.')
      return
    }

    setSubmitting(true)
    setError(null)
    try {
      const { error: verifyError } = await signIn.resetPasswordEmailCode.verifyCode({
        code: trimmedCode,
      })
      if (verifyError) {
        setError(describeAuthError(verifyError))
        return
      }
      setStep('reset')
    } catch (thrown) {
      setError(describeAuthError(thrown))
    } finally {
      setSubmitting(false)
    }
  }

  async function handleSetPassword() {
    if (!signIn || submitting) return

    if (password.trim().length === 0) {
      setError('Enter your new password.')
      return
    }

    setSubmitting(true)
    setError(null)
    try {
      const { error: submitError } = await signIn.resetPasswordEmailCode.submitPassword({
        password,
      })
      if (submitError) {
        setError(describeAuthError(submitError))
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
    <Screen
      testID="forgot-password-screen"
      header={<ScreenHeader title="Reset password" />}
      scrollable
    >
      <Stack gap={4} className="p-bb-6">
        {unavailable ? (
          <Alert testID="forgot-password-unavailable" variant="info">
            password reset is unavailable in this build
          </Alert>
        ) : null}

        {error ? (
          <Alert testID="forgot-password-error" variant="danger" title="Could not reset password">
            {error}
          </Alert>
        ) : null}

        {step === 'request' ? (
          <>
            <Text variant="heading">Reset your password</Text>
            <Text tone="muted">
              Enter the email on your account and we will send you a code to verify it is you.
            </Text>

            <FormField
              label="Email"
              required
              disabled={controlsDisabled}
              testID="forgot-password-email-field"
            >
              <TextField
                testID="forgot-password-email"
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

            <Button
              testID="forgot-password-send-code"
              label="Send reset code"
              onPress={handleRequestCode}
              loading={submitting}
              disabled={controlsDisabled}
              fullWidth
            />
          </>
        ) : null}

        {step === 'verify' ? (
          <>
            <Text variant="heading">Enter your code</Text>
            <Text tone="muted">
              Enter the verification code we sent to {email.trim() || 'your email address'}.
            </Text>

            <FormField
              label="Verification code"
              required
              disabled={controlsDisabled}
              testID="forgot-password-code-field"
            >
              <TextField
                testID="forgot-password-code"
                value={code}
                onChangeText={setCode}
                keyboardType="number-pad"
                autoComplete="one-time-code"
                textContentType="oneTimeCode"
                disabled={controlsDisabled}
              />
            </FormField>

            <Button
              testID="forgot-password-verify-code"
              label="Verify code"
              onPress={handleVerifyCode}
              loading={submitting}
              disabled={controlsDisabled}
              fullWidth
            />
          </>
        ) : null}

        {step === 'reset' ? (
          <>
            <Text variant="heading">Choose a new password</Text>
            <Text tone="muted">Set a new password for your account, then continue.</Text>

            <FormField
              label="New password"
              required
              disabled={controlsDisabled}
              testID="forgot-password-password-field"
            >
              <TextField
                testID="forgot-password-password"
                value={password}
                onChangeText={setPassword}
                secureTextEntry
                autoCapitalize="none"
                autoComplete="new-password"
                textContentType="newPassword"
                disabled={controlsDisabled}
              />
            </FormField>

            <Button
              testID="forgot-password-submit"
              label="Set new password"
              onPress={handleSetPassword}
              loading={submitting}
              disabled={controlsDisabled}
              fullWidth
            />
          </>
        ) : null}

        <Inline gap={4} justify="center" wrap>
          <Button
            testID="forgot-password-back"
            variant="link"
            label="Back to sign in"
            onPress={() => router.replace('/sign-in')}
            disabled={submitting}
          />
        </Inline>
      </Stack>
    </Screen>
  )
}