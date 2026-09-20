import { afterEach } from '@jest/globals'
import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { StyleSheet, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { ThemeProvider } from '../../theme/ThemeProvider'
import { ActionSheet } from '../../components/ui/ActionSheet'
import { BackButton } from '../../components/ui/BackButton'
import { BottomSheet } from '../../components/ui/BottomSheet'
import { Button } from '../../components/ui/Button'
import { Card } from '../../components/ui/Card'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog'
import { Dialog } from '../../components/ui/Dialog'
import { ErrorState } from '../../components/ui/ErrorState'
import { IconButton } from '../../components/ui/IconButton'
import { ListItem } from '../../components/ui/ListItem'
import { TabBar } from '../../components/ui/TabBar'

afterEach(() => {
  jest.restoreAllMocks()
})

function outlineWidth(testID: string): unknown {
  const style = StyleSheet.flatten(screen.getByTestId(testID).props.style) as Record<string, unknown>
  return style.outlineWidth
}

/**
 * Criterion 3 (#1656): keyboard.
 *
 * Web Escape and Android hardware back are the SAME close path: react-native-web's `Modal` routes
 * Escape to `onRequestClose` and Android's back calls it natively. So invoking the Modal host's
 * `onRequestClose` is the jest-observable half of both. The focus ring is React focus state (not a
 * `focus-visible:` utility, which does not resolve under jest), so `fireEvent(node, 'focus')`
 * drives it.
 */
describe('accessibility: keyboard (criterion 3)', () => {
  const overlays = [
    {
      name: 'Dialog',
      testID: 'kb-dialog',
      render: (onClose: () => void) => (
        <Dialog visible onRequestClose={onClose} title="Title" testID="kb-dialog" />
      ),
    },
    {
      name: 'ConfirmDialog',
      testID: 'kb-confirm',
      render: (onClose: () => void) => (
        <ConfirmDialog
          visible
          title="Title"
          message="Message"
          confirmLabel="Save"
          onConfirm={() => {}}
          onCancel={onClose}
          testID="kb-confirm"
        />
      ),
    },
    {
      name: 'BottomSheet',
      testID: 'kb-sheet',
      render: (onClose: () => void) => (
        <BottomSheet visible onClose={onClose} testID="kb-sheet" />
      ),
    },
    {
      name: 'ActionSheet',
      testID: 'kb-action',
      render: (onClose: () => void) => (
        <ActionSheet
          visible
          actions={[{ label: 'A', onPress: () => {} }]}
          onCancel={onClose}
          testID="kb-action"
        />
      ),
    },
  ]

  for (const overlay of overlays) {
    it(`${overlay.name} closes through onRequestClose — Escape on web, back on Android`, () => {
      const onClose = jest.fn()
      render(overlay.render(onClose))

      screen.getByTestId(overlay.testID).props.onRequestClose()
      expect(onClose).toHaveBeenCalledTimes(1)
    })
  }

  const focusRingCases: ReadonlyArray<{ name: string; testID: string; element: React.ReactElement }> = [
    { name: 'Button', testID: 'fr-button', element: <Button testID="fr-button" label="Go" /> },
    { name: 'IconButton', testID: 'fr-icon', element: <IconButton testID="fr-icon" label="Go" /> },
    { name: 'BackButton', testID: 'fr-back', element: <BackButton testID="fr-back" onPress={() => {}} /> },
    { name: 'Card', testID: 'fr-card', element: <Card testID="fr-card" onPress={() => {}} /> },
    {
      name: 'ListItem',
      testID: 'fr-list-item',
      element: <ListItem testID="fr-list-item" title="Row" onPress={() => {}} />,
    },
    {
      name: 'TabBar item',
      testID: 'fr-tab-item-a',
      element: <TabBar testID="fr-tab" items={[{ key: 'a', label: 'A' }]} />,
    },
    {
      name: 'ErrorState detail toggle',
      testID: 'fr-error-detail-toggle',
      element: (
        <ErrorState
          testID="fr-error"
          title="Oops"
          description="Try again"
          onRetry={() => {}}
          detail="Detail"
        />
      ),
    },
  ]

  it('shows the focusTokens ring on focus for every web-targeted affordance', async () => {
    const expectedWidth = nativeTokens.light.focus['bb-focus-ring-width']
    render(
      <ThemeProvider>
        <View>
          {focusRingCases.map((item) => (
            <View key={item.name}>{item.element}</View>
          ))}
        </View>
      </ThemeProvider>,
    )
    await act(async () => {})

    for (const item of focusRingCases) {
      expect({ case: item.name, width: outlineWidth(item.testID) }).toEqual({
        case: item.name,
        width: undefined,
      })

      fireEvent(screen.getByTestId(item.testID), 'focus')
      expect({ case: item.name, width: outlineWidth(item.testID) }).toEqual({
        case: item.name,
        width: expectedWidth,
      })
      expect(
        (StyleSheet.flatten(screen.getByTestId(item.testID).props.style) as Record<string, unknown>)
          .outlineColor,
      ).toBeDefined()

      fireEvent(screen.getByTestId(item.testID), 'blur')
      expect({ case: item.name, width: outlineWidth(item.testID) }).toEqual({
        case: item.name,
        width: undefined,
      })
    }
  })
})
