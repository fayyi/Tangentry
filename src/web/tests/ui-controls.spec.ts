import { expect, test } from './e2e'

test('card cover height stays fixed when card width changes and survives reload', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/tools#display')
  const width = page.getByRole('slider', { name: '卡片宽度', exact: true })
  const height = page.getByRole('slider', { name: '封面高度', exact: true })
  const cover = page.locator('.settings-preview-actual .modern-card-thumbnail')
  await height.fill('180')
  for (const value of ['180', '420']) {
    await width.fill(value)
    await expect.poll(async () => (await cover.boundingBox())?.height).toBe(180)
  }
  await page.reload()
  await expect(width).toHaveValue('420')
  await expect(height).toHaveValue('180')
  await expect.poll(async () => (await cover.boundingBox())?.height).toBe(180)
  expect(await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight)).toBe(true)
})

test('download confirmation traps focus and cancels without deleting records', async ({ page }) => {
  let deletions = 0
  await page.route('**/api/downloads', route => route.fulfill({ json: { jobs: [{
    id: 'ui-cancelled-job', url: 'https://example.com/video', title: '界面验证任务',
    state: 'cancelled', message: '已取消下载。', createdAt: 1700000000,
    maxHeight: 1080, container: 'mp4',
  }] } }))
  await page.route('**/api/downloads/ui-cancelled-job', route => {
    if (route.request().method() === 'DELETE') deletions++
    return route.fulfill({ status: 204 })
  })
  await page.goto('/downloads')
  const trigger = page.getByRole('button', { name: '删除下载记录 界面验证任务', exact: true })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: '删除 1 条记录', exact: true })
  await expect(dialog).toBeVisible()
  expect(await dialog.evaluate(node => node.matches(':modal'))).toBe(true)
  for (let index = 0; index < 5; index++) {
    await page.keyboard.press('Tab')
    expect(await dialog.evaluate(node => node.contains(document.activeElement))).toBe(true)
  }
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()
  expect(deletions).toBe(0)
})
