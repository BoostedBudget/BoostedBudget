const rootPackage = require('../../../../package.json')
const webPackage = require('../../../web/package.json')
const apiClientPackage = require('../../../../packages/api-client/package.json')
const mobilePackage = require('../../package.json')

describe('universal React dependency policy', () => {
  it('declares one explicit React runtime across applications and API-client development', () => {
    const expectedReact = mobilePackage.dependencies.react

    expect(rootPackage.dependencies.react).toBe(expectedReact)
    expect(rootPackage.dependencies['react-dom']).toBe(mobilePackage.dependencies['react-dom'])
    expect(webPackage.dependencies.react).toBe(expectedReact)
    expect(webPackage.dependencies['react-dom']).toBe(mobilePackage.dependencies['react-dom'])
    expect(apiClientPackage.devDependencies.react).toBe(expectedReact)
  })

  it('does not silently replace workspace dependency contracts with global overrides', () => {
    expect(rootPackage.overrides.react).toBeUndefined()
    expect(rootPackage.overrides['react-dom']).toBeUndefined()
    expect(rootPackage.overrides.lightningcss).toBeUndefined()
  })
})
