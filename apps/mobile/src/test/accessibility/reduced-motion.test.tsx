import { afterEach, beforeEach } from '@jest/globals'
import { useEffect } from 'react'
import { act, render, screen } from '@testing-library/react-native'
import { AccessibilityInfo, Animated } from 'react-native'
import { BottomSheet } from '../../components/ui/BottomSheet'
import { Skeleton } from '../../components/ui/Skeleton'
import { Spinner, SPINNER_NORMAL_DURATION_MS, SPINNER_REDUCED_DURATION_MS } from '../../components/ui/Spinner'
import { ToastProvider, useToast } from '../../components/ui/ToastProvider'

function fakeAnimation() {
  return { start: jest.fn(), stop: jest.fn(), reset: jest.fn() } as never
}

function spyAnimations() {
  const timing = jest.spyOn(Animated, 'timing').mockImplementation(() => fakeAnimation())
  jest.spyOn(Animated, 'loop').mockImplementation(() => fakeAnimation())
  jest.spyOn(Animated, 'sequence').mockImplementation(() => fakeAnimation())
  return timing
}

function mockReducedMotion(enabled: boolean) {
  let resolveMotion: (value: boolean) => void = () => {}
  const pending = new Promise<boolean>((resolve) => {
    resolveMotion = resolve
  })
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockReturnValue(pending)
  jest.spyOn(AccessibilityInfo, 'addEventListener').mockReturnValue({ remove: jest.fn() } as never)
  return async () => {
    await act(async () => {
      resolveMotion(enabled)
    })
  }
}

function ToastHarness() {
  const { show } = useToast()
  useEffect(() => {
    show({ message: 'Saved' })
  }, [show])
  return null
}

beforeEach(() => {
  jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(false)
  jest.spyOn(AccessibilityInfo, 'addEventListener').mockReturnValue({ remove: jest.fn() } as never)
})

afterEach(() => {
  jest.restoreAllMocks()
})

/**
 * Criterion 5 (#1656): reduced motion. Every animated primitive reads
 * `AccessibilityInfo.isReduceMotionEnabled` and takes its reduced branch. `Spinner`/`Skeleton`
 * SLOW (a frozen spinner reads as a hung page); `BottomSheet`/`Toast` COLLAPSE the transition.
 */
describe('accessibility: reduced motion (criterion 5)', () => {
  it('Spinner honours isReduceMotionEnabled and slows rather than freezes', async () => {
    const flush = mockReducedMotion(true)
    const timing = spyAnimations()
    render(<Spinner label="Loading" />)
    await flush()

    expect(AccessibilityInfo.isReduceMotionEnabled).toHaveBeenCalled()
    expect(timing).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ duration: SPINNER_REDUCED_DURATION_MS }),
    )
    expect(SPINNER_REDUCED_DURATION_MS).toBeGreaterThan(SPINNER_NORMAL_DURATION_MS)
  })

  it('Skeleton honours isReduceMotionEnabled and slows rather than freezes', async () => {
    const flush = mockReducedMotion(true)
    const timing = spyAnimations()
    render(<Skeleton testID="skeleton" />)
    await flush()

    expect(AccessibilityInfo.isReduceMotionEnabled).toHaveBeenCalled()
    expect(timing).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ duration: SPINNER_REDUCED_DURATION_MS }),
    )
  })

  it('BottomSheet collapses its slide under reduced motion', async () => {
    const flush = mockReducedMotion(true)
    render(
      <BottomSheet visible onClose={() => {}} testID="rm-sheet">
        <Skeleton />
      </BottomSheet>,
    )
    expect(screen.getByTestId('rm-sheet').props.animationType).toBe('slide')

    await flush()
    expect(AccessibilityInfo.isReduceMotionEnabled).toHaveBeenCalled()
    expect(screen.getByTestId('rm-sheet').props.animationType).toBe('none')
  })

  it('ToastProvider collapses its enter transition to zero under reduced motion', async () => {
    const flush = mockReducedMotion(true)
    const timing = spyAnimations()
    render(
      <ToastProvider>
        <ToastHarness />
      </ToastProvider>,
    )
    await flush()

    expect(AccessibilityInfo.isReduceMotionEnabled).toHaveBeenCalled()
    expect(timing).toHaveBeenLastCalledWith(
      expect.anything(),
      expect.objectContaining({ duration: 0 }),
    )
  })
})
