import { describe, expect, it } from 'bun:test'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import Color from 'colorjs.io'
import * as tokens from '../src/tokens'

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

// Accessed dynamically so this file still LOADS before the new maps exist; the
// tests then fail on their own assertions rather than on a module-resolution error.
const t = tokens as unknown as Record<string, unknown>

// `--stdout` emits web.css, then nativewind.css, then native.ts. The web slice
// is owned by `desktop-stability.test.ts`; this suite only needs nativewind.
function generateNativewindCss(): string {
  const result = Bun.spawnSync({
    cmd: [process.execPath, 'run', 'scripts/generate.ts', '--stdout'],
    cwd: packageRoot,
  })
  expect(result.exitCode).toBe(0)
  const stdout = result.stdout.toString()
  const separator = '\n--- nativewind.css ---\n'
  const separatorIndex = stdout.indexOf(separator)
  expect(separatorIndex).toBeGreaterThan(-1)
  return stdout.slice(separatorIndex + separator.length)
}

function contrastRatio(foreground: string, background: string): number {
  return new Color(foreground).contrast(new Color(background), 'WCAG21')
}

const expectedScaleKeys = {
  typography: [
    'font-bb-sans',
    'font-bb-mono',
    'text-bb-xs',
    'text-bb-sm',
    'text-bb-body',
    'text-bb-lg',
    'text-bb-xl',
    'text-bb-2xl',
    'text-bb-3xl',
    'text-bb-4xl',
    'leading-bb-tight',
    'leading-bb-snug',
    'leading-bb-normal',
    'leading-bb-relaxed',
    'font-weight-bb-regular',
    'font-weight-bb-medium',
    'font-weight-bb-semibold',
    'font-weight-bb-bold',
    'tracking-bb-tight',
    'tracking-bb-normal',
    'tracking-bb-wide',
  ],
  spacing: [
    'spacing-bb-0',
    'spacing-bb-1',
    'spacing-bb-2',
    'spacing-bb-3',
    'spacing-bb-4',
    'spacing-bb-5',
    'spacing-bb-6',
    'spacing-bb-8',
    'spacing-bb-10',
    'spacing-bb-12',
    'spacing-bb-16',
  ],
  elevation: ['shadow-bb-sm', 'shadow-bb-md', 'shadow-bb-lg'],
  motion: [
    'transition-duration-bb-fast',
    'transition-duration-bb-normal',
    'transition-duration-bb-slow',
    'ease-bb-standard',
    'ease-bb-decelerate',
    'ease-bb-accelerate',
  ],
  touch: ['min', 'comfortable'],
  focus: ['bb-focus-ring-width', 'bb-focus-ring-offset', 'bb-focus-ring-color'],
  contrast: ['bb-info-text', 'bb-danger-text', 'bb-info-strong', 'bb-danger-strong'],
} as const

describe('mobile token scales', () => {
  it('emits every new scale into generated/nativewind.css', () => {
    const css = generateNativewindCss()

    const typography = t.typographyTokens as Record<string, string> | undefined
    const spacing = t.spacingTokens as Record<string, string> | undefined
    const elevation = t.elevationTokens as Record<string, string> | undefined
    const motion = t.motionTokens as Record<string, string> | undefined
    const touch = t.touchTargetTokens as Record<string, number> | undefined
    const focus = t.focusTokens as Record<string, string> | undefined
    const contrast = t.contrastTokens as
      | { light: Record<string, string>; dark: Record<string, string> }
      | undefined

    expect(typography).toBeDefined()
    expect(spacing).toBeDefined()
    expect(elevation).toBeDefined()
    expect(motion).toBeDefined()
    expect(touch).toBeDefined()
    expect(focus).toBeDefined()
    expect(contrast).toBeDefined()

    // Pin the exact key sets so deleting a scale entry fails here rather than silently
    // shrinking the loop below.
    expect(Object.keys(typography!).sort()).toEqual([...expectedScaleKeys.typography].sort())
    expect(Object.keys(spacing!).sort()).toEqual([...expectedScaleKeys.spacing].sort())
    expect(Object.keys(elevation!).sort()).toEqual([...expectedScaleKeys.elevation].sort())
    expect(Object.keys(motion!).sort()).toEqual([...expectedScaleKeys.motion].sort())
    expect(Object.keys(touch!).sort()).toEqual([...expectedScaleKeys.touch].sort())
    expect(Object.keys(focus!).sort()).toEqual([...expectedScaleKeys.focus].sort())
    expect(Object.keys(contrast!.light).sort()).toEqual([...expectedScaleKeys.contrast].sort())
    expect(Object.keys(contrast!.dark).sort()).toEqual([...expectedScaleKeys.contrast].sort())

    for (const [name, value] of Object.entries(typography!)) {
      expect(css).toContain(`--${name}: ${value};`)
    }
    for (const [name, value] of Object.entries(spacing!)) {
      expect(css).toContain(`--${name}: ${value};`)
    }
    for (const [name, value] of Object.entries(elevation!)) {
      expect(css).toContain(`--${name}: ${value};`)
    }
    for (const [name, value] of Object.entries(motion!)) {
      expect(css).toContain(`--${name}: ${value};`)
    }
    for (const [name, value] of Object.entries(touch!)) {
      expect(css).toContain(`--bb-touch-${name}: ${value};`)
    }
    for (const [name, value] of Object.entries(focus!)) {
      expect(css).toContain(`--${name}: ${value};`)
    }

    // Contrast tokens: the raw literals live in the `:root, :host` blocks and the
    // Tailwind aliases live in the `@theme inline` block.
    for (const [name, value] of Object.entries(contrast!.light)) {
      expect(css).toContain(`--${name}: ${value};`)
      expect(css).toContain(`--color-${name}: var(--${name});`)
    }
    for (const [name, value] of Object.entries(contrast!.dark)) {
      expect(css).toContain(`--${name}: ${value};`)
    }
  })

  it('emits a Tailwind utility for every declared namespace', async () => {
    const css = generateNativewindCss()
    const themeBlocks = [...css.matchAll(/@theme inline \{([^}]+)\}/g)]
      .map((match) => match[1])
      .join('\n')
    expect(themeBlocks.length).toBeGreaterThan(0)

    const probes: Array<[variable: string, utility: string]> = [
      ['font-bb-sans', 'font-bb-sans'],
      ['text-bb-body', 'text-bb-body'],
      ['leading-bb-normal', 'leading-bb-normal'],
      ['font-weight-bb-semibold', 'font-bb-semibold'],
      ['tracking-bb-wide', 'tracking-bb-wide'],
      ['spacing-bb-4', 'p-bb-4'],
      ['shadow-bb-md', 'shadow-bb-md'],
      ['transition-duration-bb-normal', 'duration-bb-normal'],
      ['ease-bb-standard', 'ease-bb-standard'],
      ['color-bb-info-text', 'text-bb-info-text'],
    ]

    for (const [variable] of probes) {
      expect(themeBlocks).toContain(`--${variable}:`)
    }

    const classes = probes.map(([, utility]) => utility).join(' ')
    const probeCss = `@import 'tailwindcss';\n@source inline("${classes}");\n@theme inline {\n${themeBlocks}\n}\n`

    const postcss = (await import('postcss')).default
    const tailwind = (await import('@tailwindcss/postcss')).default
    const compiled = await postcss([tailwind()]).process(probeCss, {
      from: resolve(packageRoot, 'probe.css'),
    })

    for (const [, utility] of probes) {
      expect(compiled.css).toContain(`.${utility}`)
    }
  })

  it('remediates the four contrast pairs to at least 4.5:1', () => {
    const contrast = t.contrastTokens as
      | { light: Record<string, string>; dark: Record<string, string> }
      | undefined
    expect(contrast).toBeDefined()

    const light = tokens.lightSemanticTokens
    const dark = tokens.darkSemanticTokens

    const pairs: Array<[label: string, foreground: string, background: string]> = [
      ['light info-text on background', contrast!.light['bb-info-text'], light.background],
      ['light info-foreground on info-strong', light['info-foreground'], contrast!.light['bb-info-strong']],
      ['dark info-foreground on info-strong', dark['info-foreground'], contrast!.dark['bb-info-strong']],
      [
        'dark destructive-foreground on danger-strong',
        dark['destructive-foreground'],
        contrast!.dark['bb-danger-strong'],
      ],
      ['light danger-text on background', contrast!.light['bb-danger-text'], light.background],
      ['dark info-text on background', contrast!.dark['bb-info-text'], dark.background],
      ['dark danger-text on background', contrast!.dark['bb-danger-text'], dark.background],
    ]

    for (const [label, foreground, background] of pairs) {
      expect(contrastRatio(foreground, background), label).toBeGreaterThanOrEqual(4.5)
    }
  })

  it('flips the contrast tokens between light and dark', () => {
    const contrast = t.contrastTokens as
      | { light: Record<string, string>; dark: Record<string, string> }
      | undefined
    expect(contrast).toBeDefined()

    for (const name of Object.keys(contrast!.light)) {
      expect(contrast!.dark[name]).toBeDefined()
      expect(contrast!.dark[name]).not.toBe(contrast!.light[name])
    }
  })
})