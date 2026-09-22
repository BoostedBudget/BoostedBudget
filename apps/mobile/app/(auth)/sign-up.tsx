import { router } from 'expo-router'
import { useState } from 'react'
import { useMobileSignUp } from '../../src/auth/mobile-auth'
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

/** Shown when Clerk still needs a field the screen cannot collect (e.g. a phone number). */
const MISSING_FIELDS_MESSAGE = 'Your account is missing required information.'

/** Shown after a code is dispatched, so the user knows to check their inbox. */
const CODE_SENT_MESSAGE = 'We sent a verification code to your email.'

/** `credentials` collects email + password; `verify` collects the emailed code. */
type SignUpStep = 'credentials' | 'verify'

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
 * The email/password sign-up screen, backed by the auth-adapter seam.
 *
 * It consumes `useMobileSignUp()` and never a Clerk hook. It routes on the Core 3 `signUp` surface
 * pinned in `apps/mobile/docs/clerk-auth-api.md` (todo 5): after `password(...)` it reads `status`,
 * `unverifiedFields`, `missingFields` and `createdSessionId` to decide whether the sign-up is
 * already complete, still needs a field the screen cannot collect, or needs an emailed code. A
 * successful verification is completed through the adapter's `finalize`, which owns Clerk's
 * navigate contract, the `session.currentTask` diversion, and the finalize error envelope. In inert
 * mode (`signUp` is `null`) the real form markup renders disabled with an accessible notice.
 */
export default function SignUpScreen() {
  const { isLoaded, signUp, finalize } = useMobileSignUp()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [step, setStep] = useState<SignUpStep>('credentials')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const unavailable = signUp === null
  const controlsDisabled = unavailable || !isLoaded
  // The credential fields freeze once the flow moves to code entry so a user cannot edit the email
  // the code was issued for while preparing to verify it.
  const credentialsDisabled = controlsDisabled || step !== 'credentials'

  /**
   * The single completion path. `finalize` resolves `{ pendingTask, error }`: a pending task routes
   * the user to the placeholder screen through `onSessionTask`, an external decorated URL is opened
   * by the OS via `navigateToClerkDestination`, and a non-null `error` is surfaced, never swallowed.
   */
  async function finalizeSession() {
    const { error: finalizeError } = await finalize({
      destination: '/dashboard',
      navigate: navigateToClerkDestination,
      onSessionTask: () => router.replace('/session-task'),
    })
    if (finalizeError) setError(describeAuthError(finalizeError))
  }

  async function handleCreateAccount() {
    // Defensive: the submit control is disabled whenever `signUp` is null or the adapter is not
    // loaded, but the guard keeps the handler total if it is ever invoked another way.
    if (!signUp || submitting) return

    setSubmitting(true)
    setError(null)
    setNotice(null)
    try {
      const { error: passwordError } = await signUp.password({ password, emailAddress: email })
      if (passwordError) {
        setError(describeAuthError(passwordError))
        return
      }

      // `status`/`createdSessionId`: the sign-up completed without further verification.
      if (signUp.status === 'complete' && signUp.createdSessionId) {
        await finalizeSession()
        return
      }

      // `missingFields`: Clerk needs a field this screen does not collect. Surface it instead of
      // rendering a code step that could never satisfy the requirement.
      if (signUp.missingFields.length > 0) {
        setError(MISSING_FIELDS_MESSAGE)
        return
      }

      // `unverifiedFields`: at least one identification field needs a code. Send it and advance.
      if (signUp.unverifiedFields.length > 0) {
        const { error: sendError } = await signUp.verifications.sendEmailCode()
        if (sendError) {
          setError(describeAuthError(sendError))
          return
        }
        setStep('verify')
        setNotice(CODE_SENT_MESSAGE)
        return
      }

      setError(GENERIC_ERROR_MESSAGE)
    } catch (thrown) {
      setError(describeAuthError(thrown))
    } finally {
      setSubmitting(false)
    }
  }

  async function handleVerifyCode() {
    if (!signUp || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const { error: verifyError } = await signUp.verifications.verifyEmailCode({ code })
      if (verifyError) {
        setError(describeAuthError(verifyError))
        return
      }

      if (signUp.status !== 'complete' || !signUp.createdSessionId) {
        setError(MISSING_FIELDS_MESSAGE)
        return
      }

      await finalizeSession()
    } catch (thrown) {
      setError(describeAuthError(thrown))
    } finally {
      setSubmitting(false)
    }
  }

  async function handleResendCode() {
    if (!signUp || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const { error: sendError } = await signUp.verifications.sendEmailCode()
      if (sendError) {
        setError(describeAuthError(sendError))
        return
      }
      setNotice(CODE_SENT_MESSAGE)
    } catch (thrown) {
      setError(describeAuthError(thrown))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Screen testID="sign-up-screen" header={<ScreenHeader title="Create account" />} scrollable>
      <Stack gap={4} className="p-bb-6">
        <Text variant="heading">Get started</Text>
        <Text tone="muted">Create your BoostedBudget account.</Text>

        {unavailable ? (
          <Alert testID="sign-up-unavailable" variant="info">
            sign-up is unavailable in this build
          </Alert>
        ) : null}

        {notice ? (
          <Alert testID="sign-up-notice" variant="info">
            {notice}
          </Alert>
        ) : null}

        {error ? (
          <Alert testID="sign-up-error" variant="danger" title="Sign-up failed">
            {error}
          </Alert>
        ) : null}

        <FormField
          label="Email"
          required
          disabled={credentialsDisabled}
          testID="sign-up-email-field"
        >
          <TextField
            testID="sign-up-email"
            value={email}
            onChangeText={setEmail}
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="email-address"
            autoComplete="email"
            textContentType="emailAddress"
            disabled={credentialsDisabled}
          />
        </FormField>

        <FormField
          label="Password"
          required
          disabled={credentialsDisabled}
          testID="sign-up-password-field"
        >
          <TextField
            testID="sign-up-password"
            value={password}
            onChangeText={setPassword}
            secureTextEntry
            autoCapitalize="none"
            autoComplete="new-password"
            textContentType="newPassword"
            disabled={credentialsDisabled}
          />
        </FormField>

        {step === 'verify' ? (
          <>
            <FormField
              label="Verification code"
              required
              disabled={controlsDisabled}
              testID="sign-up-code-field"
            >
              <TextField
                testID="sign-up-code"
                value={code}
                onChangeText={setCode}
                keyboardType="number-pad"
                autoCapitalize="none"
                autoComplete="one-time-code"
                textContentType="oneTimeCode"
                disabled={controlsDisabled}
              />
            </FormField>

            <Button
              testID="sign-up-verify"
              label="Verify"
              onPress={handleVerifyCode}
              loading={submitting}
              disabled={controlsDisabled}
              fullWidth
            />
            <Button
              testID="sign-up-resend"
              variant="link"
              label="Resend code"
              onPress={handleResendCode}
              disabled={controlsDisabled || submitting}
            />
          </>
        ) : (
          <Button
            testID="sign-up-submit"
            label="Create account"
            onPress={handleCreateAccount}
            loading={submitting}
            disabled={controlsDisabled}
            fullWidth
          />
        )}

        <Inline gap={4} justify="center" wrap>
          <Button
            testID="sign-up-sign-in-link"
            variant="link"
            label="Sign in"
            onPress={() => router.push('/sign-in')}
            disabled={controlsDisabled}
          />
        </Inline>
      </Stack>
    </Screen>
  )
}
