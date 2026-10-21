import { createContext, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'

const ReaderActionsContext = createContext<{ target: HTMLDivElement | null; setTarget: (node: HTMLDivElement | null) => void; navigationTarget: HTMLDivElement | null; setNavigationTarget: (node: HTMLDivElement | null) => void }>({ target: null, setTarget: () => {}, navigationTarget: null, setNavigationTarget: () => {} })
const ReaderPlaybackContext = createContext({ playing: false, setPlaying: (_playing: boolean) => {} })
export function useReaderPlayback() { return useContext(ReaderPlaybackContext) }

export function useSyncReaderPlayback(playing: boolean) {
  const { setPlaying } = useReaderPlayback()
  useEffect(() => { setPlaying(playing) }, [playing, setPlaying])
  useEffect(() => () => setPlaying(false), [setPlaying])
}

export function ReaderActionsProvider({ children }: { children: ReactNode }) {
  const [target, setTarget] = useState<HTMLDivElement | null>(null)
  const [navigationTarget, setNavigationTarget] = useState<HTMLDivElement | null>(null)
  const [playing, setPlaying] = useState(false)
  return <ReaderPlaybackContext.Provider value={{ playing, setPlaying }}><ReaderActionsContext.Provider value={{ target, setTarget, navigationTarget, setNavigationTarget }}>{children}</ReaderActionsContext.Provider></ReaderPlaybackContext.Provider>
}

export function ReaderActionsSlot() {
  const { setTarget } = useContext(ReaderActionsContext)
  return <div className="reading-nav-actions" ref={setTarget}/>
}

export function ReaderNavigationSlot() {
  const { setNavigationTarget } = useContext(ReaderActionsContext)
  return <div className="reading-nav-modes" ref={setNavigationTarget}/>
}

export function ReaderNavigation({ children }: { children: ReactNode }) {
  const { navigationTarget } = useContext(ReaderActionsContext)
  return navigationTarget ? createPortal(children, navigationTarget) : children
}

export function ReaderActions({ children }: { children: ReactNode }) {
  const { target } = useContext(ReaderActionsContext)
  return target ? createPortal(children, target) : null
}
