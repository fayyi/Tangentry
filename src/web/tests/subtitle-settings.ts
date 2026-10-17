import type { Page } from '@playwright/test'

export async function openSubtitleSettings(page: Page) {
  const toggle = page.locator('.subtitle-settings-toggle')
  await toggle.waitFor({ state: 'visible' })
  if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click()
}
