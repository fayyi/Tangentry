import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './app/App'
import { CardTranslation } from './features/ai/TextTranslation'
import { CardTextProvider } from './shared/ui/CardText'
import './app/styles.css'

createRoot(document.getElementById('root')!).render(<StrictMode><CardTextProvider value={CardTranslation}><App /></CardTextProvider></StrictMode>)
