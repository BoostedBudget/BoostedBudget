import { router } from 'expo-router'
import { Button, Screen, ScreenHeader, Stack, Text } from '../../src/components/ui'

/**
 * A minimal placeholder for a pending Clerk session task.
 *
 * Clerk's `finalize` surfaces `session.currentTask` through its navigate callback. Rather than
 * silently dropping the user on `/dashboard`, the adapter routes here. The task-specific UI (MFA
 * enrolment, organization selection, password reset) is out of scope for this slice and is
 * recorded as such.
 */
export default function SessionTaskScreen() {
  return (
    <Screen
      testID="session-task-screen"
      header={<ScreenHeader title="One more step" />}
      scrollable
    >
      <Stack gap={4} className="p-bb-6">
        <Text variant="heading">Additional verification required</Text>
        <Text tone="muted">
          Additional verification or selection is required before you can continue. Complete the
          step Clerk asked for, then continue.
        </Text>
        <Button
          testID="session-task-continue"
          label="Continue"
          onPress={() => router.replace('/dashboard')}
          fullWidth
        />
      </Stack>
    </Screen>
  )
}
