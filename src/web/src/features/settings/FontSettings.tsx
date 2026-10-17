import { RotateCcw } from 'lucide-react'
import { defaultFontSettings, useFontSettings, type FontSettings as FontSettingsValue, type ReadingFont, type ReadingLineHeight, type ReadingSize, type UIFont } from './useFontSettings'

function Choice<T extends string>({ name, legend, hint, value, options, onChange }: { name: string; legend: string; hint: string; value: T; options: readonly (readonly [T, string])[]; onChange: (value: T) => void }) {
  return <fieldset className="layout-preference inbox-size-preference font-preference" aria-label={legend}><legend>{legend}<small>{hint}</small></legend><div className="layout-options">
    {options.map(([option, label]) => <label key={option}><input type="radio" name={name} value={option} checked={value === option} onChange={() => onChange(option)} /><span>{label}</span></label>)}
  </div></fieldset>
}

const uiOptions: readonly (readonly [UIFont, string])[] = [['system', '系统默认'], ['inter', 'Inter + 系统中文'], ['noto', '思源黑体']]
const readingOptions: readonly (readonly [ReadingFont, string])[] = [['system', '系统默认'], ['wenkai', '霞鹜文楷'], ['noto', '思源黑体']]
const sizeOptions: readonly (readonly [ReadingSize, string])[] = [['small', '小'], ['medium', '中'], ['large', '大']]
const lineHeightOptions: readonly (readonly [ReadingLineHeight, string])[] = [['compact', '紧凑'], ['comfortable', '舒适'], ['relaxed', '宽松']]

export function FontSettings() {
  const { settings, change, reset } = useFontSettings()
  const isDefault = (Object.keys(defaultFontSettings) as (keyof FontSettingsValue)[]).every(name => settings[name] === defaultFontSettings[name])
  return <section className="settings-section font-settings" aria-labelledby="font-settings-title">
    <div className="settings-heading"><div><h2 id="font-settings-title">字体</h2><p className="field-hint">为界面、正文和代码选择本地字体，调整会立即应用并保存在这台设备。</p></div><button type="button" className="button button-secondary" disabled={isDefault} onClick={reset}><RotateCcw size={14}/>恢复默认</button></div>
    <div className="font-preview" aria-label="字体实时预览">
      <p className="font-preview-ui">Tangentry / 发现 · Inter 2026</p>
      <p className="font-preview-reading">把想法写下来，隔一段时间再回来阅读。好的字体让长文、笔记和字幕都更容易读下去。</p>
      <code className="font-preview-code">const reading = "清晰、耐看、可持续"</code>
    </div>
    <Choice<UIFont> name="font-ui" legend="界面字体" hint="导航、按钮、设置和 AI 区域使用" value={settings.ui} options={uiOptions} onChange={ui => change({ ui })}/>
    <Choice<ReadingFont> name="font-reading" legend="正文与笔记字体" hint="文章阅读、笔记、字幕正文使用" value={settings.reading} options={readingOptions} onChange={reading => change({ reading })}/>
    <Choice<ReadingSize> name="font-reading-size" legend="正文字号" hint="只影响正文、笔记和字幕正文" value={settings.size} options={sizeOptions} onChange={size => change({ size })}/>
    <Choice<ReadingLineHeight> name="font-reading-line-height" legend="行高" hint="让长段落更紧凑或更舒展" value={settings.lineHeight} options={lineHeightOptions} onChange={lineHeight => change({ lineHeight })}/>
    <p className="field-hint settings-autosave"><span className="settings-state-dot" aria-hidden="true"/>更改自动保存</p>
  </section>
}
