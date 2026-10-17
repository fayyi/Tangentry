import { Captions, Columns2, Maximize, Minimize, PanelLeftClose, PanelLeftOpen, StickyNote, Sparkles } from 'lucide-react'
import { Children, createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent, type ReactElement, type ReactNode } from 'react'
import { adjustLayoutWidth, DEFAULT_READER_LAYOUT, readReaderLayout, READER_LAYOUT_LIMITS, writeReaderLayout, type ReaderLayout } from './readerLayout'
import './readingNotebook.css'

const NotebookContext = createContext<{ showNotes: () => void; showAI: () => void; setAIContent: (content: ReactNode | null) => void; playerWidth: number; setPlayerWidth: (value: number) => void; resetPlayerWidth: () => void } | null>(null)

export function useReadingNotebook() {
  return useContext(NotebookContext)
}

function ResizeHandle({ label, side, value, limits, onChange, onReset }: { label: string; side: 'ai' | 'notes'; value: number; limits: readonly [number, number]; onChange: (value: number) => void; onReset: () => void }) {
  const start = useRef<{ x: number; value: number } | null>(null)
  const direction = side === 'notes' ? -1 : 1
  const move = (event: PointerEvent) => { if (start.current) onChange(adjustLayoutWidth(start.current.value, (event.clientX - start.current.x) * direction, limits)) }
  const stop = () => { start.current = null; window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop) }
  const begin = (event: ReactPointerEvent<HTMLDivElement>) => { event.preventDefault(); start.current = { x: event.clientX, value }; event.currentTarget.setPointerCapture?.(event.pointerId); window.addEventListener('pointermove', move); window.addEventListener('pointerup', stop, { once: true }) }
  return <div className={`reading-resize-handle reading-resize-${side}`} role="separator" tabIndex={0} aria-label={label} aria-valuemin={limits[0]} aria-valuemax={limits[1]} aria-valuenow={Math.round(value)} onPointerDown={begin} onDoubleClick={onReset} onKeyDown={event => {
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); onChange(adjustLayoutWidth(value, (event.key === 'ArrowRight' ? 16 : -16) * direction, limits)) }
    if (event.key === 'Home') { event.preventDefault(); onChange(limits[0]) }
    if (event.key === 'End') { event.preventDefault(); onChange(limits[1]) }
  }} />
}

export function ReaderPlayerSizeHandle({ value, onChange, onReset }: { value: number; onChange: (value: number) => void; onReset: () => void }) {
  const start = useRef<{ x: number; value: number; width: number } | null>(null)
  const move = (event: PointerEvent) => { if (start.current) onChange(Math.min(100, Math.max(45, start.current.value + ((event.clientX - start.current.x) / start.current.width) * 100))) }
  const stop = () => { start.current = null; window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop) }
  return <div className="reader-player-resize-handle" role="separator" tabIndex={0} aria-label="调整播放器宽度" aria-valuemin={45} aria-valuemax={100} aria-valuenow={Math.round(value)} onPointerDown={event => { event.preventDefault(); const width = event.currentTarget.parentElement?.parentElement?.getBoundingClientRect().width || 1; start.current = { x: event.clientX, value, width }; window.addEventListener('pointermove', move); window.addEventListener('pointerup', stop, { once: true }) }} onDoubleClick={onReset} onKeyDown={event => { if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); onChange(Math.min(100, Math.max(45, value + (event.key === 'ArrowRight' ? 5 : -5)))) } if (event.key === 'Home') { event.preventDefault(); onChange(45) } if (event.key === 'End') { event.preventDefault(); onChange(100) } }} />
}

// Both regions stay mounted so changing the mobile view never recreates media.
export function ReadingNotebook({ enabled = true, ai, children }: { enabled?: boolean; ai?: ReactNode; children: ReactNode }) {
  const [view, setView] = useState<'ai' | 'captions' | 'notes'>('captions')
  const [theater, setTheater] = useState(false)
  const [aiCollapsed, setAICollapsed] = useState(false)
  const [registeredAI, setRegisteredAI] = useState<ReactNode | null>(null)
  const [layout, setLayout] = useState<ReaderLayout>(() => readReaderLayout())
  const update = useCallback((key: keyof ReaderLayout, value: number) => setLayout(current => ({ ...current, [key]: value })), [])
  const reset = useCallback((key: keyof ReaderLayout) => setLayout(current => ({ ...current, [key]: DEFAULT_READER_LAYOUT[key] })), [])
  const showNotes = useCallback(() => { setTheater(false); setView('notes') }, [])
  const showAI = useCallback(() => { setTheater(false); setView('ai') }, [])
  const setAIContent = useCallback((content: ReactNode | null) => setRegisteredAI(() => content), [])
  const context = useMemo(() => ({ showNotes, showAI, setAIContent, playerWidth: layout.player, setPlayerWidth: (value: number) => update('player', value), resetPlayerWidth: () => reset('player') }), [showAI, showNotes, setAIContent, layout.player, update, reset])
  const aiContent = registeredAI ?? ai
  const notebookEnabled = enabled || Boolean(aiContent)
  useEffect(() => { writeReaderLayout(layout) }, [layout])
  const content = Children.toArray(children)
  const className = (child: ReactNode) => (child as ReactElement<{ className?: string }>)?.props?.className ?? ''
  const primary = content.filter(child => !className(child).includes('reading-notes'))
  const notes = content.filter(child => className(child).includes('reading-notes'))
  const hasNotes = notes.length > 0
  return <NotebookContext.Provider value={notebookEnabled ? context : null}>
    <div className={`reading-columns${notebookEnabled ? ' reading-notebook' : ''}${aiContent ? ' reading-has-ai' : ''}${aiCollapsed ? ' reading-ai-collapsed' : ''}`} data-notebook-view={view} data-theater={theater} style={{ '--reading-ai-width': `${layout.ai}px`, '--reading-notes-width': `${layout.notes}px` } as CSSProperties}>
      {enabled && <div className="reading-notebook-navigation" role="group" aria-label="阅读区域">
        {aiContent && <button type="button" aria-pressed={view === 'ai'} onClick={showAI}><Sparkles size={16} aria-hidden="true"/>AI</button>}
        <button type="button" aria-pressed={view === 'captions'} onClick={() => setView('captions')}><Captions size={16} aria-hidden="true"/>字幕</button>
        <button type="button" aria-pressed={view === 'notes'} onClick={showNotes}><StickyNote size={16} aria-hidden="true"/>笔记</button>
        <span className="reading-layout-label"><Columns2 size={15} aria-hidden="true"/>字幕与笔记</span>
        <button type="button" className="reading-theater-toggle" aria-pressed={theater} onClick={() => setTheater(value => !value)}>{theater ? <Minimize size={16}/> : <Maximize size={16}/>}<span>{theater ? '退出影院' : '影院模式'}</span></button>
      </div>}
      {aiContent && <><aside className="reading-ai" aria-label="AI 大纲与对话"><div className="reading-ai-heading"><strong><Sparkles size={15} aria-hidden="true"/>AI 辅助</strong><button type="button" className="icon-button reading-ai-collapse" aria-label={aiCollapsed ? '展开 AI 面板' : '收起 AI 面板'} title={aiCollapsed ? '展开 AI 面板' : '收起 AI 面板'} onClick={() => setAICollapsed(value => !value)}>{aiCollapsed ? <PanelLeftOpen size={16}/> : <PanelLeftClose size={16}/>}</button></div>{aiContent}</aside><ResizeHandle side="ai" label="调整 AI 辅助栏宽度" value={layout.ai} limits={READER_LAYOUT_LIMITS.ai} onChange={value => update('ai', value)} onReset={() => reset('ai')}/></>}
      {primary}
      {hasNotes && <ResizeHandle side="notes" label="调整我的笔记栏宽度" value={layout.notes} limits={READER_LAYOUT_LIMITS.notes} onChange={value => update('notes', value)} onReset={() => reset('notes')}/>} {notes}
    </div>
  </NotebookContext.Provider>
}
