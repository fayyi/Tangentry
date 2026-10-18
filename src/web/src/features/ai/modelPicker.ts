export type ModelTask = 'translation' | 'summary' | 'chat'
export type ModelCapability = 'text' | 'vision' | 'reasoning' | 'translation-only'

export interface ModelMetadata { capabilities?: ModelCapability[] }
export interface ModelProviderGroup { provider: string; models: string[] }
export interface ModelProviderStats extends ModelProviderGroup { enabledCount: number }

const TRANSLATION_PREFIX = 'doubao-translate/'
const INDEX_TRANSLATE_MODEL = 'bilibili/index-translate-35b-a3b'

export function getModelProvider(modelId: string): string {
  const slash = modelId.indexOf('/')
  return slash > 0 ? modelId.slice(0, slash) : '其他/自定义'
}

export function groupModelsByProvider(models: string[]): ModelProviderGroup[] {
  const groups = new Map<string, string[]>()
  for (const model of uniqueModels(models)) {
    const provider = getModelProvider(model)
    groups.set(provider, [...(groups.get(provider) ?? []), model])
  }
  return [...groups.entries()].map(([provider, values]) => ({ provider, models: values }))
}

export function getModelProviderStats(models: string[], enabledModels: string[]): ModelProviderStats[] {
  const enabled = new Set(enabledModels)
  return groupModelsByProvider(models).map(group => ({ ...group, enabledCount: group.models.filter(model => enabled.has(model)).length }))
}

export function filterModels(models: string[], query: string): string[] {
  const needle = query.trim().toLocaleLowerCase()
  return uniqueModels(models).filter(model => !needle || model.toLocaleLowerCase().includes(needle))
}

export function sortModelsWithEnabledFirst(models: string[], enabledModels: string[]): string[] {
  const enabled = new Set(enabledModels)
  return uniqueModels(models).sort((left, right) => Number(enabled.has(right)) - Number(enabled.has(left)))
}

export function setModelsEnabled(models: string[], enabledModels: string[], shouldEnable: boolean): string[] {
  const next = new Set(enabledModels)
  for (const model of models) shouldEnable ? next.add(model) : next.delete(model)
  return [...next]
}

export function isTranslationOnlyModel(modelId: string): boolean {
  const normalized = modelId.trim().toLocaleLowerCase()
  return normalized.startsWith(TRANSLATION_PREFIX) || normalized === INDEX_TRANSLATE_MODEL
}

export function getModelCapabilities(modelId: string, metadata?: ModelMetadata): ModelCapability[] {
  if (metadata?.capabilities?.length) return [...new Set(metadata.capabilities)]
  return isTranslationOnlyModel(modelId) ? ['translation-only'] : ['text']
}

export function modelsForTask(models: string[], enabledModels: string[], task: ModelTask): string[] {
  const enabled = new Set(enabledModels)
  return uniqueModels(models).filter(model => enabled.has(model) && (task === 'translation' || !isTranslationOnlyModel(model)))
}

function uniqueModels(models: string[]): string[] {
  return [...new Set(models.map(model => model.trim()).filter(Boolean))]
}
