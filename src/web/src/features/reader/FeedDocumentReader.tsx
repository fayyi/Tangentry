import { Button } from '../../components/ui'
import { useEffect, useState } from 'react'
import { request } from '../../shared/api/client'
import type { Feed } from '../../shared/api/types'
import type { PDFAnnotationProps } from '../documents/PDFReader'
import { PDFReader } from '../documents/PDFReader'

// The RSS identity belongs to the episode; the document ID belongs to its bytes.
// Fetch only after an explicit reader action, never while browsing the gallery.
export function FeedDocumentReader({ url, annotations }: { url: string; annotations?: PDFAnnotationProps }) {
  const [documentId, setDocumentId] = useState('')
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setDocumentId(''); setError('')
    void request<Feed>('/discovery/pdf?q=' + encodeURIComponent(url), { signal: controller.signal })
      .then(feed => {
        if (controller.signal.aborted) return
        const item = feed.items.find(item => item.platform === 'pdf' && item.kind === 'pdf')
        if (!item) throw new Error('来源没有提供可读取的 PDF。')
        setDocumentId(item.platformId)
      })
      .catch(cause => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'PDF 暂时无法读取。') })
    return () => controller.abort()
  }, [url, attempt])
  if (error) return <div className="error-panel" role="alert"><p>{error}</p><Button variant="secondary" className="button button-secondary" onClick={() => setAttempt(value => value + 1)}>重新读取 PDF</Button></div>
  return documentId ? <PDFReader id={documentId} {...annotations} /> : <p role="status" className="field-hint">正在获取订阅中的 PDF…</p>
}
