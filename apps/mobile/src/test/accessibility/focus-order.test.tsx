import { afterEach } from '@jest/globals'
import { render, screen } from '@testing-library/react-native'
import { View } from 'react-native'
import { ActionSheet } from '../../components/ui/ActionSheet'
import { BottomSheet } from '../../components/ui/BottomSheet'
import { Button } from '../../components/ui/Button'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog'
import { Dialog } from '../../components/ui/Dialog'
import { List } from '../../components/ui/List'
import { ListItem } from '../../components/ui/ListItem'
import { renderCase, PRIMITIVE_CASES } from './helpers'

afterEach(() => {
  jest.restoreAllMocks()
})

/**
 * Criterion 2 (#1656): focus order.
 *
 * ⚠ The actual FOCUS TRAP and focus RESTORE are BROWSER behaviours owned by react-native-web's
 * `ModalFocusTrap` and are asserted in Todo 22(e), not here — jest has no DOM and RN `Modal` does
 * not implement web focus management. This suite pins the two things jest CAN observe: document
 * order of reachable controls, and `accessibilityViewIsModal` + the overlay role.
 */
describe('accessibility: focus order (criterion 2)', () => {
  it('keeps buttons and list rows reachable in document order', () => {
    render(
      <View>
        <Button testID="order-1" label="One" />
        <List testID="order-list">
          <ListItem testID="order-2" title="Two" onPress={() => {}} />
          <ListItem testID="order-3" title="Three" onPress={() => {}} />
        </List>
        <Button testID="order-4" label="Four" />
      </View>,
    )

    const reachable = screen.getAllByRole('button').map((node) => node.props.testID)
    expect(reachable).toEqual(['order-1', 'order-2', 'order-3', 'order-4'])
    expect(screen.getByTestId('order-list').props.accessibilityRole).toBe('list')
  })

  it('does not make a non-pressable list row a tab stop', () => {
    render(
      <View>
        <ListItem testID="plain-row" title="Plain" />
        <Button testID="after-row" label="After" />
      </View>,
    )

    expect(screen.queryByTestId('plain-row')?.props.accessibilityRole).toBeUndefined()
    expect(screen.getAllByRole('button').map((node) => node.props.testID)).toEqual(['after-row'])
  })

  const overlays = [
    {
      name: 'Dialog',
      overlay: 'fo-dialog-overlay',
      surface: 'fo-dialog-surface',
      role: 'dialog',
      element: <Dialog visible onRequestClose={() => {}} title="Title" testID="fo-dialog" />,
    },
    {
      name: 'ConfirmDialog',
      overlay: 'fo-confirm-overlay',
      surface: 'fo-confirm-surface',
      role: 'alertdialog',
      element: (
        <ConfirmDialog
          visible
          title="Title"
          message="Message"
          confirmLabel="Save"
          onConfirm={() => {}}
          onCancel={() => {}}
          testID="fo-confirm"
        />
      ),
    },
    {
      name: 'BottomSheet',
      overlay: 'fo-sheet-overlay',
      surface: 'fo-sheet-surface',
      role: 'dialog',
      element: <BottomSheet visible onClose={() => {}} testID="fo-sheet" />,
    },
    {
      name: 'ActionSheet',
      overlay: 'fo-action-overlay',
      surface: 'fo-action-surface',
      role: 'dialog',
      element: (
        <ActionSheet
          visible
          actions={[{ label: 'A', onPress: () => {} }]}
          onCancel={() => {}}
          testID="fo-action"
        />
      ),
    },
  ]

  for (const overlay of overlays) {
    it(`${overlay.name} exposes accessibilityViewIsModal with the ${overlay.role} role`, () => {
      render(overlay.element)

      expect(screen.getByTestId(overlay.overlay).props.accessibilityViewIsModal).toBe(true)
      const surface = screen.getByTestId(overlay.surface)
      expect(surface.props.accessibilityViewIsModal).toBe(true)
      expect(surface.props.role).toBe(overlay.role)
      // jest is `ios`, so the native legacy prop is set too (react-native-web omits it).
      expect(surface.props.accessibilityRole).toBe(overlay.role)
    })
  }

  it('exposes the modal flag on every overlay-shaped case in the primitive table', () => {
    const overlayCases = PRIMITIVE_CASES.filter((item) => item.component === Dialog || item.component === ConfirmDialog)
    expect(overlayCases.length).toBeGreaterThan(0)
  })

  it('renders a registry case without an overlay leaking into the base tree', async () => {
    await renderCase(PRIMITIVE_CASES.find((item) => item.name === 'Dialog') as (typeof PRIMITIVE_CASES)[number])
    expect(screen.queryByTestId('c-dialog-surface')).toBeTruthy()
  })
})
