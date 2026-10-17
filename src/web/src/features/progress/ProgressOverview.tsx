import { AlertCircle, RefreshCw, Search } from 'lucide-react'
import type { ReadingProgress } from '../../shared/api/types'
import { formatDuration } from '../../shared/ui/ContentCard'
import type { ProgressFilter, useProgressOverview } from './useProgressOverview'

const progressOptions: { value: ProgressFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'not-started', label: '未开始' },
  { value: 'in-progress', label: '进行中' },
  { value: 'completed', label: '已看完' },
]

export function ProgressOverviewControls({ overview, search, onSearch }: { overview: ReturnType<typeof useProgressOverview>; search: string; onSearch: (value: string) => void }) {
  return <section className="progress-overview" aria-label="收藏播放进度">
    <div className="progress-overview-controls">
      <div className="progress-filter-options" role="radiogroup" aria-label="按播放进度筛选">{progressOptions.map(option => <label key={option.value}><input type="radio" name="collection-progress" value={option.value} checked={overview.filter === option.value} disabled={!overview.available} onChange={() => overview.setFilter(option.value)} /><span>{option.label}</span></label>)}</div>
      <label className="progress-collection-search"><Search size={15} aria-hidden="true" /><input type="search" aria-label="搜索当前列表" placeholder="标题或作者" value={search} onChange={event => onSearch(event.target.value)} /></label>
      <button className="icon-button" aria-label="刷新收藏播放进度" title="刷新收藏播放进度" disabled={overview.loading} onClick={() => void overview.reload()}><RefreshCw size={15} className={overview.loading ? 'spin' : undefined} /></button>
      {overview.loading && <span className="visually-hidden" role="status">正在读取进度…</span>}
    </div>
    {overview.error && <p className="progress-overview-error" role="alert"><AlertCircle size={15} />{overview.error} 显示全部收藏，进度筛选暂不可用。</p>}
  </section>
}

export function CardProgress({ progress, supported }: { progress: ReadingProgress | null | undefined; supported: boolean }) {
  const label = !supported ? '暂不支持播放进度' : progress === undefined ? '进度暂不可用' : !progress || progress.positionSeconds === 0 ? '未开始' : progress.completed ? '看到结尾' : `进行中 · ${formatDuration(progress.positionSeconds)} / ${formatDuration(progress.durationSeconds)}`
  return <span className="card-progress"><span>{label}</span>{progress && progress.positionSeconds > 0 && <progress aria-label="已保存播放进度" value={progress.positionSeconds} max={progress.durationSeconds} />}</span>
}
