import { readdirSync, readFileSync } from 'node:fs'
import { extname, join, relative, resolve } from 'node:path'
import { gzipSync } from 'node:zlib'

/**
 * Committed gzip measurement used by the NativeWind evidence gate (#1658).
 *
 * `apps/web` owns the only bundle-size budget (`.size-limit.json` + the `bundle-size` job);
 * the mobile exports have no equivalent, so the gate needs a reproducible way to sum the
 * compressed bytes Metro emits. Doing that with an ad-hoc shell line would make the
 * measurement itself unreviewable and platform-dependent, so it lives here.
 *
 * gzip with default options is deterministic for identical input, which is what lets a
 * re-run of the gate be compared byte-for-byte.
 *
 * `.hbc` is included deliberately: `expo export --platform ios|android` emits the JS bundle
 * as Hermes bytecode under `_expo/static/js/<platform>/`, never as `.js`. A `.js`-only rule
 * would measure the empty generated stylesheet and report a meaningless ~20 gzip bytes for
 * each native platform, which is the opposite of what the gate exists to detect.
 */
const COMPRESSED_EXTENSIONS = new Set(['.js', '.css', '.hbc'])

export interface GzipAsset {
  path: string
  bytes: number
  gzipBytes: number
}

export interface GzipMeasurement {
  root: string
  assets: GzipAsset[]
  fileCount: number
  bytes: number
  gzipBytes: number
}

function listFiles(directory: string): string[] {
  const files: string[] = []
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const full = join(directory, entry.name)
    if (entry.isDirectory()) {
      files.push(...listFiles(full))
    } else if (entry.isFile()) {
      files.push(full)
    }
  }
  return files
}

export function measureGzippedAssets(rootDirectory: string): GzipMeasurement {
  const root = resolve(rootDirectory)
  const files = listFiles(root)
    .filter((file) => COMPRESSED_EXTENSIONS.has(extname(file)))
    .sort()

  const assets = files.map((file) => {
    const contents = readFileSync(file)
    return {
      path: relative(root, file),
      bytes: contents.byteLength,
      gzipBytes: gzipSync(contents).byteLength,
    }
  })

  return {
    root,
    assets,
    fileCount: assets.length,
    bytes: assets.reduce((sum, asset) => sum + asset.bytes, 0),
    gzipBytes: assets.reduce((sum, asset) => sum + asset.gzipBytes, 0),
  }
}
