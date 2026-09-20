import { afterEach } from '@jest/globals'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { render, screen } from '@testing-library/react-native'
import { Box } from '../../components/ui/Box'
import { Inline } from '../../components/ui/Inline'
import { Stack } from '../../components/ui/Stack'
import { Surface } from '../../components/ui/Surface'
import { TEXT_VARIANT_CLASSES, Text, type TextVariant } from '../../components/ui/Text'
import { UI_DIR, uiFiles } from '../primitives/registry-coverage'

afterEach(() => {
  jest.restoreAllMocks()
})

const TEXT_VARIANTS = Object.keys(TEXT_VARIANT_CLASSES) as TextVariant[]

/**
 * Criterion 4 (#1656): dynamic type.
 *
 * "No primitive sets allowFontScaling={false}" is enforced two ways: a source scan over every file
 * in `components/ui/`, and a render assertion over every `Text` variant. The scan catches a literal
 * anywhere (including a primitive the table does not render); the render catches a runtime value.
 */
describe('accessibility: dynamic type (criterion 4)', () => {
  it('never disables font scaling in any primitive source file', () => {
    const offenders: string[] = []
    for (const file of uiFiles()) {
      const source = readFileSync(resolve(UI_DIR, file), 'utf8')
      if (/allowFontScaling\s*[:=]\s*\{?\s*false/.test(source)) offenders.push(file)
    }
    expect(offenders).toEqual([])
  })

  for (const variant of TEXT_VARIANTS) {
    it(`Text variant "${variant}" keeps font scaling on`, () => {
      render(<Text testID={`dt-${variant}`} variant={variant} />)
      expect(screen.getByTestId(`dt-${variant}`).props.allowFontScaling).not.toBe(false)
    })

    it(`Text variant "${variant}" forwards maxFontSizeMultiplier`, () => {
      render(<Text testID={`dt-m-${variant}`} variant={variant} maxFontSizeMultiplier={2} />)
      expect(screen.getByTestId(`dt-m-${variant}`).props.maxFontSizeMultiplier).toBe(2)
    })
  }

  it('leaves maxFontSizeMultiplier unset by default so the OS preference applies', () => {
    render(<Text testID="dt-default">Body</Text>)
    expect(screen.getByTestId('dt-default').props.maxFontSizeMultiplier).toBeUndefined()
  })

  const layoutCases: ReadonlyArray<{ name: string; testID: string; element: React.ReactElement }> = [
    { name: 'Box', testID: 'dt-box', element: <Box testID="dt-box" p={4} /> },
    { name: 'Stack', testID: 'dt-stack', element: <Stack testID="dt-stack" gap={4} /> },
    { name: 'Inline', testID: 'dt-inline', element: <Inline testID="dt-inline" gap={4} /> },
    { name: 'Surface', testID: 'dt-surface', element: <Surface testID="dt-surface" /> },
  ]

  for (const item of layoutCases) {
    it(`layout primitive ${item.name} does not cap font growth`, () => {
      render(item.element)
      const node = screen.getByTestId(item.testID)
      expect(node.props.allowFontScaling).not.toBe(false)
      expect(node.props.maxFontSizeMultiplier).toBeUndefined()
    })
  }
})
