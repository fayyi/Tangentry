import { Captions, Columns2, Maximize, Minimize, PanelLeftClose, PanelLeftOpen, StickyNote, Sparkles } from 'lucide-react'
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import './readingNotebook.css'

const NotebookContext = createContext<{ showNotes: () => void; showAI: () => void; setAIContent: (content: ReactNode | null) => void } | null>(null)

export function useReadingNotebook() {
  return useContext(NotebookContext)
}

// Both regions stay mounted so changing the mobile view never recreates media.
export function ReadingNotebook({ enabled = true, ai, children }: { enabled?: boolean; ai?: ReactNode; children: ReactNode }) {
  const [view, setView] = useState<'ai' | 'captions' | 'notes'>('captions')
  const [theater, setTheater] = useState(false)
  const [aiCollapsed, setAICollapsed] = useState(false)
  const [registeredAI, setRegisteredAI] = useState<ReactNode | null>(null)
  const showNotes = useCallback(() => { setTheater(false); setView('notes') }, [])
  const showAI = useCallback(() => { setTheater(false); setView('ai') }, [])
  const setAIContent = useCallback((content: ReactNode | null) => setRegisteredAI(() => content), [])
  const context = useMemo(() => ({ showNotes, showAI, setAIContent }), [showAI, showNotes, setAIContent])
  const aiContent = registeredAI ?? ai
  const notebookEnabled = enabled || Boolean(aiContent)
  return <NotebookContext.Provider value={notebookEnabled ? context : null}>
    <div className={`reading-columns${notebookEnabled ? ' reading-notebook' : ''}${aiContent ? ' reading-has-ai' : ''}${aiCollapsed ? ' reading-ai-collapsed' : ''}`} data-notebook-view={view} data-theater={theater}>
      {enabled && <div className="reading-notebook-navigation" role="group" aria-label="阅读区域">
        {aiContent && <button type="button" aria-pressed={view === 'ai'} onClick={showAI}><Sparkles size={16} aria-hidden="true"/>AI</button>}
        <button type="button" aria-pressed={view === 'captions'} onClick={() => setView('captions')}><Captions size={16} aria-hidden="true"/>字幕</button>
        <button type="button" aria-pressed={view === 'notes'} onClick={showNotes}><StickyNote size={16} aria-hidden="true"/>笔记</button>
        <span className="reading-layout-label"><Columns2 size={15} aria-hidden="true"/>字幕与笔记</span>
        <button type="button" className="reading-theater-toggle" aria-pressed={theater} onClick={() => setTheater(value => !value)}>{theater ? <Minimize size={16}/> : <Maximize size={16}/>}<span>{theater ? '退出影院' : '影院模式'}</span></button>
      </div>}
      {aiContent && <aside className="reading-ai" aria-label="AI 大纲与对话"><div className="reading-ai-heading"><strong><Sparkles size={15} aria-hidden="true"/>AI 辅助</strong><button type="button" className="icon-button reading-ai-collapse" aria-label={aiCollapsed ? '展开 AI 面板' : '收起 AI 面板'} title={aiCollapsed ? '展开 AI 面板' : '收起 AI 面板'} onClick={() => setAICollapsed(value => !value)}>{aiCollapsed ? <PanelLeftOpen size={16}/> : <PanelLeftClose size={16}/>}</button></div>{aiContent}</aside>}
      {children}
    </div>
  </NotebookContext.Provider>
}
