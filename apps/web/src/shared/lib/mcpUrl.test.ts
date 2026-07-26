import { afterEach, beforeEach, describe, expect, test } from 'bun:test'
import { getMcpBaseUrl, getMcpEndpoint } from './mcpUrl'

// import.meta.env fields are declared `readonly` in vite-env.d.ts (correctly, for
// application code). Tests need to mutate them, so we go through a locally-typed,
// non-readonly view rather than relaxing the real ImportMetaEnv contract.
interface MutableViteEnv {
  VITE_MCP_URL?: string
  VITE_ENVIRONMENT?: string
}

function mutableEnv(): MutableViteEnv {
  return import.meta.env as unknown as MutableViteEnv
}

function setViteMcpUrl(value: string | undefined): void {
  if (value === undefined) delete mutableEnv().VITE_MCP_URL
  else mutableEnv().VITE_MCP_URL = value
}

function setViteEnvironment(value: string | undefined): void {
  if (value === undefined) delete mutableEnv().VITE_ENVIRONMENT
  else mutableEnv().VITE_ENVIRONMENT = value
}

const ORIGINAL_VITE_MCP_URL = import.meta.env.VITE_MCP_URL
const ORIGINAL_VITE_ENVIRONMENT = import.meta.env.VITE_ENVIRONMENT

beforeEach(() => {
  setViteMcpUrl(undefined)
  setViteEnvironment(undefined)
  window.ENV = undefined
})

afterEach(() => {
  setViteMcpUrl(ORIGINAL_VITE_MCP_URL)
  setViteEnvironment(ORIGINAL_VITE_ENVIRONMENT)
  window.ENV = undefined
})

describe('getMcpBaseUrl', () => {
  test('VITE_MCP_URL set and valid wins over conflicting window.ENV.MCP_URL and VITE_ENVIRONMENT', () => {
    setViteMcpUrl('https://build-time-mcp.example.com')
    setViteEnvironment('staging')
    window.ENV = { MCP_URL: 'https://runtime-conflict.example.com' }

    expect(getMcpBaseUrl()).toBe('https://build-time-mcp.example.com')
  })

  test('VITE_MCP_URL absent, window.ENV.MCP_URL set and valid wins over the environment map', () => {
    setViteEnvironment('production')
    window.ENV = { MCP_URL: 'https://runtime-mcp.example.com' }

    expect(getMcpBaseUrl()).toBe('https://runtime-mcp.example.com')
  })

  test("neither override, VITE_ENVIRONMENT='production' maps to the production MCP Worker", () => {
    setViteEnvironment('production')

    expect(getMcpBaseUrl()).toBe('https://mcp.boostedbudget.com')
  })

  test("neither override, VITE_ENVIRONMENT='staging' maps to the staging MCP Worker", () => {
    setViteEnvironment('staging')

    expect(getMcpBaseUrl()).toBe('https://staging-mcp.boostedbudget.com')
  })

  test("neither override, VITE_ENVIRONMENT='qa' returns null (no MCP Worker deployed in QA)", () => {
    setViteEnvironment('qa')

    expect(getMcpBaseUrl()).toBeNull()
  })

  test("neither override, VITE_ENVIRONMENT='development' returns null", () => {
    setViteEnvironment('development')

    expect(getMcpBaseUrl()).toBeNull()
  })

  test('neither override, VITE_ENVIRONMENT absent returns null', () => {
    expect(getMcpBaseUrl()).toBeNull()
  })

  test("neither override, VITE_ENVIRONMENT='wat' (unrecognised) returns null", () => {
    setViteEnvironment('wat')

    expect(getMcpBaseUrl()).toBeNull()
  })

  test("neither override, VITE_ENVIRONMENT='' (empty string) returns null", () => {
    setViteEnvironment('')

    expect(getMcpBaseUrl()).toBeNull()
  })

  test('window.ENV entirely undefined does not throw', () => {
    window.ENV = undefined

    expect(() => getMcpBaseUrl()).not.toThrow()
    expect(getMcpBaseUrl()).toBeNull()
  })

  test('whitespace-only VITE_MCP_URL falls through instead of returning null directly', () => {
    setViteMcpUrl('   ')
    setViteEnvironment('production')

    expect(getMcpBaseUrl()).toBe('https://mcp.boostedbudget.com')
  })

  test('non-URL VITE_MCP_URL falls through instead of returning null directly', () => {
    setViteMcpUrl('not-a-url')
    setViteEnvironment('production')

    expect(getMcpBaseUrl()).toBe('https://mcp.boostedbudget.com')
  })

  test('wrong-protocol VITE_MCP_URL (http, not https) falls through instead of returning null directly', () => {
    setViteMcpUrl('http://mcp.boostedbudget.com')
    setViteEnvironment('production')

    expect(getMcpBaseUrl()).toBe('https://mcp.boostedbudget.com')
  })

  test('whitespace-only window.ENV.MCP_URL falls through to the environment map', () => {
    window.ENV = { MCP_URL: '   ' }
    setViteEnvironment('staging')

    expect(getMcpBaseUrl()).toBe('https://staging-mcp.boostedbudget.com')
  })

  test('non-URL window.ENV.MCP_URL falls through to the environment map', () => {
    window.ENV = { MCP_URL: 'not-a-url' }
    setViteEnvironment('staging')

    expect(getMcpBaseUrl()).toBe('https://staging-mcp.boostedbudget.com')
  })

  test('wrong-protocol window.ENV.MCP_URL (http, not https) falls through to the environment map', () => {
    window.ENV = { MCP_URL: 'http://staging-mcp.boostedbudget.com' }
    setViteEnvironment('staging')

    expect(getMcpBaseUrl()).toBe('https://staging-mcp.boostedbudget.com')
  })

  test('trailing slash on VITE_MCP_URL is stripped', () => {
    setViteMcpUrl('https://x.test/')

    expect(getMcpBaseUrl()).toBe('https://x.test')
  })

  // Finding 1: `environment in MCP_URL_BY_ENVIRONMENT` walks the prototype chain, so
  // Object.prototype member names test true via `in` even though they are not own keys
  // of the map. Each of these must resolve to null, not to an inherited function/object.
  test("VITE_ENVIRONMENT='toString' returns null (prototype-chain leak guard)", () => {
    setViteEnvironment('toString')

    expect(getMcpBaseUrl()).toBeNull()
  })

  test("VITE_ENVIRONMENT='constructor' returns null (prototype-chain leak guard)", () => {
    setViteEnvironment('constructor')

    expect(getMcpBaseUrl()).toBeNull()
  })

  test("VITE_ENVIRONMENT='hasOwnProperty' returns null (prototype-chain leak guard)", () => {
    setViteEnvironment('hasOwnProperty')

    expect(getMcpBaseUrl()).toBeNull()
  })

  test("VITE_ENVIRONMENT='__proto__' returns null (prototype-chain leak guard)", () => {
    setViteEnvironment('__proto__')

    expect(getMcpBaseUrl()).toBeNull()
  })

  // Finding 2: pairing a malformed VITE_MCP_URL with a *valid* window.ENV.MCP_URL (rather
  // than with the environment map only) proves step 2 was actually consulted and won, not
  // merely skipped on the way to step 3.
  test('malformed VITE_MCP_URL falls through to a valid window.ENV.MCP_URL (proves step 2 is consulted, not skipped)', () => {
    setViteMcpUrl('not-a-url')
    window.ENV = { MCP_URL: 'https://runtime-mcp.example.com' }

    expect(getMcpBaseUrl()).toBe('https://runtime-mcp.example.com')
  })

  test('whitespace-only VITE_MCP_URL falls through to a valid window.ENV.MCP_URL', () => {
    setViteMcpUrl('   ')
    window.ENV = { MCP_URL: 'https://runtime-mcp.example.com' }

    expect(getMcpBaseUrl()).toBe('https://runtime-mcp.example.com')
  })

  test('wrong-protocol VITE_MCP_URL (http, not https) falls through to a valid window.ENV.MCP_URL', () => {
    setViteMcpUrl('http://mcp.boostedbudget.com')
    window.ENV = { MCP_URL: 'https://runtime-mcp.example.com' }

    expect(getMcpBaseUrl()).toBe('https://runtime-mcp.example.com')
  })
})

describe('getMcpEndpoint', () => {
  test('appends /mcp to the resolved base URL', () => {
    setViteMcpUrl('https://x.test')

    expect(getMcpEndpoint()).toBe('https://x.test/mcp')
  })

  test('trailing slash on the base produces exactly one slash before mcp', () => {
    setViteMcpUrl('https://x.test/')

    expect(getMcpEndpoint()).toBe('https://x.test/mcp')
  })

  test('returns null when the base is null', () => {
    expect(getMcpBaseUrl()).toBeNull()
    expect(getMcpEndpoint()).toBeNull()
  })

  // Finding 1: same prototype-chain leak guard, verified through getMcpEndpoint too.
  test("VITE_ENVIRONMENT='toString' returns null (prototype-chain leak guard)", () => {
    setViteEnvironment('toString')

    expect(getMcpEndpoint()).toBeNull()
  })
})
