export interface SummaryPoint { text: string; indices: number[] }
export interface SummaryOutline { title: string; level: number; indices?: number[]; start?: number; end?: number; page?: number; anchor?: string; children?: SummaryOutline[] }
export interface SummaryCitation { index: number; start?: number; end?: number; page?: number; quote: string }
export interface SummaryResult {
  key: string; contentId: string; source: string; baseUrl: string; model: string
  totalCalls: number; completedCalls: number; complete: boolean
  overview?: string; points?: SummaryPoint[]; citations?: SummaryCitation[]; outline?: SummaryOutline[]; updatedAt: number
}
export interface SummaryInspection { settings: AISettings; result: SummaryResult; active: boolean }
import type { AISettings } from './types'
