import { isTranslationOnlyModel } from './modelPicker'

export type ChatSettings = {
  enabledModels?: string[]
  chatModel?: string
}

/** Only an explicitly configured, enabled chat model is a default. */
export function selectedChatModel(settings: ChatSettings): string {
  const enabled = settings.enabledModels ?? []
  const configured = settings.chatModel?.trim() ?? ''
  return configured && enabled.includes(configured) && !isTranslationOnlyModel(configured) ? configured : ''
}

export function rememberedChatModel(enabledModels: string[], storage: Storage | undefined = typeof window === 'undefined' ? undefined : window.localStorage): string {
  if (!storage) return ''
  const remembered = storage.getItem('tangentry.ai.chat-model')?.trim() ?? ''
  return remembered && enabledModels.includes(remembered) && !isTranslationOnlyModel(remembered) ? remembered : ''
}

export function rememberChatModel(model: string, storage: Storage | undefined = typeof window === 'undefined' ? undefined : window.localStorage): void {
  if (!storage) return
  if (model.trim()) storage.setItem('tangentry.ai.chat-model', model)
  else storage.removeItem('tangentry.ai.chat-model')
}
