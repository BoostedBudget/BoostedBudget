import { Pressable, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Text } from './Text'
import { useTheme } from '../../theme/ThemeProvider'
import type { ThemePreference } from '../../theme/preference'

export interface ThemeOption {
  value: ThemePreference
  label: string
}

export const THEME_OPTIONS: ThemeOption[] = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'system', label: 'System' },
]

export interface ThemeSelectorProps {
  testID?: string
}

/**
 * Segmented control for the device-local theme preference. Selection is conveyed by a filled
 * surface (a shape change) plus `accessibilityState.selected`, never by colour alone. The touch
 * floor is an explicit `minHeight` from the token adapter, so it survives a zero-content option.
 */
export function ThemeSelector({ testID = 'theme-selector' }: ThemeSelectorProps) {
  const { preference, setPreference } = useTheme()

  return (
    <View
      testID={testID}
      accessibilityRole="radiogroup"
      accessibilityLabel="Theme selection"
      className="flex-row gap-bb-1 rounded-lg border border-border bg-card p-bb-1"
    >
      {THEME_OPTIONS.map(({ value, label }) => {
        const selected = preference === value

        return (
          <Pressable
            key={value}
            testID={`${testID}-${value}`}
            accessibilityRole="radio"
            accessibilityLabel={label}
            accessibilityState={{ selected }}
            onPress={() => setPreference(value)}
            style={{ minHeight: nativeTokens.light.touchTarget.min }}
            className={`flex-1 items-center justify-center rounded-lg px-bb-3 py-bb-2 ${
              selected ? 'bg-primary' : ''
            }`}
          >
            <Text
              testID={`${testID}-${value}-label`}
              variant="label"
              tone={selected ? 'default' : 'muted'}
              className={selected ? 'text-primary-foreground' : undefined}
            >
              {label}
            </Text>
          </Pressable>
        )
      })}
    </View>
  )
}