const rootPackage = require('../../../../package.json')
const webPackage = require('../../../web/package.json')
const apiClientPackage = require('../../../../packages/api-client/package.json')
const mobilePackage = require('../../package.json')
const mobileAppConfig = require('../../app.json')

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
    expect(mobilePackage.dependencies.react).toBe(expectedReact)
    expect(mobilePackage.dependencies['react-dom']).toBe(expectedReactDOM)
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
