import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'

test('聊天输入区在窄栏保持两行结构', async ({ page }) => {
  const css = await readFile(new URL('../src/features/ai/ai.css', import.meta.url), 'utf8')
  await page.setContent(`<style>
    * { box-sizing: border-box; }
    body { margin: 0; font: 12px sans-serif; }
    .icon-button { width: 36px; height: 36px; }
    ${css}
  </style><main class="shell"><div class="ai-chat-panel">
    <div class="ai-chat-suggestions"><button type="button">列出关键观点</button></div>
    <div class="ai-chat-composer">
      <textarea rows="2" placeholder="输入问题"></textarea>
      <div class="ai-chat-toolbar"><select aria-label="对话模型"><option>选择模型</option><option>通用对话模型</option></select><button class="icon-button" aria-label="发送">发送</button></div>
    </div>
  </div></main>`)
  for (const width of [280, 360]) {
    await page.locator('.shell').evaluate((element, value) => { (element as HTMLElement).style.width = `${value}px` }, width)
    const textarea = page.locator('.ai-chat-composer textarea')
    const toolbar = page.locator('.ai-chat-toolbar')
    const select = page.locator('.ai-chat-toolbar select')
    const send = page.locator('.ai-chat-toolbar .icon-button')
    const textareaBox = await textarea.boundingBox()
    const toolbarBox = await toolbar.boundingBox()
    const selectBox = await select.boundingBox()
    const sendBox = await send.boundingBox()
    expect(textareaBox?.width).toBeGreaterThan(150)
    expect(textareaBox?.height).toBeGreaterThanOrEqual(56)
    expect(toolbarBox?.y).toBeGreaterThanOrEqual((textareaBox?.y ?? 0) + (textareaBox?.height ?? 0))
    expect(selectBox?.width).toBeGreaterThan(100)
    expect(sendBox?.x).toBeGreaterThan(selectBox?.x ?? 0)
    expect(await page.locator('.ai-chat-suggestions button').evaluate(element => getComputedStyle(element).textDecorationLine)).not.toContain('underline')
  }
})
