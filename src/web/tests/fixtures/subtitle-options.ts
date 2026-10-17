import type { Page } from '@playwright/test'
import { openSubtitleSettings } from '../subtitle-settings'
export async function openSubtitleOptions(page: Page) {
  await openSubtitleSettings(page)
  if (!await page.locator('details.subtitle-management').evaluate((node: HTMLDetailsElement) => node.open)) await page.locator('details.subtitle-management > summary').click()
}
export async function closeSubtitleOptions(page: Page) {
  if (await page.locator('details.subtitle-management').evaluate((node: HTMLDetailsElement) => node.open)) await page.locator('details.subtitle-management > summary').click()
}
