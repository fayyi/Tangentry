import { FolderOpen } from 'lucide-react'
import { useState } from 'react'
import { selectDesktopPath, useCapabilities } from '../../shared/api/desktop'

export function PathInput({ label, ariaLabel = label, value, kind, placeholder, disabled, onChange }: {
  label: string; ariaLabel?: string; value: string; kind: string; placeholder?: string; disabled?: boolean; onChange: (path: string) => void
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
  return <div className="path-field"><label>{label}<span className="path-input">
    <input aria-label={ariaLabel} value={value} disabled={disabled || picking} maxLength={2048} placeholder={placeholder} onChange={event => onChange(event.target.value)}/>
    {pathPicker && <button type="button" className="icon-button" disabled={disabled || picking} title={`选择${label}`} aria-label={`选择${label}`} onClick={() => void choose()}><FolderOpen size={17}/></button>}
  </span></label>{error && <p className="subtitle-error" role="alert">{error}</p>}</div>
}
