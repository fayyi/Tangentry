import { Bookmark, RotateCcw } from 'lucide-react'
import { inboxPageSizes, useInboxPageSize } from './useInboxPageSize'
import { cardWidthRange, defaultCardAppearance, useCardAppearance } from './useCardAppearance'
import type { CardAppearance, CardCorners, CardDensity, CardTitleSize } from './useCardAppearance'

function Choice<T extends string>({ name, legend, hint, value, options, onChange }: { name: string; legend: string; hint: string; value: T; options: readonly (readonly [T, string])[]; onChange: (value: T) => void }) {
  return <fieldset className="layout-preference inbox-size-preference card-preference" aria-label={legend}><legend>{legend}<small>{hint}</small></legend><div className="layout-options">
    {options.map(([option, label]) => <label key={option}><input type="radio" name={name} value={option} checked={value === option} onChange={() => onChange(option)} /><span>{label}</span></label>)}
  </div></fieldset>
}

const samples = [
  { title: '如何用笔记把一段视频真正看懂', excerpt: '截屏、关联时间、写下当时的想法，回看时就能顺着时间轴重新走一遍思考过程。', meta: 'B站 · 12:40', media: true },
  { title: '黑白灰：耐看的界面从减法开始', excerpt: '颜色越少，层级越要靠字重、留白和对齐来表达。', meta: 'RSS · 图文', media: false },
  { title: '播客精选 · 第 42 期', excerpt: '关于专注与好奇心的一次长谈。', meta: '小宇宙 · 58 分钟', media: true },
]

export function GalleryLayoutSettings() {
  const { pageSize, changePageSize } = useInboxPageSize()
  const { appearance, change, reset } = useCardAppearance()
  const isDefault = (Object.keys(defaultCardAppearance) as (keyof CardAppearance)[]).every(name => appearance[name] === defaultCardAppearance[name])
  return <section className="settings-section" aria-labelledby="gallery-layout-title">
    <div className="settings-heading"><div><h2 id="gallery-layout-title">显示</h2><p className="field-hint">调整内容卡片的大小、密度和每页条数，发现、收藏、订阅等页面同步生效。</p></div><button type="button" className="button button-secondary" disabled={isDefault} onClick={reset}><RotateCcw size={14}/>恢复默认</button></div>
    <div className="card-appearance-preview" aria-label="卡片预览" role="img">
      <div className="card-preview-grid">{samples.map(sample => <div key={sample.title} className="modern-card card-preview">
        {sample.media && <div className="modern-card-thumbnail card-preview-media" />}
        <div className="modern-card-content"><h3 className="modern-card-title">{sample.title}</h3><p className="card-excerpt">{sample.excerpt}</p><div className="card-preview-meta"><span>{sample.meta}</span><Bookmark size={15}/></div></div>
      </div>)}</div>
    </div>
    <fieldset className="layout-preference inbox-size-preference card-preference" aria-label="卡片宽度"><legend>卡片宽度<small>越宽每行越少；窗口较窄时会自动收拢</small></legend><div className="card-width-control">
      <span aria-hidden="true">小</span><input type="range" aria-label="卡片宽度" min={cardWidthRange.min} max={cardWidthRange.max} step={cardWidthRange.step} value={appearance.width} onChange={event => change({ width: event.target.valueAsNumber })} /><span aria-hidden="true">大</span><output>{appearance.width} px</output>
    </div></fieldset>
    <Choice<CardDensity> name="card-density" legend="卡片间距" hint="卡片之间与卡片内部的留白" value={appearance.density} options={[['compact', '紧凑'], ['comfortable', '标准'], ['spacious', '宽松']]} onChange={density => change({ density })}/>
    <Choice<CardTitleSize> name="card-title-size" legend="标题字号" hint="卡片标题的大小" value={appearance.titleSize} options={[['small', '小'], ['medium', '中'], ['large', '大']]} onChange={titleSize => change({ titleSize })}/>
    <Choice<CardCorners> name="card-corners" legend="卡片圆角" hint="卡片和封面的边角" value={appearance.corners} options={[['square', '直角'], ['soft', '柔和'], ['round', '圆润']]} onChange={corners => change({ corners })}/>
    <fieldset className="layout-preference inbox-size-preference card-preference" aria-label="卡片摘要"><legend>卡片摘要<small>关闭后只显示标题与来源，更适合快速浏览</small></legend><label className="card-switch"><input type="checkbox" role="switch" checked={appearance.excerpt} onChange={event => change({ excerpt: event.target.checked })}/><span>{appearance.excerpt ? '显示' : '隐藏'}</span></label></fieldset>
    <fieldset className="layout-preference inbox-size-preference" aria-label="每页条数"><legend>每页条数<small>历史收件箱每页显示的内容数量</small></legend><div className="layout-options">
      {inboxPageSizes.map(value => <label key={value}><input type="radio" name="inbox-page-size" value={value} checked={pageSize === value} onChange={() => changePageSize(value)} /><span>{value} 条</span></label>)}
    </div></fieldset>
    <p className="field-hint settings-autosave"><span className="settings-state-dot" aria-hidden="true"/>更改自动保存</p>
  </section>
}
