import { Captions, Columns2, Maximize, Minimize, StickyNote } from 'lucide-react'
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import './readingNotebook.css'

const NotebookContext = createContext<{ showNotes: () => void } | null>(null)

export function useReadingNotebook() {
  return useContext(NotebookContext)
}

// Both regions stay mounted so changing the mobile view never recreates media.
export function ReadingNotebook({ enabled = true, children }: { enabled?: boolean; children: ReactNode }) {
  const [view, setView] = useState<'captions' | 'notes'>('captions')
  const [theater, setTheater] = useState(false)
  const showNotes = useCallback(() => { setTheater(false); setView('notes') }, [])
  const context = useMemo(() => ({ showNotes }), [showNotes])
  return <NotebookContext.Provider value={enabled ? context : null}>
    <div className={`reading-columns${enabled ? ' reading-notebook' : ''}`} data-notebook-view={view} data-theater={theater}>
      {enabled && <div className="reading-notebook-navigation" role="group" aria-label="阅读区域">
        <button type="button" aria-pressed={view === 'captions'} onClick={() => setView('captions')}><Captions size={16} aria-hidden="true"/>字幕</button>
        <button type="button" aria-pressed={view === 'notes'} onClick={showNotes}><StickyNote size={16} aria-hidden="true"/>笔记</button>
        <span className="reading-layout-label"><Columns2 size={15} aria-hidden="true"/>字幕与笔记</span>
        <button type="button" className="reading-theater-toggle" aria-pressed={theater} onClick={() => setTheater(value => !value)}>{theater ? <Minimize size={16}/> : <Maximize size={16}/>}<span>{theater ? '退出影院' : '影院模式'}</span></button>
      </div>}
      {children}
    </div>
  </NotebookContext.Provider>
}
