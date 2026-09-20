import { ScrollView, View } from 'react-native'
import { BottomSheet } from './BottomSheet'
import { Button } from './Button'
import { Text } from './Text'

export interface ActionSheetAction {
  /** Visible label; also the accessible name. Required, so a destructive action is never colour-only. */
  label: string
  onPress: () => void
  /** Renders the action with the danger token. The text label remains the non-colour cue. */
  destructive?: boolean
  disabled?: boolean
}

export interface ActionSheetProps {
  visible: boolean
  title?: string
  actions: ActionSheetAction[]
  /** Fired by the cancel action, the Android hardware back button, and web Escape. */
  onCancel: () => void
  cancelLabel?: string
  testID?: string
}

/**
 * A bottom-anchored action sheet built on `BottomSheet`.
 *
 * Each action is a `Button`, so it inherits the touch-target floor, the accessible name and the
 * focus ring. The cancel action is rendered LAST and OUTSIDE the scrollable action list, so it
 * stays reachable when the action list overflows and is always the final tab stop.
 *
 * A destructive action always carries a visible text label (`label` is required by the type) as
 * well as the danger token, so the danger is never conveyed by colour alone.
 */
export function ActionSheet({
  visible,
  title,
  actions,
  onCancel,
  cancelLabel = 'Cancel',
  testID = 'action-sheet',
}: ActionSheetProps) {
  return (
    <BottomSheet visible={visible} onClose={onCancel} testID={testID}>
      {title ? (
        <Text testID={`${testID}-title`} variant="heading">
          {title}
        </Text>
      ) : null}
      <ScrollView testID={`${testID}-actions`} style={{ flexShrink: 1 }}>
        <View className="gap-bb-2">
          {actions.map((action, index) => (
            <Button
              key={`${action.label}-${index}`}
              testID={`${testID}-action-${index}`}
              variant={action.destructive ? 'destructive' : 'outline'}
              label={action.label}
              disabled={action.disabled}
              fullWidth
              onPress={action.onPress}
            />
          ))}
        </View>
      </ScrollView>
      <View testID={`${testID}-cancel-region`}>
        <Button
          testID={`${testID}-cancel`}
          variant="outline"
          label={cancelLabel}
          fullWidth
          onPress={onCancel}
        />
      </View>
    </BottomSheet>
  )
}