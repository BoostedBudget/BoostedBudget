import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'

/**
 * Playwright visual and accessibility layer for the exported mobile web build.
 *
 * This is the browser half of the #1656 criteria that jest cannot observe (no layout engine, no
 * web focus management in React Native's `Modal`) plus the built-HTML half of the Todo 5 theme
 * bootstrap. It runs against the static export, not `expo start`.
 *
 * Screenshots are HUMAN-REVIEW artifacts written under `<attemptDir>`; this suite deliberately
 * makes NO pixel-regression assertion. The "visual" acceptance is carried by axe + geometry, and
 * every screenshot path follows the committed `<viewport>-<theme>*.png` convention below.
 */

// `require.resolve` (not `__dirname`/`import.meta`) because this spec is loaded as CommonJS —
// `apps/mobile` has no `"type": "module"` — and eslint's `no-undef` rejects `__dirname`.
const MOBILE_ROOT = dirname(require.resolve('../../package.json'))
const REPO_ROOT = resolve(MOBILE_ROOT, '..', '..')
const EXPORT_INDEX_HTML = join(MOBILE_ROOT, 'dist', 'web', 'index.html')

const ARTIFACT_DIR = process.env.E2E_VISUAL_ARTIFACT_DIR
  ? resolve(process.env.E2E_VISUAL_ARTIFACT_DIR)
  : join(
      REPO_ROOT,
      '.omo',
      'evidence',
      '1658-mobile-ui-primitives-and-themes',
      'task-22-1658-mobile-ui-primitives-and-themes',
    )

const VIEWPORTS = [
  { name: '320x568', width: 320, height: 568 },
  { name: '375x812', width: 375, height: 812 },
  { name: '768x1024', width: 768, height: 1024 },
  { name: '1024x768', width: 1024, height: 768 },
  { name: '1440x900', width: 1440, height: 900 },
] as const

const THEMES = ['light', 'dark'] as const
type Theme = (typeof THEMES)[number]

/** WCAG 2.1 AA target size (2.5.5), the floor every interactive primitive must clear. */
const TOUCH_FLOOR = 44

/**
 * Every element the browser can activate. `[tabindex]` is included so a focusable control that
 * carries no explicit role is still measured; `[tabindex="-1"]` is excluded because it is
 * programmatically focusable only and never a tap target.
 */
const INTERACTIVE_SELECTOR = [
  'a[href]',
  'button',
  'input',
  'select',
  'textarea',
  '[role="button"]',
  '[role="link"]',
  '[role="tab"]',
  '[role="switch"]',
  '[role="checkbox"]',
  '[role="radio"]',
  '[role="menuitem"]',
  '[role="option"]',
  '[tabindex]:not([tabindex="-1"])',
].join(', ')

const OVERLAYS = [
  { name: 'dialog', trigger: 'Open dialog', testId: 'gallery-light-dialog' },
  { name: 'confirm', trigger: 'Open confirm', testId: 'gallery-light-confirm' },
  { name: 'action sheet', trigger: 'Open action sheet', testId: 'gallery-light-sheet' },
] as const

/** Reduced-motion spin duration, read from the token adapter so it cannot drift from the source. */
const REDUCED_MOTION_DURATION_MS = nativeTokens.light.motion['transition-duration-bb-slow']

function artifactPath(...parts: string[]): string {
  mkdirSync(ARTIFACT_DIR, { recursive: true })
  return join(ARTIFACT_DIR, ...parts)
}

async function openGallery(page: Page, theme: Theme): Promise<void> {
  await page.emulateMedia({ colorScheme: theme })
  await page.goto('/gallery', { waitUntil: 'load' })
  await page.waitForSelector('[data-testid="gallery"]')
  // Let hydration settle so click handlers and the router's <title> are live before asserting.
  await expect(page.getByTestId('gallery')).toBeVisible()
  await page.waitForTimeout(250)
}

async function assertNoHorizontalOverflow(page: Page, label: string): Promise<void> {
  const documentOverflow = await page.evaluate(() => {
    const root = document.scrollingElement
    if (!root) return null
    return { scrollWidth: root.scrollWidth, clientWidth: root.clientWidth }
  })
  expect(documentOverflow, `${label}: document.scrollingElement must exist`).not.toBeNull()
  expect(
    documentOverflow!.scrollWidth,
    `${label}: document must not overflow horizontally`,
  ).toBeLessThanOrEqual(documentOverflow!.clientWidth)

  // The gallery scrolls inside its own container, so the document check above cannot see a
  // horizontally overflowing row. Assert the real scroll container too.
  const scrollerOverflow = await page.evaluate(() => {
    const scroller = document.querySelector('[data-testid="gallery"]')
    if (!scroller) return null
    return { scrollWidth: scroller.scrollWidth, clientWidth: scroller.clientWidth }
  })
  expect(scrollerOverflow, `${label}: the gallery scroll container must exist`).not.toBeNull()
  expect(
    scrollerOverflow!.scrollWidth,
    `${label}: the gallery content must not overflow horizontally`,
  ).toBeLessThanOrEqual(scrollerOverflow!.clientWidth + 1)
}

interface UndersizedTarget {
  tag: string
  role: string | null
  testId: string | null
  ariaLabel: string | null
  width: number
  height: number
}

async function findUndersizedTargets(page: Page): Promise<UndersizedTarget[]> {
  return page.evaluate((selector) => {
    const offenders: UndersizedTarget[] = []
    for (const element of Array.from(document.querySelectorAll(selector))) {
      const rect = element.getBoundingClientRect()
      const style = getComputedStyle(element)
      const rendered = rect.width > 0 || rect.height > 0
      if (!rendered) continue
      if (style.visibility === 'hidden' || style.display === 'none') continue
      if (rect.width < 44 || rect.height < 44) {
        offenders.push({
          tag: element.tagName.toLowerCase(),
          role: element.getAttribute('role'),
          testId: element.getAttribute('data-testid'),
          ariaLabel: element.getAttribute('aria-label'),
          width: Math.round(rect.width * 100) / 100,
          height: Math.round(rect.height * 100) / 100,
        })
      }
    }
    return offenders
  }, INTERACTIVE_SELECTOR)
}

async function assertTouchTargets(page: Page, label: string): Promise<void> {
  const offenders = await findUndersizedTargets(page)
  expect(
    offenders,
    `${label}: every interactive element must be at least ${TOUCH_FLOOR}x${TOUCH_FLOOR}`,
  ).toEqual([])
}

async function assertAxeClean(page: Page, label: string): Promise<void> {
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze()

  // A compact report: the full axe result is ~1.3 MB per viewport, and the actionable part is the
  // violation list plus the counts that prove the run actually scanned something.
  writeFileSync(
    artifactPath(`axe-${label}.json`),
    JSON.stringify(
      {
        url: page.url(),
        viewport: label,
        violations: results.violations,
        incomplete: results.incomplete.map((entry) => ({
          id: entry.id,
          impact: entry.impact,
          nodes: entry.nodes.length,
        })),
        counts: {
          violations: results.violations.length,
          passes: results.passes.length,
          incomplete: results.incomplete.length,
          inapplicable: results.inapplicable.length,
        },
      },
      null,
      2,
    ),
  )

  const blocking = results.violations
    .filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    .map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      help: violation.help,
      nodes: violation.nodes.length,
    }))

  expect(blocking, `${label}: axe must report no serious or critical violations`).toEqual([])
}

interface ContrastViolation {
  testId: string | null
  text: string
  foreground: string
  background: string
  ratio: number
  required: number
}

/**
 * The enforced contrast gate.
 *
 * `@axe-core/playwright`'s `color-contrast` rule is included above, but on this page it returns
 * INCOMPLETE rather than pass/fail: react-native-web stacks absolutely-positioned screen and
 * overlay containers, and axe's background resolution then reports "the background color could
 * not be determined because it is overlapped by another element". An inconclusive rule is a
 * check that silently fails open, so the rendered colours are measured directly here instead —
 * one WCAG 2.1 ratio per text node, against the composited effective background, from the REAL
 * computed styles of the built export. This is the browser half of Todo 20's token-level
 * contrast suite.
 */
async function findContrastViolations(page: Page): Promise<ContrastViolation[]> {
  return page.evaluate((): ContrastViolation[] => {
    type Rgba = [number, number, number, number]

    const parseColor = (value: string): Rgba => {
      const match = value.match(/rgba?\(([^)]+)\)/)
      if (!match) return [0, 0, 0, 0]
      const parts = match[1]
        .split(/[\s,/]+/)
        .filter(Boolean)
        .map(Number)
      return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0, parts[3] ?? 1]
    }

    const composite = (top: Rgba, bottom: Rgba): Rgba => [
      top[0] * top[3] + bottom[0] * (1 - top[3]),
      top[1] * top[3] + bottom[1] * (1 - top[3]),
      top[2] * top[3] + bottom[2] * (1 - top[3]),
      1,
    ]

    const relativeLuminance = (color: Rgba): number => {
      const channels = color.slice(0, 3).map((channel) => {
        const value = channel / 255
        return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
      })
      return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
    }

    const contrastRatio = (a: Rgba, b: Rgba): number => {
      const first = relativeLuminance(a)
      const second = relativeLuminance(b)
      const lighter = Math.max(first, second)
      const darker = Math.min(first, second)
      return (lighter + 0.05) / (darker + 0.05)
    }

    const effectiveBackground = (element: Element): Rgba => {
      let accumulated: Rgba = [255, 255, 255, 1]
      let node: Element | null = element
      while (node) {
        const background = parseColor(getComputedStyle(node).backgroundColor)
        if (background[3] > 0) {
          accumulated = composite(background, accumulated)
          if (background[3] >= 1) break
        }
        node = node.parentElement
      }
      return accumulated
    }

    const root = document.querySelector('[data-testid="gallery"]')
    if (!root) return []

    const violations: ContrastViolation[] = []
    for (const element of Array.from(root.querySelectorAll('*'))) {
      const hasOwnText = Array.from(element.childNodes).some(
        (child) => child.nodeType === 3 && (child.textContent ?? '').trim().length > 0,
      )
      if (!hasOwnText) continue
      // Content hidden from assistive technology is not text for WCAG 1.4.3.
      if (element.closest('[aria-hidden="true"]') !== null) continue
      const text = (element.textContent ?? '').trim()
      // A lone non-alphanumeric character is an icon glyph, which WCAG 1.4.11 governs as a
      // graphical object (3:1), not 1.4.3 as text (4.5:1). Glyphs are out of scope for this gate.
      if (text.length <= 1 && !/[A-Za-z0-9]/.test(text)) continue
      const style = getComputedStyle(element)
      if (style.visibility === 'hidden' || style.display === 'none') continue
      const rect = element.getBoundingClientRect()
      if (rect.width === 0 || rect.height === 0) continue

      const background = effectiveBackground(element)
      const foreground = composite(parseColor(style.color), background)
      const fontSize = Number.parseFloat(style.fontSize)
      const fontWeight = Number.parseInt(style.fontWeight, 10) || 400
      // WCAG 2.1 AA: 3:1 for large text (>= 24px, or >= 18.66px bold), 4.5:1 otherwise.
      const required = fontSize >= 24 || (fontSize >= 18.66 && fontWeight >= 700) ? 3 : 4.5
      const ratio = contrastRatio(foreground, background)
      if (ratio < required) {
        violations.push({
          testId: element.getAttribute('data-testid'),
          text: (element.textContent ?? '').trim().slice(0, 40),
          foreground: style.color,
          background: `rgb(${background
            .slice(0, 3)
            .map((channel) => Math.round(channel))
            .join(', ')})`,
          ratio: Math.round(ratio * 100) / 100,
          required,
        })
      }
    }
    return violations
  })
}

async function assertRenderedContrast(page: Page, label: string): Promise<void> {
  const violations = await findContrastViolations(page)
  expect(
    violations,
    `${label}: every rendered text node must meet the WCAG 2.1 AA contrast floor`,
  ).toEqual([])
}

async function assertNoClippedContent(page: Page, label: string): Promise<void> {
  const clipped = await page.evaluate(() => {
    const root = document.querySelector('[data-testid="gallery"]')
    if (!root) return null
    const offenders: Record<string, unknown>[] = []
    for (const element of Array.from(root.querySelectorAll('*'))) {
      const style = getComputedStyle(element)
      const clipsX = style.overflowX === 'hidden' && element.scrollWidth > element.clientWidth + 1
      const clipsY = style.overflowY === 'hidden' && element.scrollHeight > element.clientHeight + 1
      if (clipsX || clipsY) {
        offenders.push({
          tag: element.tagName.toLowerCase(),
          testId: element.getAttribute('data-testid'),
          scrollWidth: element.scrollWidth,
          clientWidth: element.clientWidth,
          scrollHeight: element.scrollHeight,
          clientHeight: element.clientHeight,
        })
      }
    }
    return offenders
  })
  expect(clipped, `${label}: the gallery root must exist`).not.toBeNull()
  expect(clipped, `${label}: no primitive content may be clipped by an overflow-hidden box`).toEqual(
    [],
  )
}

async function assertNoOverflowingChildren(page: Page, label: string): Promise<void> {
  const overflowing = await page.evaluate(() => {
    const offenders: Record<string, unknown>[] = []
    for (const wrapper of Array.from(document.querySelectorAll('[data-testid*="-entry-"]'))) {
      const wrapperRect = wrapper.getBoundingClientRect()
      for (const child of Array.from(wrapper.children)) {
        const childRect = child.getBoundingClientRect()
        if (childRect.left < wrapperRect.left - 1 || childRect.right > wrapperRect.right + 1) {
          offenders.push({
            wrapper: wrapper.getAttribute('data-testid'),
            child: child.getAttribute('data-testid') ?? child.tagName.toLowerCase(),
            childLeft: Math.round(childRect.left),
            childRight: Math.round(childRect.right),
            wrapperLeft: Math.round(wrapperRect.left),
            wrapperRight: Math.round(wrapperRect.right),
          })
        }
      }
    }
    return offenders
  })
  expect(overflowing, `${label}: no layout primitive may overflow its parent`).toEqual([])
}

async function captureScreenshots(page: Page, viewportName: string, theme: Theme): Promise<void> {
  await page.screenshot({ path: artifactPath(`${viewportName}-${theme}.png`), fullPage: true })
  // The gallery scrolls inside its own container, so a page screenshot only shows the fold.
  // Element screenshots of the two theme sections are the full human-review artifact.
  for (const section of THEMES) {
    await page
      .getByTestId(`gallery-${section}`)
      .screenshot({ path: artifactPath(`${viewportName}-${theme}-${section}-section.png`) })
  }
}

function focusInsideModal(page: Page, modalTestId: string): Promise<boolean> {
  return page.evaluate((id) => {
    const modal = document.querySelector(`[data-testid="${id}"]`)
    if (!modal) return false
    const active = document.activeElement
    if (!active) return false
    if (modal.contains(active)) return true
    // react-native-web's ModalFocusTrap focuses its own container when the dialog has no
    // focusable descendant; that container is the modal's direct parent.
    return modal.parentElement !== null && modal.parentElement.contains(active)
  }, modalTestId)
}

async function measureSpinnerPeriod(page: Page): Promise<number | null> {
  return page.evaluate(async () => {
    const spinner = document.querySelector('[role="progressbar"][aria-label="Loading small"]')
    if (!spinner) return null
    const readAngle = (transform: string): number | null => {
      if (!transform || transform === 'none') return null
      const matrix = transform.match(/matrix\(([^)]+)\)/)
      if (!matrix) return null
      const [a, b] = matrix[1].split(',').map(Number)
      let degrees = (Math.atan2(b, a) * 180) / Math.PI
      if (degrees < 0) degrees += 360
      return degrees
    }
    const start = performance.now()
    let previous = readAngle(getComputedStyle(spinner).transform)
    let travelled = 0
    await new Promise<void>((done) => {
      const tick = () => {
        const now = performance.now()
        const current = readAngle(getComputedStyle(spinner).transform)
        if (current !== null && previous !== null) {
          let delta = current - previous
          if (delta < 0) delta += 360
          travelled += delta
        }
        previous = current
        if (now - start >= 600) done()
        else requestAnimationFrame(tick)
      }
      requestAnimationFrame(tick)
    })
    const elapsed = performance.now() - start
    return travelled > 0 ? elapsed / (travelled / 360) : null
  })
}

test.describe('gallery: viewport, theme, geometry and axe', () => {
  for (const theme of THEMES) {
    for (const viewport of VIEWPORTS) {
      const label = `${viewport.name}-${theme}`
      test(`${label}: no overflow, touch targets, axe, clipping, screenshot`, async ({ page }) => {
        await page.setViewportSize({ width: viewport.width, height: viewport.height })
        await openGallery(page, theme)

        await assertNoHorizontalOverflow(page, label)
        await assertTouchTargets(page, label)
        await assertNoClippedContent(page, label)
        await assertNoOverflowingChildren(page, label)
        await assertRenderedContrast(page, label)
        await assertAxeClean(page, label)
        await captureScreenshots(page, viewport.name, theme)
      })
    }
  }
})

test.describe('gallery: browser focus management', () => {
  test('every overlay traps focus on open, refuses background focus, and restores it on close', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 375, height: 812 })
    await openGallery(page, 'light')

    for (const overlay of OVERLAYS) {
      const scope = page.getByTestId('gallery-light-overlays')
      const trigger = scope.getByRole('button', { name: overlay.trigger })

      await trigger.click()
      await expect(page.getByTestId(overlay.testId), `${overlay.name} opens`).toBeVisible()

      // Focus lands inside the modal's focus scope.
      await expect
        .poll(() => focusInsideModal(page, overlay.testId), {
          message: `${overlay.name}: focus must land inside the modal`,
        })
        .toBe(true)

      // The background is not focusable: a programmatic focus on a sibling is redirected back.
      const backgroundLabel = await page.evaluate(() => {
        const sibling = document.querySelector(
          '[data-testid="gallery-light-overlays"] [role="button"]',
        )
        if (!sibling) return null
        ;(sibling as HTMLElement).focus()
        return sibling.getAttribute('aria-label')
      })
      expect(backgroundLabel, `${overlay.name}: a background trigger must exist`).not.toBeNull()
      await expect
        .poll(() => focusInsideModal(page, overlay.testId), {
          message: `${overlay.name}: background focus must be trapped`,
        })
        .toBe(true)

      // Escape closes (react-native-web's Modal), and focus returns to the trigger.
      await page.keyboard.press('Escape')
      await expect(page.getByTestId(overlay.testId), `${overlay.name} closes`).toBeHidden()
      await expect(trigger, `${overlay.name}: focus must return to its trigger`).toBeFocused()
    }
  })
})

test.describe('gallery: reduced motion', () => {
  test('the running spinner is never faster than the reduced-motion duration', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/gallery', { waitUntil: 'load' })
    await page.waitForSelector('[data-testid="gallery"]')
    await page.waitForTimeout(250)

    const mediaMatches = await page.evaluate(
      () => window.matchMedia('(prefers-reduced-motion: reduce)').matches,
    )
    expect(mediaMatches, 'the reduced-motion media query must be emulated').toBe(true)

    // Any CSS animation/transition the page runs must also honour the reduced duration.
    const cssAnimations = await page.evaluate(() =>
      document.getAnimations().map((animation) => ({
        name: (animation as CSSAnimation).animationName ?? '',
        duration: animation.effect?.getTiming().duration ?? 0,
      })),
    )
    for (const animation of cssAnimations) {
      expect(
        Number(animation.duration),
        `CSS animation ${animation.name || '<unnamed>'} must not be faster than the reduced duration`,
      ).toBeGreaterThanOrEqual(REDUCED_MOTION_DURATION_MS)
    }

    // The spinner animates on the JS driver (see Spinner.tsx). Measure its real period: under
    // reduced motion it must be the reduced duration, not the normal one. `expect.poll` absorbs
    // the async `AccessibilityInfo.isReduceMotionEnabled()` resolution.
    await expect
      .poll(async () => measureSpinnerPeriod(page), {
        message: 'the spinner must be running, not frozen',
        timeout: 10_000,
      })
      .not.toBeNull()

    await expect
      .poll(async () => measureSpinnerPeriod(page), {
        message: 'the reduced-motion spin period must be the reduced duration',
        timeout: 10_000,
      })
      .toBeGreaterThanOrEqual(REDUCED_MOTION_DURATION_MS * 0.85)
  })
})

test.describe('gallery: web animation driver', () => {
  test('no animated primitive logs the native-driver fallback warning', async ({ page }) => {
    const warnings: string[] = []
    page.on('console', (message) => {
      if (message.type() === 'warning' && message.text().includes('useNativeDriver')) {
        warnings.push(message.text())
      }
    })

    await page.setViewportSize({ width: 375, height: 812 })
    await openGallery(page, 'light')

    expect(warnings, 'no primitive may mount with useNativeDriver on web').toEqual([])
  })
})

test.describe('gallery: built theme bootstrap', () => {
  test('the inline theme bootstrap is inlined in the built export before any script src or preload', () => {
    const html = readFileSync(EXPORT_INDEX_HTML, 'utf8')

    const inlineIndex = html.indexOf("var THEME_KEY = 'bb-theme'")
    expect(inlineIndex, 'the built index.html must contain the inlined theme bootstrap').toBeGreaterThan(
      -1,
    )

    // It must be INLINED, not referenced: the nearest preceding <script> must carry no src.
    const openTagStart = html.lastIndexOf('<script', inlineIndex)
    const openTagEnd = html.indexOf('>', openTagStart)
    expect(openTagStart, 'the bootstrap must live inside a <script> element').toBeGreaterThan(-1)
    expect(html.slice(openTagStart, openTagEnd + 1)).not.toContain('src=')

    const firstModuleScript = html.indexOf('<script type="module"')
    const firstPreload = html.indexOf('rel="preload"')
    const firstScriptSrc = html.indexOf('<script src=')

    expect(firstModuleScript, 'the export must emit a module script').toBeGreaterThan(-1)
    expect(firstPreload, 'the export must emit a modulepreload').toBeGreaterThan(-1)
    expect(firstScriptSrc, 'the export must emit a script src').toBeGreaterThan(-1)

    expect(inlineIndex, 'the bootstrap must precede the first module script').toBeLessThan(
      firstModuleScript,
    )
    expect(inlineIndex, 'the bootstrap must precede the first modulepreload').toBeLessThan(
      firstPreload,
    )
    expect(inlineIndex, 'the bootstrap must precede the first <script src=').toBeLessThan(
      firstScriptSrc,
    )
  })
})
