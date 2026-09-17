import { readdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

export async function normalizeGeneratedOutput(root) {
  const entries = await readdir(root, { withFileTypes: true })
  await Promise.all(entries.map(async entry => {
    const entryPath = join(root, entry.name)
    if (entry.isDirectory()) return normalizeGeneratedOutput(entryPath)
    if (!entry.isFile() || !entry.name.endsWith('.ts')) return

    const source = await readFile(entryPath, 'utf8')
    const normalized = `${source.trimEnd()}\n`
    if (normalized !== source) await writeFile(entryPath, normalized)
  }))
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  await normalizeGeneratedOutput(fileURLToPath(new URL('../src', import.meta.url)))
}
