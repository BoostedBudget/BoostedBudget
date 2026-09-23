import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'

const rootPackage = require('../../../../package.json')
const webPackage = require('../../../web/package.json')
const apiClientPackage = require('../../../../packages/api-client/package.json')
const mobilePackage = require('../../package.json')
const mobileAppConfig = require('../../app.json')

const MOBILE_ROOT = dirname(require.resolve('../../package.json'))
const REPO_ROOT = resolve(MOBILE_ROOT, '..', '..')
const DEPENDABOT_PATH = resolve(REPO_ROOT, '.github/dependabot.yml')
const dependabotYaml = readFileSync(DEPENDABOT_PATH, 'utf8')

describe('universal React dependency policy', () => {
  it('pins the validated NativeWind release candidate exactly', () => {
    expect(mobilePackage.dependencies.nativewind).toBe('5.0.0-rc.0')
  })

  it('installs Android system appearance support when automatic theming is enabled', () => {
    expect(mobileAppConfig.expo.userInterfaceStyle).toBe('automatic')
    expect(mobilePackage.dependencies['expo-system-ui']).toBeDefined()
  })

  it('declares one explicit React runtime across applications and API-client development', () => {
    const expectedReact = mobilePackage.dependencies.react
    const expectedReactDOM = mobilePackage.dependencies['react-dom']

    expect(expectedReact).toMatch(/^19\.2\.\d+$/)
    expect(expectedReactDOM).toBe(expectedReact)

    expect(rootPackage.dependencies.react).toBe(expectedReact)
    expect(rootPackage.dependencies['react-dom']).toBe(expectedReactDOM)
    expect(webPackage.dependencies.react).toBe(expectedReact)
    expect(webPackage.dependencies['react-dom']).toBe(expectedReactDOM)
    expect(apiClientPackage.devDependencies.react).toBe(expectedReact)
    expect(apiClientPackage.devDependencies['react-dom']).toBe(expectedReactDOM)
  })

  it('keeps the web router on the supported React 19 peer graph', () => {
    const routerMatch = webPackage.dependencies['react-router'].match(/^\^(\d+)\.(\d+)\.(\d+)$/)

    expect(routerMatch).not.toBeNull()
    const [, major, minor, patch] = routerMatch!
    expect(Number(major)).toBe(7)
    expect(Number(minor) > 13 || (Number(minor) === 13 && Number(patch) >= 1)).toBe(true)
  })

  it('uses the validated Expo 57 and React Native compatibility ranges', () => {
    expect(mobilePackage.dependencies.expo).toMatch(/^~57\.\d+\.\d+$/)
    expect(mobilePackage.dependencies['expo-router']).toMatch(/^~57\.\d+\.\d+$/)
    expect(mobilePackage.dependencies['react-native']).toMatch(/^0\.86\.\d+$/)
    expect(mobilePackage.dependencies['react-native-web']).toMatch(/^~0\.21\.\d+$/)
    expect(mobilePackage.dependencies['@tanstack/react-query']).toMatch(/^\^5\.\d+\.\d+$/)
    expect(webPackage.dependencies['@tanstack/react-query']).toMatch(/^\^5\.\d+\.\d+$/)
    expect(apiClientPackage.peerDependencies['@tanstack/react-query']).toMatch(/^\^5\.\d+\.\d+$/)
    expect(apiClientPackage.devDependencies['@tanstack/react-query']).toMatch(/^\^5\.\d+\.\d+$/)
    expect(mobilePackage.dependencies['react-native-css']).toBe('3.1.0-rc.0')
  })

  it('does not silently replace workspace dependency contracts with global overrides', () => {
    expect(rootPackage.overrides.react).toBeUndefined()
    expect(rootPackage.overrides['react-dom']).toBeUndefined()
    expect(rootPackage.overrides.lightningcss).toBeUndefined()
  })
})

describe('mobile Clerk dependency policy', () => {
  it('declares @clerk/expo with a numeric semver spec', () => {
    // The CI guard (scripts/check-mobile-ci.py) matches no compatibility pattern for
    // @clerk/expo, so it does NOT semver-check it. This assertion is the only pin.
    expect(mobilePackage.dependencies['@clerk/expo']).toMatch(/^[\^~]?\d+\.\d+\.\d+/)
  })

  it('bans the deprecated @clerk/clerk-expo package', () => {
    expect(mobilePackage.dependencies['@clerk/clerk-expo']).toBeUndefined()
    expect(mobilePackage.devDependencies['@clerk/clerk-expo']).toBeUndefined()
  })

  it('bans the deprecated @clerk/expo-passkeys package', () => {
    expect(mobilePackage.dependencies['@clerk/expo-passkeys']).toBeUndefined()
    expect(mobilePackage.devDependencies['@clerk/expo-passkeys']).toBeUndefined()
  })

  it('holds @clerk major updates for manual review in Dependabot', () => {
    // The CI guard (scripts/check-mobile-ci.py) only constrains dependencies in
    // MOBILE_COMPATIBILITY_DEPS or the Expo compatibility group, and @clerk/expo is in
    // neither. The Dependabot ignore is therefore the only mechanism that keeps a Clerk
    // major from arriving as a routine bump, so this assertion reads the YAML as text.
    const lines = dependabotYaml.split('\n')
    const entryIndex = lines.findIndex((line) =>
      line.includes('dependency-name: "@clerk/*"'),
    )
    expect(entryIndex).toBeGreaterThanOrEqual(0)

    // A bare dependency-name match is insufficient: the update type must sit with the
    // entry, so assert both inside the same three-line window.
    const window = lines.slice(entryIndex, entryIndex + 3).join('\n')
    expect(window).toContain('dependency-name: "@clerk/*"')
    expect(window).toContain('version-update:semver-major')
  })
})
