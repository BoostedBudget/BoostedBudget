import { EventEmitter } from 'node:events'
import { test, expect, type Page } from '@playwright/test'
import { captureNavigationJson } from './helpers/capture-navigation-response'

const apiPath = '/api/v1/analytics/cash-flow-summary'
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

// Model one public CDP session. Browser barriers below independently verify its ordering.
function events(options: { attach?: Promise<void>; enable?: Promise<void> } = {}) {
  const emitter = new EventEmitter()
  const pageEvents = new EventEmitter()
  const ready = deferred<void>()
  const initialTimers = process.getActiveResourcesInfo().filter(name => name === 'Timeout').length
  let loaderId = 'old'
  let nextId = 0
  let detached = false
  const bodies = new Map<string, { read: () => Promise<unknown>; encoded: boolean }>()
  const session = Object.assign(emitter, {
    send: async (method: string, params?: { requestId: string }) => {
      if (method === 'Network.enable') await options.enable
      if (method === 'Page.getFrameTree') return { frameTree: { frame: { id: 'main', loaderId: 'old' } } }
      if (method === 'Network.getResponseBody') {
        const source = bodies.get(params!.requestId)!
        const body = await source.read()
        const json = typeof body === 'string' ? body : JSON.stringify(body)
        return { body: source.encoded ? Buffer.from(json).toString('base64') : json, base64Encoded: source.encoded }
      }
      return {}
    },
    detach: async () => { detached = true },
  })
  const page = Object.assign(pageEvents, {
    context: () => ({ newCDPSession: async () => { await options.attach; return session } }),
    isClosed: () => false,
  }) as unknown as Page
  const request = (requestPath = apiPath, frameId = 'main', method = 'GET') => ({
    requestId: String(++nextId), loaderId, frameId,
    request: { url: `http://capture.test${requestPath}`, method },
  })
  const response = (req: ReturnType<typeof request>, body: () => Promise<unknown>, status = 200, encoded = false) => {
    bodies.set(req.requestId, { read: body, encoded })
    emitter.emit('Network.responseReceived', { requestId: req.requestId, response: { status } })
    emitter.emit('Network.loadingFinished', { requestId: req.requestId })
  }
  const commit = (destination = '/destination') => {
    loaderId = `document-${++nextId}`
    emitter.emit('Page.frameNavigated', { frame: { id: 'main', loaderId, url: `http://capture.test${destination}` } })
  }
  const start = (navigate: () => Promise<unknown> = async () => { commit() }, timeout = 500) =>
    captureNavigationJson<{ source: string }>(page, {
      destinationPath: '/destination', apiPath, timeout,
      navigate: () => { ready.resolve(); return navigate() },
    })
  const clean = () => {
    expect(emitter.eventNames().flatMap(name => emitter.listeners(name))).toHaveLength(0)
    expect(pageEvents.eventNames().flatMap(name => pageEvents.listeners(name))).toHaveLength(0)
    expect(process.getActiveResourcesInfo().filter(name => name === 'Timeout')).toHaveLength(initialTimers)
    expect(detached).toBe(true)
  }
  return { emitter, pageEvents, request, response, commit, start, clean, ready: ready.promise }
}

test('event ordering ignores a precommit request even when its response arrives after commit', async () => {
  const f = events()
  const old = f.request()
  f.emitter.emit('Network.requestWillBeSent', old)
  const pending = f.start()
  await f.ready
  f.response(old, async () => ({ source: 'old' }))
  const fresh = f.request(`${apiPath}?period=month`)
  f.emitter.emit('Network.requestWillBeSent', fresh)
  f.response(fresh, async () => ({ source: 'new' }))
  await expect(pending).resolves.toEqual({ source: 'new' })
  f.clean()
})

test('ignores precommit, iframe, non-GET and similar endpoint requests', async () => {
  const f = events()
  const pending = f.start(async () => {
    const early = f.request()
    f.emitter.emit('Network.requestWillBeSent', early)
    f.commit()
    f.response(early, async () => ({ source: 'early' }))
  })
  await f.ready
  for (const req of [f.request(`${apiPath}/other`), f.request(apiPath, 'iframe'), f.request(apiPath, undefined, 'POST')]) {
    f.emitter.emit('Network.requestWillBeSent', req)
    f.response(req, async () => ({ source: 'unrelated' }))
  }
  const req = f.request()
  f.emitter.emit('Network.requestWillBeSent', req)
  f.response(req, async () => ({ source: 'new' }))
  await expect(pending).resolves.toEqual({ source: 'new' })
  f.clean()
})

for (const [failure, expectedError] of [
  ['non-2xx', /503/],
  ['malformed JSON', /JSON/],
  ['rejected body', /body failed/],
  ['stalled body', /timed out/i],
  ['missing headers', /timed out/i],
  ['stalled download', /timed out/i],
  ['navigation failure', /navigation failed/],
  ['superseded navigation', /navigation|destination/i],
  ['unexpected destination', /navigation|destination/i],
  ['page close', /closed/i],
] as const) {
  test(`rejects ${failure} and releases listeners`, async () => {
    const f = events()
    const body = deferred<unknown>()
    const navigation = deferred<void>()
    const pending = f.start(() => navigation.promise, 80)
    const rejected = expect(pending).rejects.toThrow(expectedError)
    await f.ready
    if (failure === 'navigation failure') navigation.reject(new Error('navigation failed'))
    else if (failure === 'unexpected destination') { f.commit('/wrong'); navigation.resolve() }
    else {
      f.commit()
      const req = f.request()
      f.emitter.emit('Network.requestWillBeSent', req)
      if (failure === 'stalled download') f.emitter.emit('Network.responseReceived', { requestId: req.requestId, response: { status: 200 } })
      else if (failure !== 'missing headers') f.response(req, () => {
        if (failure === 'malformed JSON') return Promise.resolve('{')
        if (failure === 'rejected body') return Promise.reject(new Error('body failed'))
        return body.promise
      }, failure === 'non-2xx' ? 503 : 200)
      if (failure === 'superseded navigation') f.commit('/elsewhere')
      if (failure === 'page close') f.pageEvents.emit('close')
      navigation.resolve()
    }
    await rejected
    f.clean()
    // A late body rejection must remain observed even after timeout/navigation teardown.
    // Only unused fixture promises need their own observer; handed-off bodies rely on the helper.
    if (!['stalled body', 'superseded navigation', 'page close'].includes(failure)) {
      void body.promise.catch(() => undefined)
    }
    body.reject(new Error('late body failure'))
    await new Promise<void>(resolve => setImmediate(resolve))
  })
}

test('starts reading JSON on loadingFinished and waits for navigation too', async () => {
  const f = events()
  const navigation = deferred<void>()
  let read = false
  let settled = false
  const pending = f.start(() => navigation.promise).then(value => { settled = true; return value })
  await f.ready
  f.commit()
  const req = f.request()
  f.emitter.emit('Network.requestWillBeSent', req)
  f.response(req, async () => { read = true; return { source: 'new' } })
  expect(read).toBe(true)
  await Promise.resolve()
  expect(settled).toBe(false)
  navigation.resolve()
  await expect(pending).resolves.toEqual({ source: 'new' })
  f.clean()
})

test('body success does not remove the deadline or hide late navigation failure', async () => {
  const f = events()
  const navigation = deferred<void>()
  const pending = f.start(() => navigation.promise, 80)
  const rejected = expect(pending).rejects.toThrow(/timed out/i)
  await f.ready
  f.commit()
  const req = f.request()
  f.emitter.emit('Network.requestWillBeSent', req)
  f.response(req, async () => ({ source: 'new' }))
  await rejected
  f.clean()
  navigation.reject(new Error('late navigation failure'))
  await new Promise<void>(resolve => setImmediate(resolve))
})

test('synchronous navigation failure releases resources', async () => {
  const f = events()
  await expect(f.start(() => { throw new Error('navigation failed synchronously') })).rejects.toThrow(/synchronously/)
  f.clean()
})

test('decodes base64 JSON and keeps the first eligible response', async () => {
  const f = events()
  const body = deferred<unknown>()
  const pending = f.start()
  await f.ready
  const first = f.request()
  const second = f.request()
  f.emitter.emit('Network.requestWillBeSent', first)
  f.emitter.emit('Network.requestWillBeSent', second)
  f.response(first, () => body.promise, 200, true)
  f.emitter.emit('Network.requestWillBeSent', { ...second, redirectResponse: { status: 302 } })
  f.response(second, async () => ({ source: 'wrong' }))
  body.resolve({ source: 'new' })
  await expect(pending).resolves.toEqual({ source: 'new' })
  f.clean()
})

for (const failure of ['request failure', 'session detached', 'different-path history', 'redirect'] as const) {
  test(`rejects ${failure} and cleans up the CDP session`, async () => {
    const f = events()
    const pending = f.start()
    const rejected = expect(pending).rejects.toThrow(/failed|closed|navigation|302/i)
    await f.ready
    const req = f.request()
    f.emitter.emit('Network.requestWillBeSent', req)
    if (failure === 'request failure') f.emitter.emit('Network.loadingFailed', { requestId: req.requestId, errorText: 'net::ERR_FAILED' })
    if (failure === 'session detached') f.emitter.emit('close')
    if (failure === 'different-path history') f.emitter.emit('Page.navigatedWithinDocument', { frameId: 'main', url: 'http://capture.test/elsewhere' })
    if (failure === 'redirect') f.emitter.emit('Network.requestWillBeSent', {
      ...req, redirectResponse: { status: 302 }, request: { url: 'http://capture.test/other', method: 'GET' },
    })
    await rejected
    f.clean()
  })
}

test('ignores a late old-loader request even if first observed after destination commit', async () => {
  const f = events()
  const old = f.request()
  const pending = f.start()
  await f.ready
  f.emitter.emit('Network.requestWillBeSent', old)
  f.response(old, async () => ({ source: 'old' }))
  const fresh = f.request()
  f.emitter.emit('Network.requestWillBeSent', fresh)
  f.response(fresh, async () => ({ source: 'new' }))
  await expect(pending).resolves.toEqual({ source: 'new' })
  f.clean()
})

test('rejects a superseding commit back to the initial document loader', async () => {
  const f = events()
  const pending = f.start()
  const rejected = expect(pending).rejects.toThrow(/navigation superseded/i)
  await f.ready
  f.emitter.emit('Page.frameNavigated', {
    frame: { id: 'main', loaderId: 'old', url: 'http://capture.test/old' },
  })
  await rejected
  f.clean()
})

test('deadline covers session attachment and detaches a session that arrives after timeout', async () => {
  const attach = deferred<void>()
  const f = events({ attach: attach.promise })
  let navigated = false
  await expect(f.start(async () => { navigated = true }, 80)).rejects.toThrow(/timed out/i)
  attach.resolve()
  await new Promise<void>(resolve => setImmediate(resolve))
  expect(navigated).toBe(false)
  f.clean()
})

test('session enable failure cleans up without invoking navigation', async () => {
  const enable = deferred<void>()
  const f = events({ enable: enable.promise })
  let navigated = false
  const rejected = expect(f.start(async () => { navigated = true })).rejects.toThrow(/enable failed/)
  enable.reject(new Error('enable failed'))
  await rejected
  expect(navigated).toBe(false)
  f.clean()
})

test('real browser observes old response while destination commit is held, then captures new document JSON', async ({ page }) => {
  const oldRequested = deferred<void>()
  const releaseOld = deferred<void>()
  const destinationRequested = deferred<void>()
  const releaseDestination = deferred<void>()
  const observed: string[] = []
  await page.route('http://capture.test/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === apiPath) {
      const source = url.searchParams.get('source')!
      if (source === 'old') { oldRequested.resolve(); await releaseOld.promise }
      await route.fulfill({ json: { source } })
    } else if (url.pathname === '/destination') {
      destinationRequested.resolve()
      await releaseDestination.promise
      await route.fulfill({ contentType: 'text/html', body: `<script>fetch('${apiPath}?source=new')</script>` })
    } else await route.fulfill({ contentType: 'text/html', body: '<title>Old document</title>' })
  })
  page.on('response', response => {
    const url = new URL(response.url())
    if (url.pathname === apiPath) observed.push(url.searchParams.get('source')!)
  })
  await page.goto('http://capture.test/old')
  await page.evaluate(path => { void fetch(`${path}?source=old`) }, apiPath)
  await oldRequested.promise
  let settled = false
  const pending = captureNavigationJson<{ source: string }>(page, {
    destinationPath: '/destination', apiPath, timeout: 5000,
    navigate: () => page.goto('http://capture.test/destination', { waitUntil: 'commit' }),
  }).then(value => { settled = true; return value })
  // Observe both branches immediately; a failure must not become an unhandled rejection.
  const result = pending.then(value => ({ value }), error => ({ error }))
  try {
    await destinationRequested.promise
    const oldResponse = page.waitForResponse(r => r.url().endsWith('source=old'))
    releaseOld.resolve()
    await (await oldResponse).finished()
    expect(observed).toEqual(['old'])
    expect(settled).toBe(false)
    releaseDestination.resolve()
    expect(await result).toEqual({ value: { source: 'new' } })
    expect(observed).toEqual(['old', 'new'])
  } finally { releaseOld.resolve(); releaseDestination.resolve(); await result }
})

test('real browser permits same-document replaceState while capturing destination JSON', async ({ page }) => {
  await page.route('http://capture.test/**', route => new URL(route.request().url()).pathname === apiPath
    ? route.fulfill({ json: { source: 'new' } })
    : route.fulfill({ contentType: 'text/html', body: `<script>history.replaceState({}, '', location.href); fetch('${apiPath}')</script>` }))
  await expect(captureNavigationJson(page, {
    destinationPath: '/destination', apiPath, timeout: 5000,
    navigate: () => page.goto('http://capture.test/destination', { waitUntil: 'commit' }),
  })).resolves.toEqual({ source: 'new' })
})

test('real browser rejects a same-URL document reload while JSON is pending', async ({ page }) => {
  const requested = deferred<void>()
  const releaseBody = deferred<void>()
  await page.route('http://capture.test/**', async route => {
    if (new URL(route.request().url()).pathname === apiPath) {
      requested.resolve()
      await releaseBody.promise
      await route.fulfill({ json: { source: 'new' } })
    } else await route.fulfill({ contentType: 'text/html', body: '<title>Destination</title>' })
  })
  const pending = captureNavigationJson(page, {
    destinationPath: '/destination', apiPath, timeout: 5000,
    navigate: () => page.goto('http://capture.test/destination', { waitUntil: 'commit' }),
  })
  const rejected = expect(pending).rejects.toThrow(/navigation|superseded/i)
  try {
    await page.waitForURL('http://capture.test/destination')
    await page.evaluate(path => { void fetch(path).catch(() => undefined) }, apiPath)
    await requested.promise
    await page.reload({ waitUntil: 'commit' })
    await rejected
  } finally { releaseBody.resolve() }
})

test('real browser ignores old-document same-path history while a reload commit is held', async ({ page }) => {
  const reloadRequested = deferred<void>()
  const releaseReload = deferred<void>()
  const historyRequested = deferred<void>()
  const releaseHistory = deferred<void>()
  const observed: string[] = []
  let documents = 0
  await page.route('http://capture.test/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/release-history') {
      historyRequested.resolve()
      await releaseHistory.promise
      await route.fulfill({ body: 'ready' })
    } else if (url.pathname === apiPath) await route.fulfill({ json: { source: url.searchParams.get('source') } })
    else {
      documents++
      if (documents > 1) {
        reloadRequested.resolve()
        await releaseReload.promise
      }
      await route.fulfill({ contentType: 'text/html', body: documents === 1
        ? '<title>Old document at same path</title>'
        : `<script>fetch('${apiPath}?source=new')</script>` })
    }
  })
  page.on('response', response => {
    const url = new URL(response.url())
    if (url.pathname === apiPath) observed.push(url.searchParams.get('source')!)
  })
  await page.goto('http://capture.test/destination')
  // Install the callback before reload; page.evaluate itself waits for an in-flight navigation.
  await page.evaluate(path => {
    void fetch('/release-history').then(async () => {
      history.replaceState({}, '', location.href)
      await (await fetch(`${path}?source=old`)).json()
    })
  }, apiPath)
  await historyRequested.promise
  const pending = captureNavigationJson(page, {
    destinationPath: '/destination', apiPath, timeout: 5000,
    navigate: () => page.reload({ waitUntil: 'commit' }),
  })
  const result = pending.then(value => ({ value }), error => ({ error }))
  try {
    await reloadRequested.promise
    const oldResponse = page.waitForResponse(response => response.url().endsWith('source=old'))
    releaseHistory.resolve()
    await (await oldResponse).finished()
    expect(observed).toEqual(['old'])
    releaseReload.resolve()
    expect(await result).toEqual({ value: { source: 'new' } })
    expect(observed).toEqual(['old', 'new'])
  } finally { releaseHistory.resolve(); releaseReload.resolve(); await result }
})
