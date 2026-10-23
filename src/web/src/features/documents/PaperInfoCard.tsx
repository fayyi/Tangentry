import { Button } from '../../components/ui'
import { ExternalLink, FileText } from 'lucide-react'
import { useState } from 'react'
import type { PaperMetadata } from '../../shared/api/types'
import './paper.css'

const date = (value?: number) => value ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(new Date(value * 1000)) : ''

export function PaperInfoCard({ paper }: { paper: PaperMetadata }) {
  const [abstractOpen, setAbstractOpen] = useState(false)
  const authors = paper.authors.join('、')
  return <section className="paper-info" aria-label="论文信息">
    <div className="paper-info-heading"><FileText size={17} aria-hidden="true"/><span>arXiv 论文</span><a href={paper.arxivUrl} target="_blank" rel="noopener noreferrer">arXiv <ExternalLink size={13} aria-hidden="true"/></a></div>
    <p className="paper-info-authors">{authors || '作者信息暂缺'}</p>
    <div className="paper-info-details">
      {paper.categories.length > 0 && <span>{paper.categories.join(' · ')}</span>}
      <span>arXiv:{paper.arxivId}{paper.version ? `v${paper.version}` : ''}</span>
      {date(paper.publishedAt) && <span>发布于 {date(paper.publishedAt)}</span>}
      {date(paper.updatedAt) && paper.updatedAt !== paper.publishedAt && <span>更新于 {date(paper.updatedAt)}</span>}
    </div>
    <div className="paper-info-links">
      {paper.hfUrl && <a href={paper.hfUrl} target="_blank" rel="noopener noreferrer">Hugging Face <ExternalLink size={13} aria-hidden="true"/></a>}
      {paper.githubUrl && <a href={paper.githubUrl} target="_blank" rel="noopener noreferrer">GitHub <ExternalLink size={13} aria-hidden="true"/></a>}
      <Button variant="quiet" type="button" className="text-button" aria-expanded={abstractOpen} onClick={() => setAbstractOpen(value => !value)}>{abstractOpen ? '收起摘要' : '查看摘要'}</Button>
    </div>
    {abstractOpen && <p className="paper-info-abstract">{paper.abstract || '暂无摘要。'}</p>}
  </section>
}
