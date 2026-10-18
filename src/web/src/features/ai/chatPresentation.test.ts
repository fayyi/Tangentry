import { describe, expect, it } from 'vitest'
import { rememberedChatModel, rememberChatModel, selectedChatModel } from './chatPresentation'

describe('chat model presentation', () => {
  it('uses only an explicitly configured enabled model', () => {
    expect(selectedChatModel({ chatModel: 'general-chat', enabledModels: ['translation', 'general-chat'] })).toBe('general-chat')
    expect(selectedChatModel({ enabledModels: ['general-chat'] })).toBe('')
    expect(selectedChatModel({ chatModel: 'not-enabled', enabledModels: ['general-chat'] })).toBe('')
    expect(selectedChatModel({ enabledModels: ['bilibili/Index-Translate-35B-A3B'] })).toBe('')
  })

  it('stores and clears a manually selected model', () => {
    const storage = new Map<string, string>()
    const adapter: Storage = {
      get length() { return storage.size },
      clear: () => storage.clear(),
      getItem: key => storage.get(key) ?? null,
      key: index => [...storage.keys()][index] ?? null,
      removeItem: key => { storage.delete(key) },
      setItem: (key, value) => { storage.set(key, value) },
    }
    rememberChatModel('general-chat', adapter)
    expect(adapter.getItem('tangentry.ai.chat-model')).toBe('general-chat')
    expect(rememberedChatModel(['general-chat'], adapter)).toBe('general-chat')
    expect(rememberedChatModel(['translation'], adapter)).toBe('')
    rememberChatModel('', adapter)
    expect(adapter.getItem('tangentry.ai.chat-model')).toBeNull()
  })

  it('prefers the configured model over a remembered selection', () => {
    expect(selectedChatModel({ chatModel: 'general-chat', enabledModels: ['general-chat', 'other-chat'] })).toBe('general-chat')
  })
})
