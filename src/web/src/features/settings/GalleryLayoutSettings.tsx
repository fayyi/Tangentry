import { useState, type CSSProperties } from 'react'
import { RotateCcw } from 'lucide-react'
import { Button, Checkbox } from '../../components/ui'
import { ModernGalleryCard } from '../../shared/ui/ModernGalleryCard'
import type { ContentItem } from '../../shared/api/types'
import { cardCoverHeightRange, cardWidthRange, defaultCardAppearance, useCardAppearance } from './useCardAppearance'
import type { CardAppearance, CardCorners, CardDensity, CardTitleSize } from './useCardAppearance'

const samples: ContentItem[] = [
  { id: 'preview:video', kind: 'video', platform: 'bilibili', platformId: '', title: '如何用笔记把一段视频真正看懂', description: '截屏、关联时间、写下当时的想法，回看时顺着时间轴重新走一遍思考过程。', author: '好奇心研究所', authorId: '', url: '', thumbnailUrl: '', durationSeconds: 760, publishedAt: 0 },
  { id: 'preview:article', kind: 'article', platform: 'rss', platformId: '', title: '黑白灰：耐看的界面从减法开始', description: '颜色越少，层级越要靠字重、留白和对齐来表达。给重要的内容留出空间。', author: '阅读与设计', authorId: '', url: '', thumbnailUrl: '', durationSeconds: 0, publishedAt: 0 },
  { id: 'preview:audio', kind: 'audio', platform: 'xiaoyuzhou', platformId: '', title: '关于专注与好奇心的一次长谈', description: '播客精选 · 第 42 期', author: '慢慢听', authorId: '', url: '', thumbnailUrl: '', durationSeconds: 3480, publishedAt: 0 },
]
const densityOptions: readonly (readonly [CardDensity, string])[] = [['compact', '紧凑'], ['comfortable', '标准'], ['spacious', '宽松']]
const titleOptions: readonly (readonly [CardTitleSize, string])[] = [['small', '小'], ['medium', '中'], ['large', '大']]
const cornerOptions: readonly (readonly [CardCorners, string])[] = [['square', '直角'], ['soft', '柔和'], ['round', '圆润']]
const noop = () => {}

export function GalleryLayoutSettings() {
  const { appearance, change, reset } = useCardAppearance()
  const [sampleIndex, setSampleIndex] = useState(0)
  const isDefault = (Object.keys(defaultCardAppearance) as (keyof CardAppearance)[]).every(name => appearance[name] === defaultCardAppearance[name])
  return <div className="settings-appearance-panel">
    <div className="settings-panel-label"><div><h3>内容卡片</h3><p>宽度与封面高度独立设置，窗口变宽时封面保持相同高度。</p></div><Button variant="quiet" type="button" className="text-button" disabled={isDefault} onClick={reset}><RotateCcw size={14}/>恢复默认</Button></div>
    <div className="settings-appearance-grid">
      <div className="settings-preview-canvas" aria-label="内容卡片实时预览">
        <div className="settings-preview-canvas-head"><span><i/>实际尺寸预览</span><output>{appearance.width} × {appearance.coverHeight}px 封面</output></div>
        <div className="settings-choice-row" role="group" aria-label="预览内容类型">{['视频', '文章', '播客'].map((label, index) => <Button variant="quiet" type="button" key={label} aria-pressed={sampleIndex === index} className={sampleIndex === index ? 'active' : ''} onClick={() => setSampleIndex(index)}>{label}</Button>)}</div>
        <div className="settings-preview-stage"><div className="settings-preview-actual" style={{ width: `${appearance.width}px` } as CSSProperties}><ModernGalleryCard item={samples[sampleIndex]} saved={false} pending={false} onRead={noop} onSave={noop}/></div></div>
        <p className="settings-preview-note">与内容列表使用同一卡片。空间不足时宽度适应窗口，封面高度保持固定。</p>
      </div>
      <div className="settings-control-stack" aria-label="内容卡片设置">
        <label className="settings-range-field"><span>卡片宽度 <output>{appearance.width}px</output></span><input aria-label="卡片宽度" type="range" min={cardWidthRange.min} max={cardWidthRange.max} step={cardWidthRange.step} value={appearance.width} onChange={event => change({ width: event.currentTarget.valueAsNumber })}/></label>
        <label className="settings-range-field"><span>封面高度 <output>{appearance.coverHeight}px</output></span><input aria-label="封面高度" type="range" min={cardCoverHeightRange.min} max={cardCoverHeightRange.max} step={cardCoverHeightRange.step} value={appearance.coverHeight} onChange={event => change({ coverHeight: event.currentTarget.valueAsNumber })}/></label>
        <fieldset><legend>浏览密度</legend><div className="settings-choice-row">{densityOptions.map(([density, label]) => <Button variant="quiet" key={density} type="button" className={appearance.density === density ? 'active' : ''} aria-pressed={appearance.density === density} onClick={() => change({ density })}>{label}</Button>)}</div></fieldset>
        <fieldset><legend>圆角</legend><div className="settings-choice-row">{cornerOptions.map(([corners, label]) => <Button variant="quiet" key={corners} type="button" className={appearance.corners === corners ? 'active' : ''} aria-pressed={appearance.corners === corners} onClick={() => change({ corners })}>{label}</Button>)}</div></fieldset>
        <fieldset><legend>标题大小</legend><div className="settings-choice-row">{titleOptions.map(([titleSize, label]) => <Button variant="quiet" key={titleSize} type="button" className={appearance.titleSize === titleSize ? 'active' : ''} aria-pressed={appearance.titleSize === titleSize} onClick={() => change({ titleSize })}>{label}</Button>)}</div></fieldset>
        <label className="settings-toggle-field"><span><strong>显示摘要</strong><small>在卡片中保留上下文</small></span><Checkbox role="switch" checked={appearance.excerpt} onChange={event => change({ excerpt: event.currentTarget.checked })}/></label>
      </div>
    </div>
  </div>
}
