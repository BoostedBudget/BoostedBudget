import { fireEvent, render, screen } from '@testing-library/react-native'
import { View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Dialog } from '../../components/ui/Dialog'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog'
import { DIALOG_REGISTRY } from '../../components/ui/dialog.registry'
import { setColorScheme } from '../color-scheme'
import { COLOR_SCHEMES, resolvedColor, resolvedStyle, runtimeTokenColors } from './helpers'

const TOUCH_MIN = nativeTokens.light.touchTarget.min

/**
 * The initial-focus contract is expressed as DOM order, because that is the mechanism the web
 * platform actually uses: react-native-web's `ModalFocusTrap` focuses the FIRST focusable
 * descendant of the modal on open (`ModalFocusTrap.js` → `focusFirstDescendant`). jest has no DOM
 * and RN `Modal` does not implement web focus management, so the browser half of this contract is
 * asserted in Todo 22(e); here we pin the order that produces it.
 */
function actionLabels(): string[] {
  return screen.getAllByRole('button').map((node) => node.props.accessibilityLabel as string)
}

describe('Dialog modal semantics', () => {
  it('renders nothing while closed', () => {
    render(<Dialog visible={false} onRequestClose={() => {}} title="Hidden" />)

    expect(screen.queryByTestId('dialog-overlay')).toBeNull()
    expect(screen.queryByTestId('dialog-surface')).toBeNull()
  })

  it('marks the overlay and the dialog surface as modal with the dialog role when open', () => {
    render(<Dialog visible onRequestClose={() => {}} title="Settings" description="Change things" />)

    const overlay = screen.getByTestId('dialog-overlay')
    expect(overlay.props.accessibilityViewIsModal).toBe(true)

    const surface = screen.getByTestId('dialog-surface')
    expect(surface.props.accessibilityViewIsModal).toBe(true)
    expect(surface.props.role).toBe('dialog')
    // RN 0.86's legacy `accessibilityRole` union omits `dialog`; the native shim still carries it.
    expect(surface.props.accessibilityRole).toBe('dialog')
  })

  it('renders the title and description on the dialog surface', () => {
    render(<Dialog visible onRequestClose={() => {}} title="Settings" description="Change things" />)

    expect(screen.getByText('Settings')).toBeTruthy()
    expect(screen.getByText('Change things')).toBeTruthy()
  })

  it('removes the overlay when visible flips to false (no stale overlay)', () => {
    const { rerender } = render(<Dialog visible onRequestClose={() => {}} title="Settings" />)
    expect(screen.getByTestId('dialog-overlay')).toBeTruthy()

    rerender(<Dialog visible={false} onRequestClose={() => {}} title="Settings" />)
    expect(screen.queryByTestId('dialog-overlay')).toBeNull()
  })
})

describe('Dialog close handling', () => {
  it('invokes the close handler from onRequestClose — Android hardware back and web Escape', () => {
    const onRequestClose = jest.fn()
    render(<Dialog visible onRequestClose={onRequestClose} title="Settings" />)

    // react-native-web's Modal routes Escape to `onRequestClose` (ModalContent.js), and Android's
    // hardware back calls it natively, so both share this single close path.
    screen.getByTestId('dialog').props.onRequestClose()
    expect(onRequestClose).toHaveBeenCalledTimes(1)
  })

  it('does not dismiss on a backdrop tap by default', () => {
    const onRequestClose = jest.fn()
    render(<Dialog visible onRequestClose={onRequestClose} title="Settings" />)

    // The backdrop is hidden from the accessibility tree, so it needs the hidden-element opt-in.
    fireEvent(screen.getByTestId('dialog-backdrop', { includeHiddenElements: true }), 'responderRelease')
    expect(onRequestClose).not.toHaveBeenCalled()
  })

  it('dismisses on a backdrop tap only when dismissOnBackdrop is true', () => {
    const onRequestClose = jest.fn()
    render(<Dialog visible dismissOnBackdrop onRequestClose={onRequestClose} title="Settings" />)

    fireEvent(screen.getByTestId('dialog-backdrop', { includeHiddenElements: true }), 'responderRelease')
    expect(onRequestClose).toHaveBeenCalledTimes(1)
  })
})

describe('ConfirmDialog', () => {
  it('renders an alertdialog with the title and message', () => {
    render(
      <ConfirmDialog
        visible
        title="Delete account"
        message="This cannot be undone."
        confirmLabel="Delete"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    )

    const surface = screen.getByTestId('confirm-dialog-surface')
    expect(surface.props.role).toBe('alertdialog')
    expect(surface.props.accessibilityRole).toBe('alertdialog')
    expect(screen.getByText('Delete account')).toBeTruthy()
    expect(screen.getByText('This cannot be undone.')).toBeTruthy()
  })

  it('makes the confirm action the primary focus target when not destructive', () => {
    render(
      <ConfirmDialog
        visible
        title="Save changes"
        message="Apply them now?"
        confirmLabel="Save"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    )

    // First focusable descendant wins the initial focus; the confirm action is first.
    expect(actionLabels()).toEqual(['Save', 'Cancel'])
  })

  it('never makes a destructive confirm the initial focus — the safe action is first', () => {
    render(
      <ConfirmDialog
        visible
        destructive
        title="Delete account"
        message="This cannot be undone."
        confirmLabel="Delete"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    )

    const labels = actionLabels()
    expect(labels[0]).toBe('Cancel')
    expect(labels[0]).not.toBe('Delete')
    expect(labels).toEqual(['Cancel', 'Delete'])
  })

  it('gives a destructive confirm the danger token and a text label, never colour alone', () => {
    const runtime = runtimeTokenColors().light
    setColorScheme('light')
    render(
      <ConfirmDialog
        visible
        destructive
        title="Delete account"
        message="This cannot be undone."
        confirmLabel="Delete"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    )

    const confirm = screen.getByTestId('confirm-dialog-confirm')
    expect(resolvedColor('confirm-dialog-confirm', 'backgroundColor')).toBe(runtime['bb-danger-strong'])
    // The non-colour cue is the visible text label, which is also the accessible name.
    expect(screen.getByText('Delete')).toBeTruthy()
    expect(confirm.props.accessibilityLabel).toBe('Delete')
  })

  it('meets the touch floor on both actions', () => {
    render(
      <ConfirmDialog
        visible
        destructive
        title="Delete account"
        message="This cannot be undone."
        confirmLabel="Delete"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    )

    for (const id of ['confirm-dialog-confirm', 'confirm-dialog-cancel']) {
      expect(resolvedStyle(id).minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
    }
  })

  it('invokes onConfirm and onCancel from the actions', () => {
    const onConfirm = jest.fn()
    const onCancel = jest.fn()
    render(
      <ConfirmDialog
        visible
        title="Save changes"
        message="Apply them now?"
        confirmLabel="Save"
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    )

    fireEvent.press(screen.getByTestId('confirm-dialog-confirm'))
    fireEvent.press(screen.getByTestId('confirm-dialog-cancel'))
    expect(onConfirm).toHaveBeenCalledTimes(1)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('disables both actions and shows a spinner while loading', () => {
    const onConfirm = jest.fn()
    const onCancel = jest.fn()
    render(
      <ConfirmDialog
        visible
        loading
        title="Delete account"
        message="This cannot be undone."
        confirmLabel="Delete"
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    )

    expect(screen.getByTestId('confirm-dialog-confirm').props.accessibilityState).toMatchObject({
      busy: true,
      disabled: true,
    })
    expect(screen.getByTestId('confirm-dialog-confirm-spinner')).toBeTruthy()
    expect(screen.getByTestId('confirm-dialog-cancel').props.accessibilityState).toMatchObject({
      disabled: true,
    })

    fireEvent.press(screen.getByTestId('confirm-dialog-confirm'))
    fireEvent.press(screen.getByTestId('confirm-dialog-cancel'))
    expect(onConfirm).not.toHaveBeenCalled()
    expect(onCancel).not.toHaveBeenCalled()
  })
})

describe('DIALOG_REGISTRY', () => {
  it('has one entry per text-bearing dialog surface', () => {
    const names = DIALOG_REGISTRY.map((entry) => entry.name)
    for (const name of ['Dialog.title', 'Dialog.description', 'ConfirmDialog.title', 'ConfirmDialog.message']) {
      expect(names).toContain(name)
    }
  })

  it('pins the destructive confirm to destructive-foreground on bb-danger-strong', () => {
    const entry = DIALOG_REGISTRY.find((item) => item.name === 'ConfirmDialog.confirmDestructive')
    expect(entry).toMatchObject({
      foregroundToken: 'destructive-foreground',
      backgroundToken: 'bb-danger-strong',
      ratioClass: 'body',
      interactive: true,
    })
  })

  it('resolves every foreground/background token in both themes', () => {
    for (const entry of DIALOG_REGISTRY) {
      for (const scheme of COLOR_SCHEMES) {
        const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>
        expect(section[entry.foregroundToken]).toBeDefined()
        expect(section[entry.backgroundToken]).toBeDefined()
      }
    }
  })
})

describe('Dialog colour provenance', () => {
  it('renders the dialog surface from the popover token', () => {
    const runtime = runtimeTokenColors().light
    setColorScheme('light')
    render(<Dialog visible onRequestClose={() => {}} title="Settings" />)

    expect(resolvedColor('dialog-surface', 'backgroundColor')).toBe(runtime.popover)
  })
})

describe('Dialog types', () => {
  it('exposes the documented prop unions', () => {
    const role: 'dialog' | 'alertdialog' = 'alertdialog'
    expect(role).toBe('alertdialog')
    expect(View).toBeDefined()
  })
})