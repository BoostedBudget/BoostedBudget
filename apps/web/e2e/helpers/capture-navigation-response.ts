import type { CDPSession, Page } from '@playwright/test'

/** Chromium-only E2E capture: one CDP stream gives exact document and request identity. */
export function captureNavigationJson<T>(
  page: Page,
  options: { destinationPath: string; apiPath: string; timeout: number; navigate: () => Promise<unknown> }
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const requests = new Set<string>()
    let session: CDPSession | undefined
    let mainFrameId: string
    let initialLoaderId: string
    let loaderId: string | undefined
    let selectedRequest: string | undefined
    let navigationDone = false
    let bodyDone = false
    let reading = false
    let settled = false
    let value: T

    const cleanup = () => {
      clearTimeout(timer)
      page.off('close', onClose)
      if (session) {
        session.off('Page.frameNavigated', onNavigation)
        session.off('Page.navigatedWithinDocument', onHistory)
        session.off('Network.requestWillBeSent', onRequest)
        session.off('Network.responseReceived', onResponse)
        session.off('Network.loadingFinished', onFinished)
        session.off('Network.loadingFailed', onFailed)
        session.off('close', onSessionClose)
        // Detach is observed but cannot extend the capture deadline if the browser is unresponsive.
        void session.detach().catch(() => undefined)
      }
      requests.clear()
    }
    const fail = (error: unknown) => {
      if (settled) return
      settled = true
      cleanup()
      reject(error)
    }
    const complete = () => {
      if (settled || !navigationDone || !bodyDone) return
      settled = true
      cleanup()
      resolve(value)
    }
    const onNavigation = ({ frame }: { frame: { id: string; loaderId: string; url: string } }) => {
      if (frame.id !== mainFrameId || (!loaderId && frame.loaderId === initialLoaderId)) return
      if (loaderId || new URL(frame.url).pathname !== options.destinationPath) {
        fail(new Error(`Navigation superseded expected destination ${options.destinationPath}: ${frame.url}`))
        return
      }
      loaderId = frame.loaderId
    }
    const onHistory = ({ frameId, url }: { frameId: string; url: string }) => {
      if (loaderId && frameId === mainFrameId && new URL(url).pathname !== options.destinationPath) {
        fail(new Error(`Navigation left expected destination ${options.destinationPath}: ${url}`))
      }
    }
    const onRequest = (event: {
      requestId: string; loaderId: string; frameId?: string
      request: { url: string; method: string }; redirectResponse?: { status: number }
    }) => {
      // CDP reuses requestId on redirects; never let a redirect inherit an eligible identity.
      if (requests.has(event.requestId) && event.redirectResponse) {
        if (!selectedRequest || selectedRequest === event.requestId) {
          fail(new Error(`GET ${options.apiPath} returned HTTP ${event.redirectResponse.status}`))
        }
        return
      }
      if (!loaderId || event.loaderId !== loaderId || event.frameId !== mainFrameId || selectedRequest) return
      if (event.request.method === 'GET' && new URL(event.request.url).pathname === options.apiPath) {
        requests.add(event.requestId)
      }
    }
    const onResponse = ({ requestId, response }: { requestId: string; response: { status: number } }) => {
      if (selectedRequest || !requests.has(requestId)) return
      selectedRequest = requestId
      if (response.status < 200 || response.status >= 300) {
        fail(new Error(`GET ${options.apiPath} returned HTTP ${response.status}`))
      }
    }
    const onFinished = ({ requestId }: { requestId: string }) => {
      if (requestId !== selectedRequest || reading) return
      reading = true
      // CDP makes bodies available at loadingFinished, not at headers. Read on that event,
      // with no navigation await, using the same session that established document ownership.
      void session!.send('Network.getResponseBody', { requestId }).then(result => {
        try {
          value = JSON.parse(result.base64Encoded ? Buffer.from(result.body, 'base64').toString('utf8') : result.body) as T
          bodyDone = true
          complete()
        } catch (error) { fail(error) }
      }, fail)
    }
    const onFailed = ({ requestId, errorText }: { requestId: string; errorText: string }) => {
      if (requests.has(requestId) && (!selectedRequest || selectedRequest === requestId)) {
        fail(new Error(`GET ${options.apiPath} failed: ${errorText}`))
      }
    }
    const onSessionClose = () => fail(new Error(`CDP session closed while capturing ${options.apiPath}`))
    const onClose = () => fail(new Error(`Page closed while capturing ${options.apiPath}`))
    const timer = setTimeout(() => fail(new Error(`Timed out capturing ${options.apiPath} after ${options.timeout}ms`)), options.timeout)

    page.on('close', onClose)
    if (page.isClosed()) { onClose(); return }
    const setup = async () => {
      session = await page.context().newCDPSession(page)
      if (settled) { void session.detach().catch(() => undefined); return }
      session.on('close', onSessionClose)
      await session.send('Page.enable')
      if (settled) return
      await session.send('Network.enable')
      if (settled) return
      const { frameTree } = await session.send('Page.getFrameTree')
      if (settled) return
      mainFrameId = frameTree.frame.id
      initialLoaderId = frameTree.frame.loaderId
      session.on('Page.frameNavigated', onNavigation)
      session.on('Page.navigatedWithinDocument', onHistory)
      session.on('Network.requestWillBeSent', onRequest)
      session.on('Network.responseReceived', onResponse)
      session.on('Network.loadingFinished', onFinished)
      session.on('Network.loadingFailed', onFailed)
      // Both navigation and body promises are observed immediately, including late rejections.
      await options.navigate()
      if (!settled) {
        navigationDone = true
        complete()
      }
    }
    void setup().catch(fail)
  })
}
