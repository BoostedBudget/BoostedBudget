import { spawnSync } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, resolve } from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

import { measureGzippedAssets } from './gzip-size'

/**
 * NativeWind v5 release-candidate evidence gate (#1658, Todo 4).
 *
 * This is a runnable probe, not a jest test: it runs the real `expo-doctor` and the three
 * real `expo export` builds, measures what they emitted, and writes
 * `apps/mobile/nativewind-gate.evidence.json`. A test could pass from a worker's self-report;
 * this cannot, because every item carries a measured value beside its threshold.
 *
 * Fixed evidence shape — Todo 6 derives its `KEEP_RC`/`RETAIN` verdict from
 * `evidence.items.every(item => item.pass)`, so the top level is exactly `{ items: [...] }`
 * and every item is exactly `{ id, measured, threshold, pass }`. Do not add sibling keys to
 * the top level without updating Todo 6.
 *
 * `measured` is a number or boolean, never prose: an exit code / count / byte size, or the
 * boolean result of the version-pin comparison. `threshold` states the bound the measurement
 * is checked against; the version items carry `{ expected, resolved }` so the resolved version
 * is recorded beside the pin it was checked against. `pass` is the comparison's result.
 *
 * Baseline: `nativewind-gate-baseline.json` is recorded ONCE, after Wave 1 (todos 1, 2, 3, 5,
 * 26) has landed and BEFORE any Todo 8-18 primitive work, because Wave 1 itself changes the
 * token artifacts — a branch-point baseline would misattribute Wave-1 token work to the
 * NativeWind substrate. The delta items measure the substrate against that frozen baseline.
 *
 * `--check` exits non-zero when any item fails. The `mobile-test` job runs this mode; the
 * workflow step is pinned in `scripts/check-mobile-ci.py`'s REQUIRED_STEPS so the gate cannot
 * be dropped from CI without that guard failing.
 */
const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url))
const MOBILE_DIR = resolve(SCRIPT_DIR, '..')
const REPO_ROOT = resolve(MOBILE_DIR, '..', '..')
const BASELINE_PATH = join(SCRIPT_DIR, 'nativewind-gate-baseline.json')
const EVIDENCE_PATH = join(MOBILE_DIR, 'nativewind-gate.evidence.json')
const LOCKFILE_PATH = join(REPO_ROOT, 'bun.lock')

export const THRESHOLDS = {
  doctorExitCode: 0,
  exportExitCode: 0,
  reactNativeCssCopies: 1,
  webMaxDeltaPercent: 15,
  nativeMaxDeltaPercent: 20,
} as const

const COMMAND_TIMEOUT_MS = 10 * 60 * 1000
const TIMEOUT_EXIT_CODE = 124

type Platform = 'web' | 'ios' | 'android'

const BUNDLE_EXPORTS: ReadonlyArray<{ platform: Platform; script: string; outputDir: string }> = [
  { platform: 'web', script: 'export:web', outputDir: 'dist/web' },
  { platform: 'ios', script: 'export:ios', outputDir: 'dist/ios' },
  { platform: 'android', script: 'export:android', outputDir: 'dist/android' },
]

export interface GateItem {
  id: string
  measured: number | boolean | null
  threshold: unknown
  pass: boolean
}

export interface GateEvidence {
  items: GateItem[]
}

interface Baseline {
  bundles: Record<Platform, { gzipBytes: number }>
}

interface CommandOutcome {
  script: string
  exitCode: number
  detail: string
}

function runScript(script: string): CommandOutcome {
  const result = spawnSync(process.execPath, ['run', script], {
    cwd: MOBILE_DIR,
    timeout: COMMAND_TIMEOUT_MS,
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  })
  const errorCode = result.error ? (result.error as { code?: string }).code : undefined
  const timedOut = errorCode === 'ETIMEDOUT' || result.signal === 'SIGTERM'
  const exitCode = timedOut ? TIMEOUT_EXIT_CODE : (result.status ?? 1)
  const detail = timedOut
    ? `timed out after ${COMMAND_TIMEOUT_MS}ms`
    : (result.stderr ?? '').trim().split('\n').slice(-3).join(' | ')
  return { script, exitCode, detail }
}

export function countResolvedReactNativeCssCopies(lockfilePath: string): number {
  const lockfile = readFileSync(lockfilePath, 'utf8')
  const entry = /"react-native-css(?:@[^"]*)?"\s*:\s*\[\s*"react-native-css@([^"]+)"/g
  const versions = new Set<string>()
  for (const match of lockfile.matchAll(entry)) {
    versions.add(match[1])
  }
  return versions.size
}

function readJson<T>(path: string): T {
  return JSON.parse(readFileSync(path, 'utf8')) as T
}

function installedVersion(packageName: string): string {
  const require = createRequire(import.meta.url)
  let manifestPath: string
  try {
    manifestPath = require.resolve(`${packageName}/package.json`)
  } catch {
    manifestPath = join(REPO_ROOT, 'node_modules', packageName, 'package.json')
  }
  const manifest = readJson<{ version?: string }>(manifestPath)
  if (typeof manifest.version !== 'string') {
    throw new Error(`cannot read a version from ${manifestPath}`)
  }
  return manifest.version
}

function loadBaseline(): Baseline {
  if (!existsSync(BASELINE_PATH)) {
    throw new Error(
      `baseline is missing at ${BASELINE_PATH}; it is committed and must exist to compute deltas`,
    )
  }
  const parsed = readJson<Partial<Baseline>>(BASELINE_PATH)
  if (!parsed.bundles) {
    throw new Error(`baseline ${BASELINE_PATH} has no "bundles" section`)
  }
  for (const { platform } of BUNDLE_EXPORTS) {
    const bundle = parsed.bundles[platform]
    if (!bundle || typeof bundle.gzipBytes !== 'number') {
      throw new Error(`baseline ${BASELINE_PATH} has no numeric bundles.${platform}.gzipBytes`)
    }
  }
  return { bundles: parsed.bundles }
}

function buildItems(baseline: Baseline): { items: GateItem[]; outcomes: CommandOutcome[] } {
  const items: GateItem[] = []
  const outcomes: CommandOutcome[] = []

  const doctor = runScript('doctor')
  outcomes.push(doctor)
  items.push({
    id: 'expo-doctor-exit',
    measured: doctor.exitCode,
    threshold: THRESHOLDS.doctorExitCode,
    pass: doctor.exitCode === THRESHOLDS.doctorExitCode,
  })

  const byPlatform = new Map<Platform, CommandOutcome>()
  for (const { platform, script } of BUNDLE_EXPORTS) {
    const outcome = runScript(script)
    outcomes.push(outcome)
    byPlatform.set(platform, outcome)
    items.push({
      id: `expo-export-${platform}-exit`,
      measured: outcome.exitCode,
      threshold: THRESHOLDS.exportExitCode,
      pass: outcome.exitCode === THRESHOLDS.exportExitCode,
    })
  }

  const copies = countResolvedReactNativeCssCopies(LOCKFILE_PATH)
  items.push({
    id: 'react-native-css-copies',
    measured: copies,
    threshold: THRESHOLDS.reactNativeCssCopies,
    pass: copies === THRESHOLDS.reactNativeCssCopies,
  })

  for (const { platform, outputDir } of BUNDLE_EXPORTS) {
    const outcome = byPlatform.get(platform)!
    const baselineBytes = baseline.bundles[platform].gzipBytes
    const maxDeltaPercent =
      platform === 'web' ? THRESHOLDS.webMaxDeltaPercent : THRESHOLDS.nativeMaxDeltaPercent
    const maxBytes = Math.ceil(baselineBytes * (1 + maxDeltaPercent / 100))
    const maxDeltaBytes = maxBytes - baselineBytes

    const measuredBytes =
      outcome.exitCode === 0
        ? measureGzippedAssets(join(MOBILE_DIR, outputDir)).gzipBytes
        : null
    const deltaBytes = measuredBytes === null ? null : measuredBytes - baselineBytes

    items.push({
      id: `${platform}-export-gzip-bytes`,
      measured: measuredBytes,
      threshold: { baselineBytes, maxDeltaPercent, maxBytes },
      pass: measuredBytes !== null && measuredBytes <= maxBytes,
    })
    items.push({
      id: `${platform}-export-gzip-delta-bytes`,
      measured: deltaBytes,
      threshold: { baselineBytes, maxDeltaBytes },
      pass: deltaBytes !== null && deltaBytes <= maxDeltaBytes,
    })
  }

  const mobilePackage = readJson<{ dependencies?: Record<string, string> }>(
    join(MOBILE_DIR, 'package.json'),
  )
  const declared = mobilePackage.dependencies ?? {}
  for (const packageName of ['nativewind', 'react-native-css'] as const) {
    const resolved = installedVersion(packageName)
    const expected = declared[packageName]
    const pinned = expected !== undefined && resolved === expected
    items.push({
      id: `${packageName}-version`,
      measured: pinned,
      threshold: { expected: expected ?? null, resolved },
      pass: pinned,
    })
  }

  return { items, outcomes }
}

function report(items: GateItem[], outcomes: CommandOutcome[]): void {
  process.stdout.write(`NativeWind RC evidence gate (${EVIDENCE_PATH})\n`)
  for (const item of items) {
    const status = item.pass ? 'PASS' : 'FAIL'
    process.stdout.write(
      `  ${status}  ${item.id}: measured=${JSON.stringify(item.measured)} ` +
        `threshold=${JSON.stringify(item.threshold)}\n`,
    )
  }
  for (const outcome of outcomes) {
    if (outcome.exitCode !== 0 && outcome.detail) {
      process.stdout.write(`  detail  ${outcome.script}: ${outcome.detail}\n`)
    }
  }
  const passed = items.filter((item) => item.pass).length
  process.stdout.write(`${passed}/${items.length} items passed\n`)
}

function main(): number {
  const baseline = loadBaseline()
  const { items, outcomes } = buildItems(baseline)
  const evidence: GateEvidence = { items }
  writeFileSync(EVIDENCE_PATH, `${JSON.stringify(evidence, null, 2)}\n`)
  report(items, outcomes)

  const failures = items.filter((item) => !item.pass).length
  if (process.argv.includes('--check') && failures > 0) {
    process.stderr.write(`nativewind-gate: ${failures} threshold(s) failed\n`)
    return 1
  }
  return 0
}

process.exitCode = main()
