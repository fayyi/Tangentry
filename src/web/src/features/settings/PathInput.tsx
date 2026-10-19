import { File, FolderOpen } from 'lucide-react'
import { useState } from 'react'
import { selectDesktopPath, useCapabilities, type DesktopPathKind } from '../../shared/api/desktop'

export function PathInput({ label, ariaLabel = label, value, kind, placeholder, disabled, onChange }: {
  label: string; ariaLabel?: string; value: string; kind: DesktopPathKind; placeholder?: string; disabled?: boolean; onChange: (path: string) => void
}) {
  const [picking, setPicking] = useState(false)
  const [error, setError] = useState('')
  const { pathPicker } = useCapabilities()
  async function choose() {
    setPicking(true); setError('')
    try {
      const result = await selectDesktopPath(kind, value)
      if (!result.cancelled) onChange(result.path)
    } catch (cause) { setError(cause instanceof Error ? cause.message : '无法选择路径。') }
    finally { setPicking(false) }
  }
  const directory = kind === 'model-directory' || kind === 'backup-directory'
  const pickerAction = value ? `重新选择${directory ? '文件夹' : '文件'}` : `选择${directory ? '文件夹' : '文件'}`
  return <div className="path-field"><label>{label}<span className="path-input">
    {directory && pathPicker ? <span className="path-display" aria-label={ariaLabel}>{value || '尚未选择文件夹'}</span> : <input aria-label={ariaLabel} value={value} disabled={disabled || picking} maxLength={2048} placeholder={placeholder} onChange={event => onChange(event.target.value)}/>}
    {pathPicker && <button type="button" className="button button-secondary path-picker-button" disabled={disabled || picking} title={pickerAction} aria-label={pickerAction} onClick={() => void choose()}>{directory ? <FolderOpen size={15}/> : <File size={15}/>} {picking ? '正在选择…' : pickerAction}</button>}
    {directory && pathPicker && value && <button type="button" className="button button-ghost" disabled={disabled || picking} onClick={() => onChange('')}>清除</button>}
  </span></label>{!pathPicker && directory && <p className="field-hint">这是 Tangentry 服务端上的路径，不是浏览器所在电脑的路径。</p>}{error && <p className="subtitle-error" role="alert">{error}</p>}</div>
}
