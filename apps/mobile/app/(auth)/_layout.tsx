import { Stack } from 'expo-router'

/**
 * The public auth group.
 *
 * It is deliberately a plain `Stack` — never the `.Protected` variant (open iOS defect
 * expo/expo#37305). Signed-out access to these routes is intended — the `(app)` group is what
 * guards the authenticated surface via the adapter and `<Redirect>`.
 */
export default function AuthLayout() {
  return <Stack screenOptions={{ headerBackTitle: 'Back' }} />
}
