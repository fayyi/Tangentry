import { Bookmark, RotateCcw } from 'lucide-react'
import { cardWidthRange, defaultCardAppearance, useCardAppearance } from './useCardAppearance'
import type { CardAppearance, CardCorners } from './useCardAppearance'

const samples = [
  { title: '如何用笔记把一段视频真正看懂', excerpt: '截屏、关联时间、写下当时的想法，回看时就能顺着时间轴重新走一遍思考过程。', meta: 'B站 · 12:40', media: true },
  { title: '黑白灰：耐看的界面从减法开始', excerpt: '颜色越少，层级越要靠字重、留白和对齐来表达。', meta: 'RSS · 图文', media: false },
  { title: '播客精选 · 第 42 期', excerpt: '关于专注与好奇心的一次长谈。', meta: '小宇宙 · 58 分钟', media: true },
]

function CornersPicker({ value, onChange }: { value: CardCorners; onChange: (v: CardCorners) => void }) {
  return <div className="corners-picker">
    {(['square', 'soft', 'round'] as const).map(c => <button key={c} type="button" className={`corners-option ${value === c ? 'active' : ''}`} onClick={() => onChange(c)} aria-label={c} style={{ borderRadius: c === 'square' ? '3px' : c === 'soft' ? '12px' : '20px' }}><div className="corners-sample" /></button>)}
  </div>
}

export function GalleryLayoutSettings() {
  const { appearance, change, reset } = useCardAppearance()
  const isDefault = (Object.keys(defaultCardAppearance) as (keyof CardAppearance)[]).every(name => appearance[name] === defaultCardAppearance[name])

  return <section className="settings-section gallery-layout-section" aria-labelledby="gallery-layout-title">
    <div className="gallery-layout-header">
      <div><h2 id="gallery-layout-title">内容卡片</h2><p className="field-hint">调整发现、收藏和订阅页面卡片的显示方式。</p></div>
      <button type="button" className="button button-secondary" disabled={isDefault} onClick={reset}><RotateCcw size={14}/></button>
    </div>
    <div className="gallery-layout-main">
      <div className="gallery-preview"><div className="card-appearance-preview" aria-label="卡片预览"><div className="card-preview-grid">{samples.map(sample => <div key={sample.title} className="modern-card card-preview">{sample.media && <div className="modern-card-thumbnail card-preview-media" />}<div className="modern-card-content"><h3 className="modern-card-title">{sample.title}</h3>{appearance.excerpt && <p className="card-excerpt">{sample.excerpt}</p>}<div className="card-preview-meta"><span>{sample.meta}</span><Bookmark size={15}/></div></div></div>)}</div></div></div>
      <div className="gallery-controls">
        <div className="control-group">
          <label>间距</label>
          <div className="control-options">{(['compact', 'comfortable', 'spacious'] as const).map(d => <button key={d} type="button" className={`option-btn ${appearance.density === d ? 'active' : ''}`} onClick={() => change({ density: d })}><span>{d === 'compact' ? '紧凑' : d === 'comfortable' ? '标准' : '宽松'}</span></button>)}</div>
        </div>
        <div className="control-group">
          <label>宽度</label>
          <div className="width-slider"><input type="range" min={cardWidthRange.min} max={cardWidthRange.max} step={cardWidthRange.step} value={appearance.width} onChange={e => change({ width: e.currentTarget.valueAsNumber })} /><output>{appearance.width}px</output></div>
        </div>
        <div className="control-group">
          <label>圆角</label>
          <CornersPicker value={appearance.corners} onChange={c => change({ corners: c })} />
        </div>
        <div className="control-group">
          <label>摘要</label>
          <label className="toggle-switch"><input type="checkbox" checked={appearance.excerpt} onChange={e => change({ excerpt: e.currentTarget.checked })} /><span>{appearance.excerpt ? '显示' : '隐藏'}</span></label>
        </div>
      </div>
    </div>
  </section>
}
