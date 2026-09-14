/**
 * client.ts tests.
 *
 * Issue #1514 (fix round 6): the dev-mode error logger in this file logged
 * `error.config?.url` verbatim. For the account-deletion cancellation endpoint
 * (`/api/v1/user/delete/cancel/{token}` — a PUBLIC endpoint the backend validates
 * by the token alone, see backend/internal/huma/operations/deletion.go), that
 * token is a bearer capability, not a resource id — logging it verbatim (even in
 * dev-only console output) is a real exposure, not a cosmetic one.
 *
 * This is the first test file in this package (no prior test infrastructure).
 * `bun test` runs it directly with no browser preload or module mocking.
 */
import { afterEach, describe, expect, test } from 'bun:test'
import { readdir } from 'node:fs/promises'
import { dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { AxiosRequestConfig, InternalAxiosRequestConfig } from 'axios'
import { AxiosError, AxiosHeaders } from 'axios'
import ts from 'typescript'
import {
  AXIOS_INSTANCE,
  configureApiClient,
  customInstance,
  getApiUrl,
  redactCapabilityUrlPath,
  setAuthTokenProvider,
} from './client'

const originalAdapter = AXIOS_INSTANCE.defaults.adapter

afterEach(() => {
  configureApiClient()
  AXIOS_INSTANCE.defaults.adapter = originalAdapter
})

function requestConfig(config: AxiosRequestConfig): InternalAxiosRequestConfig {
  return {
    ...config,
    headers: AxiosHeaders.from(config.headers),
  } as InternalAxiosRequestConfig
}

function respondWith(status: number, headers: Record<string, string> = {}) {
  AXIOS_INSTANCE.defaults.adapter = async (config) => {
    if (status >= 400) {
      throw new AxiosError(
        `Request failed with status code ${status}`,
        undefined,
        requestConfig(config),
        undefined,
        {
          data: {},
          status,
          statusText: String(status),
          headers,
          config: requestConfig(config),
        },
      )
    }

    return {
      data: { ok: true },
      status,
      statusText: 'OK',
      headers,
      config: requestConfig(config),
    }
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

const browserRuntimeGlobals = new Set([
  'document',
  'localStorage',
  'location',
  'navigator',
  'sessionStorage',
  'window',
])
const browserAmbientDeclarations = new Set(['ImportMeta', 'ImportMetaEnv', 'Window'])

function isTestSource(relativePath: string) {
  return relativePath.split('/').includes('__tests__')
    || /\.(?:test|spec)(?:\.d)?\.tsx?$/.test(relativePath)
}

async function findPackageSources(
  directory = new URL('./', import.meta.url),
  relativeDirectory = '',
): Promise<Array<{ relativePath: string; url: URL }>> {
  const entries = await readdir(directory, { withFileTypes: true })
  const sources: Array<{ relativePath: string; url: URL }> = []

  for (const entry of entries.sort((left, right) => {
    if (left.name < right.name) return -1
    if (left.name > right.name) return 1
    return 0
  })) {
    const relativePath = `${relativeDirectory}${entry.name}`
    const url = new URL(entry.isDirectory() ? `${entry.name}/` : entry.name, directory)
    if (entry.isDirectory()) {
      sources.push(...await findPackageSources(url, `${relativePath}/`))
    } else if (/\.tsx?$/.test(relativePath) && !isTestSource(relativePath)) {
      sources.push({ relativePath, url })
    }
  }

  return sources
}

function isNamedDeclaration(node: ts.Node, identifier: ts.Identifier): boolean {
  if (
    ts.isBindingElement(node)
    || ts.isClassDeclaration(node)
    || ts.isClassExpression(node)
    || ts.isEnumDeclaration(node)
    || ts.isEnumMember(node)
    || ts.isFunctionDeclaration(node)
    || ts.isFunctionExpression(node)
    || ts.isInterfaceDeclaration(node)
    || ts.isMethodDeclaration(node)
    || ts.isMethodSignature(node)
    || ts.isParameter(node)
    || ts.isPropertyDeclaration(node)
    || ts.isPropertySignature(node)
    || ts.isTypeAliasDeclaration(node)
    || ts.isTypeParameterDeclaration(node)
    || ts.isVariableDeclaration(node)
  ) {
    return node.name === identifier
  }

  return false
}

function isRuntimeIdentifier(node: ts.Identifier, checker: ts.TypeChecker): boolean {
  const parent = node.parent
  if (isNamedDeclaration(parent, node)) return false
  if (ts.isPropertyAccessExpression(parent) && parent.name === node) return false
  if (ts.isQualifiedName(parent) && parent.right === node) return false
  if (ts.isPropertyAssignment(parent) && parent.name === node) return false
  if (ts.isImportClause(parent) || ts.isImportSpecifier(parent) || ts.isNamespaceImport(parent)) {
    return false
  }

  if (ts.isShorthandPropertyAssignment(parent)) {
    return checker.getShorthandAssignmentValueSymbol(parent) === undefined
  }

  return checker.getSymbolAtLocation(node) === undefined
}

function unwrapExpression(expression: ts.Expression): ts.Expression {
  let unwrapped = expression
  while (
    ts.isParenthesizedExpression(unwrapped)
    || ts.isAsExpression(unwrapped)
    || ts.isTypeAssertionExpression(unwrapped)
    || ts.isSatisfiesExpression(unwrapped)
    || ts.isNonNullExpression(unwrapped)
  ) {
    unwrapped = unwrapped.expression
  }
  return unwrapped
}

function isUnboundGlobalThis(expression: ts.Expression, checker: ts.TypeChecker): boolean {
  const root = unwrapExpression(expression)
  if (!ts.isIdentifier(root) || root.text !== 'globalThis') return false
  const symbol = checker.getSymbolAtLocation(root)
  return symbol === undefined
    || symbol.declarations === undefined
    || symbol.declarations.length === 0
}

function collectGlobalThisAliases(
  sourceFile: ts.SourceFile,
  checker: ts.TypeChecker,
): Set<ts.Symbol> {
  // Deliberately flow-insensitive: once a binding can reference globalThis, later local
  // reassignment does not make browser access portable again.
  const aliases = new Set<ts.Symbol>()
  let changed = true

  function isGlobalThisLike(expression: ts.Expression): boolean {
    const root = unwrapExpression(expression)
    if (
      ts.isBinaryExpression(root)
      && root.operatorToken.kind === ts.SyntaxKind.EqualsToken
    ) {
      return isGlobalThisLike(root.right)
    }
    if (!ts.isIdentifier(root)) return false
    if (root.text === 'globalThis' && isUnboundGlobalThis(root, checker)) return true
    const symbol = checker.getSymbolAtLocation(root)
    return symbol !== undefined && aliases.has(symbol)
  }

  while (changed) {
    changed = false
    function visit(node: ts.Node): void {
      if (
        (ts.isVariableDeclaration(node) || ts.isParameter(node))
        && ts.isIdentifier(node.name)
        && node.initializer
        && isGlobalThisLike(node.initializer)
      ) {
        const symbol = checker.getSymbolAtLocation(node.name)
        if (symbol && !aliases.has(symbol)) {
          aliases.add(symbol)
          changed = true
        }
      }
      if (
        ts.isBinaryExpression(node)
        && node.operatorToken.kind === ts.SyntaxKind.EqualsToken
        && ts.isIdentifier(unwrapExpression(node.left))
        && isGlobalThisLike(node.right)
      ) {
        const symbol = checker.getSymbolAtLocation(unwrapExpression(node.left))
        if (symbol && !aliases.has(symbol)) {
          aliases.add(symbol)
          changed = true
        }
      }
      ts.forEachChild(node, visit)
    }
    visit(sourceFile)
  }

  return aliases
}

function destructuredBrowserMemberOnGlobalThis(
  node: ts.Node,
  checker: ts.TypeChecker,
  aliases: Set<ts.Symbol>,
): string | undefined {
  if (!ts.isBindingElement(node) || !ts.isObjectBindingPattern(node.parent)) return undefined
  const owner = node.parent.parent
  if (
    (!ts.isVariableDeclaration(owner) && !ts.isParameter(owner))
    || !owner.initializer
  ) {
    return undefined
  }
  if (!isGlobalThisLike(owner.initializer, checker, aliases)) return undefined

  const property = node.propertyName ?? node.name
  if (!ts.isIdentifier(property) && !ts.isStringLiteralLike(property)
    && !ts.isComputedPropertyName(property)) {
    return undefined
  }
  const name = propertyNameText(property)
  return name && browserRuntimeGlobals.has(name) ? name : undefined
}

function destructuringAssignmentBrowserMembersOnGlobalThis(
  node: ts.Node,
  checker: ts.TypeChecker,
  aliases: Set<ts.Symbol>,
): string[] {
  if (
    !ts.isBinaryExpression(node)
    || node.operatorToken.kind !== ts.SyntaxKind.EqualsToken
    || !isGlobalThisLike(node.right, checker, aliases)
  ) {
    return []
  }

  const target = unwrapExpression(node.left)
  if (!ts.isObjectLiteralExpression(target)) return []

  return target.properties.flatMap((property) => {
    const name = ts.isShorthandPropertyAssignment(property)
      ? property.name.text
      : ts.isPropertyAssignment(property)
        ? propertyNameText(property.name)
        : undefined
    return name && browserRuntimeGlobals.has(name) ? [name] : []
  })
}

function isGlobalThisLike(
  expression: ts.Expression,
  checker: ts.TypeChecker,
  aliases: Set<ts.Symbol>,
): boolean {
  const root = unwrapExpression(expression)
  if (!ts.isIdentifier(root)) return false
  if (root.text === 'globalThis') return isUnboundGlobalThis(root, checker)
  const symbol = checker.getSymbolAtLocation(root)
  return symbol !== undefined && aliases.has(symbol)
}

function browserMemberOnGlobalThis(
  node: ts.Node,
  checker: ts.TypeChecker,
  aliases: Set<ts.Symbol>,
): string | undefined {
  if (
    ts.isPropertyAccessExpression(node)
    && isGlobalThisLike(node.expression, checker, aliases)
    && browserRuntimeGlobals.has(node.name.text)
  ) {
    return node.name.text
  }

  if (
    ts.isElementAccessExpression(node)
    && isGlobalThisLike(node.expression, checker, aliases)
  ) {
    const argument = unwrapExpression(node.argumentExpression)
    if (ts.isStringLiteralLike(argument) && browserRuntimeGlobals.has(argument.text)) {
      return argument.text
    }
  }

  return undefined
}

function isViteClientImport(node: ts.Node): boolean {
  if (
    (ts.isImportDeclaration(node) || ts.isExportDeclaration(node))
    && node.moduleSpecifier
    && ts.isStringLiteral(node.moduleSpecifier)
  ) {
    return node.moduleSpecifier.text === 'vite/client'
  }

  if (
    ts.isImportTypeNode(node)
    && ts.isLiteralTypeNode(node.argument)
    && ts.isStringLiteral(node.argument.literal)
  ) {
    return node.argument.literal.text === 'vite/client'
  }

  return ts.isCallExpression(node)
    && node.expression.kind === ts.SyntaxKind.ImportKeyword
    && node.arguments.length === 1
    && ts.isStringLiteral(node.arguments[0])
    && node.arguments[0].text === 'vite/client'
}

function propertyNameText(name: ts.PropertyName): string | undefined {
  const candidate = ts.isComputedPropertyName(name) ? unwrapExpression(name.expression) : name
  return ts.isIdentifier(candidate) || ts.isStringLiteralLike(candidate)
    ? candidate.text
    : undefined
}

function vitePropertyDeclaration(node: ts.Node): string | undefined {
  if (
    !ts.isPropertyAssignment(node)
    && !ts.isPropertyDeclaration(node)
    && !ts.isPropertySignature(node)
    && !ts.isMethodDeclaration(node)
    && !ts.isMethodSignature(node)
    && !ts.isGetAccessorDeclaration(node)
    && !ts.isSetAccessorDeclaration(node)
  ) {
    return undefined
  }

  const name = propertyNameText(node.name)
  return name?.startsWith('VITE_') ? name : undefined
}

function isGlobalDeclaration(node: ts.Node, sourceFile: ts.SourceFile): boolean {
  for (let parent = node.parent; parent !== sourceFile; parent = parent.parent) {
    if (ts.isModuleDeclaration(parent)) {
      return (parent.flags & ts.NodeFlags.GlobalAugmentation) !== 0
    }
  }

  return !ts.isExternalModule(sourceFile)
}

function isAmbientDeclaration(node: ts.Node, sourceFile: ts.SourceFile): boolean {
  if (sourceFile.isDeclarationFile) return true

  for (let current: ts.Node | undefined = node; current; current = current.parent) {
    if (
      ts.isModuleDeclaration(current)
      && (current.flags & ts.NodeFlags.GlobalAugmentation) !== 0
    ) {
      return true
    }
    if (
      ts.canHaveModifiers(current)
      && ts.getModifiers(current)?.some((modifier) => modifier.kind === ts.SyntaxKind.DeclareKeyword)
    ) {
      return true
    }
  }

  return false
}

function ambientBrowserValueDeclaration(
  node: ts.Node,
  sourceFile: ts.SourceFile,
): string | undefined {
  if (
    !ts.isVariableDeclaration(node)
    && !ts.isFunctionDeclaration(node)
    && !ts.isClassDeclaration(node)
    && !ts.isEnumDeclaration(node)
    && !ts.isModuleDeclaration(node)
  ) {
    return undefined
  }
  if (!node.name || !ts.isIdentifier(node.name)) return undefined
  if (!browserRuntimeGlobals.has(node.name.text)) return undefined
  if (!isAmbientDeclaration(node, sourceFile)) return undefined
  return node.name.text
}

function isBrowserLibraryReference(library: string): boolean {
  const normalized = library.toLowerCase()
  return normalized === 'dom'
    || normalized.startsWith('dom.')
    || normalized === 'webworker'
    || normalized.startsWith('webworker.')
}

function createTypeChecker(sourceFile: ts.SourceFile): ts.TypeChecker {
  const options: ts.CompilerOptions = {
    noLib: true,
    noResolve: true,
    target: ts.ScriptTarget.Latest,
  }
  const host: ts.CompilerHost = {
    fileExists: (fileName) => fileName === sourceFile.fileName,
    getCanonicalFileName: (fileName) => fileName,
    getCurrentDirectory: () => '',
    getDefaultLibFileName: () => 'lib.d.ts',
    getNewLine: () => '\n',
    getSourceFile: (fileName) => fileName === sourceFile.fileName ? sourceFile : undefined,
    readFile: (fileName) => fileName === sourceFile.fileName ? sourceFile.text : undefined,
    useCaseSensitiveFileNames: () => true,
    writeFile: () => {},
  }
  return ts.createProgram([sourceFile.fileName], options, host).getTypeChecker()
}

function findPortabilityViolations(source: string, relativePath: string): string[] {
  const scriptKind = relativePath.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  const sourceFile = ts.createSourceFile(
    relativePath,
    source,
    ts.ScriptTarget.Latest,
    true,
    scriptKind,
  )
  const checker = createTypeChecker(sourceFile)
  const globalThisAliases = collectGlobalThisAliases(sourceFile, checker)
  const violations = new Set<string>()

  for (const reference of sourceFile.typeReferenceDirectives) {
    if (reference.fileName === 'vite/client') violations.add('vite/client type reference')
  }
  for (const reference of sourceFile.libReferenceDirectives) {
    if (isBrowserLibraryReference(reference.fileName)) {
      violations.add(`browser library reference ${reference.fileName}`)
    }
  }

  function visit(node: ts.Node): void {
    if (isViteClientImport(node)) violations.add('vite/client import')

    const ambientBrowserGlobal = ambientBrowserValueDeclaration(node, sourceFile)
    if (ambientBrowserGlobal) {
      violations.add(`ambient browser value declaration ${ambientBrowserGlobal}`)
    }

    if (
      (ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node))
      && browserAmbientDeclarations.has(node.name.text)
      && isGlobalDeclaration(node, sourceFile)
    ) {
      violations.add(`${node.name.text} ambient declaration`)
    }

    if (
      ts.isMetaProperty(node)
      && node.keywordToken === ts.SyntaxKind.ImportKeyword
      && node.name.text === 'meta'
    ) {
      violations.add('import.meta')
    }

    if (ts.isIdentifier(node) && node.text.startsWith('VITE_')) {
      violations.add(`VITE_ identifier ${node.text}`)
    }

    const viteProperty = vitePropertyDeclaration(node)
    if (viteProperty) violations.add(`VITE_ property ${viteProperty}`)

    if (
      ts.isIdentifier(node)
      && browserRuntimeGlobals.has(node.text)
      && isRuntimeIdentifier(node, checker)
    ) {
      violations.add(`browser runtime global ${node.text}`)
    }

    const globalThisMember = browserMemberOnGlobalThis(node, checker, globalThisAliases)
    if (globalThisMember) violations.add(`globalThis browser member ${globalThisMember}`)

    const destructuredMember = destructuredBrowserMemberOnGlobalThis(
      node,
      checker,
      globalThisAliases,
    )
    if (destructuredMember) {
      violations.add(`destructured globalThis browser member ${destructuredMember}`)
    }

    for (const member of destructuringAssignmentBrowserMembersOnGlobalThis(
      node,
      checker,
      globalThisAliases,
    )) {
      violations.add(`destructuring assignment globalThis browser member ${member}`)
    }

    ts.forEachChild(node, visit)
  }

  visit(sourceFile)
  return [...violations].sort().map((description) => `${relativePath}: ${description}`)
}

describe('API client portability detector', () => {
  const forbiddenSyntax = [
    ['optional window access', 'window?.location'],
    ['computed window access', "window['location']"],
    ['optional document access', 'document?.body'],
    ['computed document access', "document['body']"],
    ['local storage access', "localStorage.getItem('key')"],
    ['session storage access', "sessionStorage['token']"],
    ['navigator access', 'navigator.userAgent'],
    ['location access', 'location.href'],
    ['globalThis browser property', 'globalThis.window'],
    ['globalThis computed browser property', "globalThis['window']"],
    ['parenthesized globalThis browser property', '(globalThis).window'],
    ['wrapped globalThis computed browser property', "globalThis[(('window'))]"],
    ['asserted globalThis computed browser property', "globalThis['window' as const]"],
    ['aliased globalThis browser property', 'const root = globalThis; root.location.href'],
    ['chained globalThis alias', 'const root = globalThis; const runtime = root; runtime.window'],
    ['shorthand globalThis destructuring', 'const { window } = globalThis as any; window.location'],
    [
      'destructured globalThis browser property',
      'const { window: browserWindow } = globalThis as any; browserWindow.location',
    ],
    [
      'computed destructuring through a globalThis alias',
      "const root = globalThis; const { ['location']: current } = (root as any); current.href",
    ],
    [
      'post-declaration globalThis assignment',
      'let runtime: any; runtime = globalThis; runtime.location.href',
    ],
    [
      'wrapped assignment with a chained alias',
      "let runtime: any; runtime = (globalThis as any); const next = runtime; next['window']",
    ],
    [
      'destructuring assignment from globalThis',
      'let browserWindow: any; ({ window: browserWindow } = globalThis as any); browserWindow.location',
    ],
    [
      'computed destructuring assignment from a wrapped alias',
      "const root = globalThis; let current: any; ({ ['location']: current } = (root as any)); current.href",
    ],
    [
      'parameter default from globalThis',
      'function read(root: any = globalThis) { return root.window }',
    ],
    [
      'destructured parameter default from globalThis',
      'function read({ window }: any = globalThis) { return window.location }',
    ],
    [
      'computed destructured parameter default from a wrapped alias',
      "const root = globalThis; function read({ ['location']: current }: any = (root as any)) { return current.href }",
    ],
    [
      'chained assignment from globalThis',
      'let root: any, next: any; root = next = globalThis; root.location.href',
    ],
    ['ImportMeta declaration', 'interface ImportMeta { readonly env: Config }'],
    ['ImportMetaEnv declaration', 'interface ImportMetaEnv {}'],
    ['Window declaration', 'interface Window {}'],
    ['import.meta expression', 'import.meta.env'],
    ['Vite identifier', "const VITE_API_URL = 'https://api.example.com'"],
    ['Vite property declaration', 'interface Config { VITE_API_URL: string }'],
    ['computed Vite property declaration', 'interface Config { ["VITE_API_URL"]: string }'],
    ['Vite side-effect import', "import 'vite/client'"],
    ['Vite type import', "import type {} from 'vite/client'"],
    ['Vite type-reference directive', '/// <reference types="vite/client" />'],
  ] as const

  test.each(forbiddenSyntax)('rejects %s', (_description, source) => {
    expect(findPortabilityViolations(source, 'fixture.ts')).not.toEqual([])
  })

  const ambientBrowserGlobals = [
    'window',
    'document',
    'navigator',
    'location',
    'localStorage',
    'sessionStorage',
  ] as const

  test.each(ambientBrowserGlobals)('rejects top-level ambient %s declarations', (name) => {
    const source = `declare var ${name}: { value: string }; ${name}.value`

    expect(findPortabilityViolations(source, 'fixture.ts')).not.toEqual([])
  })

  test.each(ambientBrowserGlobals)('rejects declare global ambient %s declarations', (name) => {
    const source = `
      export {}
      declare global { var ${name}: { value: string } }
      ${name}.value
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).not.toEqual([])
  })

  const ambientValueDeclarationForms = [
    ['function', 'declare function navigator(): string; navigator()'],
    ['class', 'declare class window {}; new window()'],
    ['enum', 'declare enum document { Value }; document.Value'],
    ['namespace', 'declare namespace localStorage { const value: string }; localStorage.value'],
  ] as const

  test.each(ambientValueDeclarationForms)('rejects ambient %s declarations', (_kind, source) => {
    expect(findPortabilityViolations(source, 'fixture.ts')).not.toEqual([])
  })

  const moduleAmbientValueDeclarationForms = [
    ['const', (name: string) => `declare const ${name}: { value: string }; ${name}.value`],
    ['let', (name: string) => `declare let ${name}: { value: string }; ${name}.value`],
    ['var', (name: string) => `declare var ${name}: { value: string }; ${name}.value`],
    ['function', (name: string) => `declare function ${name}(): string; ${name}()`],
    ['class', (name: string) => `declare class ${name} {}; new ${name}()`],
    ['enum', (name: string) => `declare enum ${name} { Value }; ${name}.Value`],
    [
      'namespace',
      (name: string) => `declare namespace ${name} { const value: string }; ${name}.value`,
    ],
  ] as const

  for (const name of ambientBrowserGlobals) {
    test.each(moduleAmbientValueDeclarationForms)(
      `rejects module-scoped ambient %s declarations named ${name}`,
      (_kind, declaration) => {
        const source = `export {}; ${declaration(name)}`

        expect(findPortabilityViolations(source, 'fixture.ts')).not.toEqual([])
      },
    )
  }

  test('accepts equivalent non-ambient local value declarations', () => {
    const source = `
      export function localBindings(): string {
        const window = { value: 'fixture' }
        let document = window
        var sessionStorage = document
        return sessionStorage.value
      }
      export function navigator(): string { return 'fixture' }
      export class window {}
      export enum document { Value }
      export namespace localStorage { export const value = 'fixture' }
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).toEqual([])
  })

  test.each(['dom', 'dom.iterable', 'webworker', 'webworker.importscripts'])(
    'rejects the %s browser library reference',
    (library) => {
      const source = `/// <reference lib="${library}" />\nlet browserValue: Window | Worker`

      expect(findPortabilityViolations(source, 'fixture.ts')).not.toEqual([])
    },
  )

  test('ignores comments and ordinary string prose', () => {
    const source = `
      // window.location document.body import.meta.env VITE_API_URL
      /* interface Window {} and interface ImportMetaEnv {} */
      const prose = 'vite/client globalThis["window"] navigator localStorage sessionStorage'
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).toEqual([])
  })

  test('ignores locally bound and type-only browser-shaped names', () => {
    const source = `
      type window = string
      function read(window: { value: string }): window {
        return window.value
      }
      const model = { window: 'value', navigator: 'value' }
      const location = { href: 'fixture' }
      const root = { window: 'fixture' }
      void location.href
      void root.window
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).toEqual([])
  })

  test('ignores destructuring and assignments sourced from local data', () => {
    const source = `
      const local = { window: { location: 'fixture' }, location: { href: 'fixture' } }
      const { window } = local
      const { window: browserWindow, ['location']: current } = local
      let runtime: typeof local
      runtime = local
      const next = runtime
      void browserWindow.location
      void window.location
      void current.href
      void next.window
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).toEqual([])
  })

  test('ignores destructuring assignments sourced from local data', () => {
    const source = `
      const local = { window: { location: 'fixture' }, location: { href: 'fixture' } }
      let browserWindow: typeof local.window
      let current: typeof local.location
      ({ window: browserWindow, ['location']: current } = local)
      void browserWindow.location
      void current.href
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).toEqual([])
  })

  test('conservatively retains a globalThis alias after local reassignment', () => {
    const source = `
      const local = { location: { href: 'fixture' } }
      let runtime: typeof globalThis | typeof local
      runtime = globalThis
      runtime = local
      void runtime.location.href
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).not.toEqual([])
  })

  test('ignores parameter defaults and chained assignments sourced from local data', () => {
    const source = `
      const local = { window: 'fixture', location: { href: 'fixture' } }
      function read(root: typeof local = local) { return root.window }
      let root: typeof local
      let next: typeof local
      root = next = local
      void read()
      void root.location.href
      void next.window
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).toEqual([])
  })

  test('ignores destructured parameter defaults sourced from local data', () => {
    const source = `
      const local = { window: { location: 'fixture' }, location: { href: 'fixture' } }
      function read(
        { window, ['location']: current }: typeof local = local,
      ) {
        return window.location + current.href
      }
      void read()
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).toEqual([])
  })

  test('ignores module-scoped browser-shaped declarations', () => {
    const source = `
      export interface Window { value: string }
      export interface ImportMeta { value: string }
      export interface ImportMetaEnv { value: string }
    `

    expect(findPortabilityViolations(source, 'fixture.ts')).toEqual([])
  })

  test('excludes test declaration files from package source discovery', () => {
    expect(isTestSource('fixtures/browser.test.d.ts')).toBe(true)
    expect(isTestSource('fixtures/browser.spec.d.ts')).toBe(true)
  })

  test('does not include browser libraries in the package typecheck', async () => {
    const tsconfig = await Bun.file(new URL('../tsconfig.json', import.meta.url)).json()

    expect(tsconfig.compilerOptions.lib).toEqual(['ES2020'])
    expect(tsconfig.compilerOptions.types).toEqual(['react', 'node'])
    expect(tsconfig.exclude).toEqual([
      'node_modules',
      '**/__tests__/**',
      '**/*.test.*',
      '**/*.spec.*',
    ])
  })

  test('effective package typecheck excludes DOM, WebWorker, and jsdom declarations', () => {
    const configUrl = new URL('../tsconfig.json', import.meta.url)
    const configPath = fileURLToPath(configUrl)
    const configFile = ts.readConfigFile(configPath, ts.sys.readFile)
    expect(configFile.error).toBeUndefined()

    const parsed = ts.parseJsonConfigFileContent(
      configFile.config,
      ts.sys,
      dirname(configPath),
      undefined,
      configPath,
    )
    expect(parsed.errors).toEqual([])

    const program = ts.createProgram(parsed.fileNames, parsed.options)
    const forbiddenFiles = program.getSourceFiles()
      .map((sourceFile) => sourceFile.fileName.replaceAll('\\', '/').toLowerCase())
      .filter((fileName) => /\/(?:lib\.dom(?:\.iterable)?|lib\.webworker(?:\.[^/]*)?)\.d\.ts$/.test(fileName)
        || fileName.includes('/@types/jsdom/'))

    expect(forbiddenFiles).toEqual([])
  })
})

describe('portable API client runtime', () => {
  test('does not couple package-owned source to browser or Vite globals', async () => {
    const violations: string[] = []

    for (const { relativePath, url } of await findPackageSources()) {
      const source = await Bun.file(url).text()
      violations.push(...findPortabilityViolations(source, relativePath))
    }

    expect(violations).toEqual([])
  })

  test('imports with no browser globals and retains compatibility exports', async () => {
    expect(typeof globalThis.window).toBe('undefined')
    expect(typeof configureApiClient).toBe('function')
    expect(typeof customInstance).toBe('function')
    expect(typeof setAuthTokenProvider).toBe('function')
    expect(AXIOS_INSTANCE.defaults.timeout).toBe(30000)
    expect(AXIOS_INSTANCE.defaults.headers['Content-Type']).toBe('application/json')
  })

  test('normalizes the configured base URL without changing the axios singleton', () => {
    const originalInstance = AXIOS_INSTANCE

    configureApiClient({ baseUrl: 'https://api.example.com///' })

    expect(AXIOS_INSTANCE).toBe(originalInstance)
    expect(getApiUrl()).toBe('https://api.example.com')
    expect(AXIOS_INSTANCE.defaults.baseURL).toBe('https://api.example.com')
  })

  test('injects the configured bearer token while retaining request headers', async () => {
    let request: AxiosRequestConfig | undefined
    AXIOS_INSTANCE.defaults.adapter = async (config) => {
      request = config
      return {
        data: { ok: true },
        status: 200,
        statusText: 'OK',
        headers: {},
        config: requestConfig(config),
      }
    }
    configureApiClient({ getAccessToken: async () => 'native-token' })

    await customInstance({ url: '/api/v1/accounts', headers: { 'X-Caller': 'mobile' } })

    expect(request?.headers?.Authorization).toBe('Bearer native-token')
    expect(request?.headers?.['X-Caller']).toBe('mobile')
  })

  test('invokes unauthorized only after a token was obtained', async () => {
    let unauthorizedCalls = 0
    respondWith(401)
    configureApiClient({
      getAccessToken: async () => null,
      onUnauthorized: () => { unauthorizedCalls += 1 },
    })
    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)
    expect(unauthorizedCalls).toBe(0)

    setAuthTokenProvider(async () => 'valid-token')
    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)
    expect(unauthorizedCalls).toBe(1)
  })

  test('never invokes unauthorized for public capability paths', async () => {
    let unauthorizedCalls = 0
    respondWith(401)
    configureApiClient({
      getAccessToken: async () => 'valid-token',
      onUnauthorized: () => { unauthorizedCalls += 1 },
    })

    await expect(customInstance({ url: '/api/v1/user/delete/cancel/secret' })).rejects.toBeInstanceOf(AxiosError)

    expect(unauthorizedCalls).toBe(0)
  })

  test('reports only redacted request metadata to the configured error capability', async () => {
    const reports: unknown[] = []
    respondWith(503, { 'x-request-id': 'req-123' })
    configureApiClient({ reportError: (details) => reports.push(details) })

    await expect(
      customInstance({ url: '/api/v1/households/invites/super-secret/accept' }),
    ).rejects.toBeInstanceOf(AxiosError)

    expect(reports).toEqual([{
      url: '/api/v1/households/invites/[redacted]/accept',
      status: 503,
      requestId: 'req-123',
    }])
    expect(JSON.stringify(reports)).not.toContain('super-secret')
  })

  test('reconfiguration replaces runtime state instead of leaking authentication', async () => {
    let firstUnauthorizedCalls = 0
    let secondUnauthorizedCalls = 0
    respondWith(401)
    configureApiClient({
      baseUrl: 'https://first.example.com',
      getAccessToken: async () => 'first-token',
      onUnauthorized: () => { firstUnauthorizedCalls += 1 },
    })
    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)
    expect(firstUnauthorizedCalls).toBe(1)

    configureApiClient({
      baseUrl: 'https://second.example.com/',
      onUnauthorized: () => { secondUnauthorizedCalls += 1 },
    })
    await expect(customInstance({ url: '/api/v1/accounts' })).rejects.toBeInstanceOf(AxiosError)

    expect(firstUnauthorizedCalls).toBe(1)
    expect(secondUnauthorizedCalls).toBe(0)
    expect(getApiUrl()).toBe('https://second.example.com')
  })

  test('overlapping requests retain their originating base URL and callbacks', async () => {
    const firstToken = deferred<string | null>()
    const requests: Array<{ baseUrl: string | undefined; authorization: unknown }> = []
    const unauthorized: string[] = []
    AXIOS_INSTANCE.defaults.adapter = async (config) => {
      requests.push({
        baseUrl: config.baseURL,
        authorization: config.headers.Authorization,
      })
      const internalConfig = requestConfig(config)
      throw new AxiosError('Unauthorized', undefined, internalConfig, undefined, {
        data: {},
        status: 401,
        statusText: 'Unauthorized',
        headers: {},
        config: internalConfig,
      })
    }

    configureApiClient({
      baseUrl: 'https://first.example.com',
      getAccessToken: () => firstToken.promise,
      onUnauthorized: () => { unauthorized.push('first') },
    })
    const firstRequest = customInstance({ url: '/api/v1/accounts' })

    configureApiClient({
      baseUrl: 'https://second.example.com',
      getAccessToken: async () => 'second-token',
      onUnauthorized: () => { unauthorized.push('second') },
    })
    const secondRequest = customInstance({ url: '/api/v1/accounts' })
    await expect(secondRequest).rejects.toBeInstanceOf(AxiosError)

    firstToken.resolve('first-token')
    await expect(firstRequest).rejects.toBeInstanceOf(AxiosError)

    expect(requests).toEqual([
      { baseUrl: 'https://second.example.com', authorization: 'Bearer second-token' },
      { baseUrl: 'https://first.example.com', authorization: 'Bearer first-token' },
    ])
    expect(unauthorized).toEqual(['second', 'first'])
  })

  test('overlapping direct axios requests retain their runtime ownership', async () => {
    const releaseFirst = deferred<void>()
    const firstStarted = deferred<void>()
    const requests: Array<{ baseUrl: string | undefined; url: string | undefined }> = []
    const unauthorized: string[] = []
    AXIOS_INSTANCE.defaults.adapter = async (config) => {
      requests.push({ baseUrl: config.baseURL, url: config.url })
      const internalConfig = requestConfig(config)
      if (config.url === '/prime') {
        return {
          data: { ok: true },
          status: 200,
          statusText: 'OK',
          headers: {},
          config: internalConfig,
        }
      }
      if (config.baseURL === 'https://first.example.com') {
        firstStarted.resolve()
        await releaseFirst.promise
      }
      throw new AxiosError('Unauthorized', undefined, internalConfig, undefined, {
        data: {},
        status: 401,
        statusText: 'Unauthorized',
        headers: {},
        config: internalConfig,
      })
    }

    configureApiClient({
      baseUrl: 'https://first.example.com',
      getAccessToken: async () => 'first-token',
      onUnauthorized: () => { unauthorized.push('first') },
    })
    await customInstance({ url: '/prime' })
    const firstRequest = AXIOS_INSTANCE({ url: '/overlap' })
    await firstStarted.promise

    configureApiClient({
      baseUrl: 'https://second.example.com',
      getAccessToken: async () => 'second-token',
      onUnauthorized: () => { unauthorized.push('second') },
    })
    await customInstance({ url: '/prime' })
    await expect(AXIOS_INSTANCE({ url: '/overlap' })).rejects.toBeInstanceOf(AxiosError)

    releaseFirst.resolve()
    await expect(firstRequest).rejects.toBeInstanceOf(AxiosError)

    expect(requests).toEqual([
      { baseUrl: 'https://first.example.com', url: '/prime' },
      { baseUrl: 'https://first.example.com', url: '/overlap' },
      { baseUrl: 'https://second.example.com', url: '/prime' },
      { baseUrl: 'https://second.example.com', url: '/overlap' },
    ])
    expect(unauthorized).toEqual(['second', 'first'])
  })
})

describe('redactCapabilityUrlPath', () => {
  const TOKEN = 'eyJhbGciOiJIUzI1NiJ9.SENTINEL_CAPABILITY_TOKEN.abc123'

  test('redacts the account-deletion cancellation token path segment', () => {
    const url = `/api/v1/user/delete/cancel/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/api/v1/user/delete/cancel/[redacted]')
  })

  test('redacts the household-invite token path segment, including trailing /accept', () => {
    const url = `/api/v1/households/invites/${TOKEN}/accept`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/api/v1/households/invites/[redacted]/accept')
  })

  // Issue #1514 (fix round 7): round 6 only covered the API path shape. The frontend page
  // route the emailed link actually points at (`/cancel-deletion/{token}`, App.tsx) is a
  // separate shape this dev logger's `error.config?.url` would never carry (it only ever
  // logs API request URLs) — but this package is now the single source of truth for the
  // whole pattern list (see client.ts), so it's tested here regardless of whether this
  // particular call site can reach it, to keep the list itself correct.
  test('redacts the frontend cancel-deletion page route', () => {
    const url = `/cancel-deletion/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/cancel-deletion/[redacted]')
  })

  test('redacts the legacy /household/invite/{token} alias route', () => {
    const url = `/household/invite/${TOKEN}`
    const result = redactCapabilityUrlPath(url)
    expect(result).not.toContain(TOKEN)
    expect(result).toBe('/household/invite/[redacted]')
  })

  test('leaves a URL with no known capability route unchanged', () => {
    const url = '/api/v1/accounts/acc_123'
    expect(redactCapabilityUrlPath(url)).toBe(url)
  })

  test('passes through undefined unchanged', () => {
    expect(redactCapabilityUrlPath(undefined)).toBeUndefined()
  })
})
