import { createContext, useContext } from 'react'
import type { ComponentType, ReactNode } from 'react'

// Cards show plain text by default. A feature (AI translation) can supply a
// renderer that swaps in translated text, so shared cards never depend on it.
export type CardTextRendererProps = {
  contentId: string
  text: string
  source: 'title' | 'article'
  children: (display: string, original?: string) => ReactNode
}

const PlainText: ComponentType<CardTextRendererProps> = ({ text, children }) => children(text)
const CardTextContext = createContext<ComponentType<CardTextRendererProps>>(PlainText)

export const CardTextProvider = CardTextContext.Provider

export function CardTitle({ contentId, text, as: Heading = 'h2', className }: { contentId: string; text: string; as?: 'h2' | 'h3'; className: string }) {
  const Render = useContext(CardTextContext)
  return <Render contentId={contentId} text={text} source="title">
    {(display, original) => <Heading className={className} title={original} lang={original ? 'zh-Hans' : undefined}>{display}</Heading>}
  </Render>
}

export function CardExcerpt({ contentId, text }: { contentId: string; text: string }) {
  const Render = useContext(CardTextContext)
  const excerpt = Array.from(text).slice(0, 280).join('')
  if (!excerpt) return null
  return <div className="bilingual-excerpt">
    <Render contentId={contentId} text={excerpt} source="article">
      {(display, original) => <p className="card-excerpt" title={original} lang={original ? 'zh-Hans' : undefined}>{display}</p>}
    </Render>
  </div>
}
