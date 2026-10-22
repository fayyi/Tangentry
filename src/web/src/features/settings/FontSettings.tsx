import { RotateCcw } from 'lucide-react'
import { defaultFontSettings, useFontSettings, type FontSettings as FontSettingsValue, type ReadingFont, type ReadingLineHeight, type ReadingSize, type UIFont } from './useFontSettings'

const uiOptions: readonly (readonly [UIFont, string])[] = [['system', '系统'], ['inter', 'Inter'], ['noto', '思源黑体']]
const readingOptions: readonly (readonly [ReadingFont, string])[] = [['system', '系统'], ['wenkai', '霞鹜文楷'], ['noto', '思源黑体']]
const sizeOptions: readonly (readonly [ReadingSize, string])[] = [['small', '小'], ['medium', '中'], ['large', '大']]
const lineHeightOptions: readonly (readonly [ReadingLineHeight, string])[] = [['compact', '紧'], ['comfortable', '中'], ['relaxed', '宽']]

export function FontSettings() {
  const { settings, change, reset } = useFontSettings()
  const isDefault = (Object.keys(defaultFontSettings) as (keyof FontSettingsValue)[]).every(name => settings[name] === defaultFontSettings[name])

  return <section className="settings-section font-settings-section" aria-labelledby="font-settings-title">
    <div className="font-settings-header">
      <div><h2 id="font-settings-title">字体与排版</h2><p className="field-hint">为阅读体验选择字体和排版方式。</p></div>
      <button type="button" className="button button-secondary" disabled={isDefault} onClick={reset}><RotateCcw size={14}/></button>
    </div>
    <div className="font-settings-preview" aria-label="字体预览">
      <p className="font-preview-ui">Tangentry — 阅读优先</p>
      <p className="font-preview-reading">把想法写下来，隔一段时间再回来阅读。</p>
    </div>
    <div className="font-settings-controls">
      <div className="font-control"><label>界面</label><div className="font-options">{uiOptions.map(([v, l]) => <button key={v} type="button" className={`font-btn ${settings.ui === v ? 'active' : ''}`} onClick={() => change({ ui: v })}>{l}</button>)}</div></div>
      <div className="font-control"><label>正文</label><div className="font-options">{readingOptions.map(([v, l]) => <button key={v} type="button" className={`font-btn ${settings.reading === v ? 'active' : ''}`} onClick={() => change({ reading: v })}>{l}</button>)}</div></div>
      <div className="font-control"><label>字号</label><div className="font-options">{sizeOptions.map(([v, l]) => <button key={v} type="button" className={`font-btn ${settings.size === v ? 'active' : ''}`} onClick={() => change({ size: v })}>{l}</button>)}</div></div>
      <div className="font-control"><label>行高</label><div className="font-options">{lineHeightOptions.map(([v, l]) => <button key={v} type="button" className={`font-btn ${settings.lineHeight === v ? 'active' : ''}`} onClick={() => change({ lineHeight: v })}>{l}</button>)}</div></div>
    </div>
  </section>
}
