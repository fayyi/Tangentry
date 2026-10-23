import { Button } from '../../components/ui'
import { Check, Download, RefreshCw, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { request } from '../../shared/api/client'
import { listOrEmpty } from '../../shared/api/list'

interface Model { id: string; backend?: string; size: string; description: string; installed: boolean; path?: string; available?: boolean }
interface Job { id: string; modelId: string; state: string; error?: string; received?: number; total?: number }
interface Catalog { models: Model[]; job: Job | null; available: boolean }
const running = (job: Job | null) => job != null && ['fetching', 'verifying', 'cancelling'].includes(job.state)

export function WhisperModels({ onInstalled, onChoose, refreshKey, backend = 'faster-whisper', configurationPending = false, selectedModel = '', selectedAligner = '' }: { onInstalled: () => void; onChoose: (path: string, id: string) => void; refreshKey: string; backend?: string; configurationPending?: boolean; selectedModel?: string; selectedAligner?: string }) {
  const [catalog, setCatalog] = useState<Catalog | null>(null)
  const [revision, setRevision] = useState(0)
  const [error, setError] = useState(''), [pending, setPending] = useState(false)
  const [checking, setChecking] = useState(true), [readError, setReadError] = useState('')
  const callback = useRef(onInstalled); callback.current = onInstalled
  const previous = useRef('')
  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    let failures = 0
    setChecking(true); setReadError('')
    async function read() {
      try {
        const value = await request<Catalog>('/transcription/models', { signal: controller.signal })
        if (controller.signal.aborted) return
        setCatalog(value)
        failures = 0; setReadError('')
        if (value.job?.state === 'completed' && previous.current && previous.current !== 'completed') callback.current()
        previous.current = value.job?.state ?? ''
      } catch {
        if (controller.signal.aborted) return
        failures++
        setReadError(['fetching', 'verifying', 'cancelling'].includes(previous.current) ? '状态读取失败，下载可能仍在继续。' : '模型列表读取失败，请刷新重试。')
      } finally {
        if (!controller.signal.aborted) setChecking(false)
      }
      if (!controller.signal.aborted && failures < 3 && ['fetching', 'verifying', 'cancelling'].includes(previous.current)) timer = setTimeout(() => { void read() }, 3000 * Math.max(1, failures))
    }
    void read()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [revision, refreshKey])
  async function download(id: string) {
    setPending(true); setError('')
    try {
      const job = await request<Job>(`/transcription/models/${id}/download`, { method: 'POST' })
      setCatalog(value => value ? { ...value, job } : value)
      previous.current = job.state
      setRevision(value => value + 1)
    } catch (cause) { setError(cause instanceof Error ? cause.message : '下载未能开始。') }
    finally { setPending(false) }
  }
  async function cancel() {
    if (!catalog?.job) return
    setPending(true); setError('')
    try { await request(`/transcription/model-downloads/${catalog.job.id}/cancel`, { method: 'POST' }); setRevision(value => value + 1) }
    catch (cause) { setError(cause instanceof Error ? cause.message : '取消未能完成。') }
    finally { setPending(false) }
  }
  const models = listOrEmpty(catalog?.models).filter(model => (model.backend || 'faster-whisper') === backend)
  const groups = backend === 'qwen3-asr' ? [
    { label: '识别模型', hint: '识别声音里的文字', models: models.filter(model => model.id !== 'qwen3-aligner-0.6b') },
    { label: '时间对齐', hint: '为每句话标出时间', models: models.filter(model => model.id === 'qwen3-aligner-0.6b') },
  ] : [{ label: '', hint: '', models }]
  return <div className="whisper-models" aria-labelledby="whisper-models-title">
    <div className="settings-heading"><h3 id="whisper-models-title">可选模型 <span>{backend==='qwen3-asr'?'Qwen3-ASR':backend}</span></h3><Button variant="quiet" iconOnly type="button" className="icon-button" aria-label="刷新模型列表" title="刷新模型列表" disabled={checking || pending} onClick={()=>setRevision(value=>value+1)}><RefreshCw size={15}/></Button></div>
    <p className="field-hint">下载后选用，保存后生效。文件保存在本机，不会自动下载。</p>
    {backend === 'qwen3-asr' && (!selectedModel.trim() || !selectedAligner.trim()) && <p className="field-hint" role="status">{!selectedModel.trim() && !selectedAligner.trim() ? '需选择识别与对齐模型。' : !selectedModel.trim() ? '还需选择识别模型。' : '还需选择对齐模型。'}</p>}
    {checking && <p className="field-hint" role="status">正在读取模型…</p>}
    {readError && <p className="subtitle-error" role="alert">{readError}</p>}
    {catalog && backend !== 'whisper.cpp' && !catalog.available && <p className="field-hint">先保存装有 huggingface_hub 的 Python 环境，即可下载模型。下载不需要先填写模型路径。</p>}
    <div className="whisper-model-list">{groups.map(group => <div key={group.label}>
      {group.label && <div className="whisper-model-group-heading"><h4>{group.label}</h4><span>{group.hint}</span></div>}
      {group.models.map(model => {
        const selectedPath = model.id === 'qwen3-aligner-0.6b' ? selectedAligner : selectedModel
        const selected = Boolean(model.installed && model.path && model.path === selectedPath)
        return <div className="whisper-model-row" key={model.id}>
          <div><strong>{model.id}</strong><span>{model.installed && '已安装 · '}{model.size} · {model.description}</span></div>
          {model.installed ? <Button variant="quiet" type="button" className="button button-quiet" aria-label={`${selected ? '已选择' : '选用'} ${model.id}`} aria-pressed={selected} title={selected ? '当前选择' : '选择此模型'} disabled={configurationPending || pending || !model.path || selected} onClick={() => model.path && onChoose(model.path, model.id)}>{selected && <Check size={14} aria-hidden="true"/>}{selected ? '已选择' : '选用'}</Button> : <Button variant="quiet" type="button" className="button button-quiet" aria-label={`下载 ${model.id}`} disabled={configurationPending || checking || !!readError || pending || running(catalog?.job ?? null) || !(model.available ?? catalog?.available)} onClick={() => { void download(model.id) }}><Download size={14}/>下载</Button>}
        </div>
      })}
    </div>)}</div>
    {catalog?.job && <div className="whisper-model-progress" role="status"><span>{catalog.job.modelId} · {{fetching:'正在下载',verifying:'正在校验',cancelling:'正在取消',completed:'下载完成',canceled:'已取消',failed:'下载失败'}[catalog.job.state] ?? catalog.job.state}{catalog.job.state === 'fetching' && catalog.job.total ? ` ${Math.floor((catalog.job.received ?? 0) * 100 / catalog.job.total)}%` : ''}</span>{running(catalog.job) && <Button variant="quiet" type="button" className="button button-quiet" disabled={pending || catalog.job.state === 'cancelling'} onClick={() => { void cancel() }}><X size={14}/>取消下载</Button>}</div>}
    {catalog?.job?.error && <p className="subtitle-error" role="alert">{catalog.job.error}</p>}
    {error && <p className="subtitle-error" role="alert">{error}</p>}
  </div>
}
