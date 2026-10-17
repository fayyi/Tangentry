export interface AISettings { protocol: 'openai' | 'compatible'; baseUrl: string; model: string; chatModel?: string; enabledModels?: string[]; availableModels?: string[]; summaryModel?: string; summaryPrompt?: string; summaryContextBudget?: number; summaryReady?: boolean; hasKey: boolean; ready: boolean; translating: boolean; revision: number }
export interface TranslationResult { key: string; contentId: string; language: string; baseUrl: string; model: string; total: number; texts: string[]; updatedAt: number }
export interface TranslationInspection { settings: AISettings; result: TranslationResult; active: boolean }
export type AITask = 'translation' | 'summary'
export interface AICheckResult { task: AITask; model: string; elapsedMs: number; revision: number }
