import { RotateCcw } from 'lucide-react'
import { defaultFontSettings, useFontSettings, type FontSettings as FontSettingsValue, type ReadingFont, type ReadingLineHeight, type ReadingSize, type UIFont } from './useFontSettings'

const uiOptions: readonly (readonly [UIFont, string])[] = [['system', '系统'], ['inter', 'Inter'], ['noto', '思源黑体']]
const readingOptions: readonly (readonly [ReadingFont, string])[] = [['system', '系统'], ['wenkai', '霞鹜文楷'], ['noto', '思源黑体']]
const sizeOptions: readonly (readonly [ReadingSize, string])[] = [['small', '小'], ['medium', '中'], ['large', '大']]
const lineHeightOptions: readonly (readonly [ReadingLineHeight, string])[] = [['compact', '紧'], ['comfortable', '中'], ['relaxed', '宽']]

function FontChoice<T extends string>({ label, options, value, onChange }: { label: string; options: readonly (readonly [T, string])[]; value: T; onChange: (value: T) => void }) {
  return <fieldset><legend>{label}</legend><div className="settings-choice-row" role="group" aria-label={label}>{options.map(([option, optionLabel]) => <button key={option} type="button" className={value === option ? 'active' : ''} aria-pressed={value === option} onClick={() => onChange(option)}>{optionLabel}</button>)}</div></fieldset>
}

export function FontSettings() {
  const { settings, change, reset } = useFontSettings()
  const isDefault = (Object.keys(defaultFontSettings) as (keyof FontSettingsValue)[]).every(name => settings[name] === defaultFontSettings[name])
  return <div className="settings-type-panel">
    <div className="settings-panel-label"><div><span>阅读画布</span><h3>字体与排版</h3><p>只影响文章、笔记和字幕正文。</p></div><button type="button" className="text-button" disabled={isDefault} onClick={reset}><RotateCcw size={14}/>恢复默认</button></div>
    <div className="settings-type-grid">
      <div className="settings-reading-canvas" aria-label="阅读排版预览"><span>READING PREVIEW</span><h3>把收藏变成真正的理解</h3><p>好的阅读不是把更多内容装进稍后处理，而是给重要的想法留出再次相遇的时间。你可以在一段文字旁边写下当时的疑问，过几天回来看时，它们会自然地连成一条线。</p><p>Tangentry 把这些片段安静地放在一起，让 <em>context</em> 和语境同时保留下来。</p><footer>{settings.reading} · {settings.size} · {settings.lineHeight}</footer></div>
      <div className="settings-control-stack"><FontChoice label="界面字体" options={uiOptions} value={settings.ui} onChange={ui => change({ ui })}/><FontChoice label="阅读字体" options={readingOptions} value={settings.reading} onChange={reading => change({ reading })}/><FontChoice label="字号" options={sizeOptions} value={settings.size} onChange={size => change({ size })}/><FontChoice label="行高" options={lineHeightOptions} value={settings.lineHeight} onChange={lineHeight => change({ lineHeight })}/></div>
    </div>
  </div>
}
