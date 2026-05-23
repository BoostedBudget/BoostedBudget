import type { Page, Locator } from '@playwright/test'

export async function dragHandleInPhases(
  page: Page,
  handle: Locator,
  options: {
    dx: number
    midpointFraction?: number
    steps?: number
    cellLocator?: Locator
  }
): Promise<{ midWidth: number; finalWidth: number; startX: number; startY: number }> {
  const { dx, midpointFraction = 0.5, steps = 5 } = options
  const bodyCell = options.cellLocator ?? page.locator('table tbody tr').first().locator('td').first()
  const box = await handle.boundingBox()
  if (!box) throw new Error('Resize handle not visible')

  const startX = box.x + box.width / 2
  const startY = box.y + box.height / 2
  const midX = startX + Math.round(dx * midpointFraction)
  const endX = startX + dx

  await page.mouse.move(startX, startY)
  await page.mouse.down()
  await page.mouse.move(midX, startY, { steps })
  const midWidth = await bodyCell.evaluate((el) => el.getBoundingClientRect().width)
  await page.mouse.move(endX, startY, { steps })
  await page.mouse.up()

  const finalWidth = await bodyCell.evaluate((el) => el.getBoundingClientRect().width)

  return { midWidth, finalWidth, startX, startY }
}
