import { Button, Input, Select } from '../../components/ui'
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
      <label className="progress-filter-select"><span className="visually-hidden">按播放进度筛选</span><Select aria-label="按播放进度筛选" value={overview.filter} disabled={!overview.available} onChange={event => overview.setFilter(event.target.value as ProgressFilter)}>{progressOptions.map(option => <option key={option.value} value={option.value}>{option.label}进度</option>)}</Select></label>
      <label className="progress-collection-search"><Search size={15} aria-hidden="true" /><Input type="search" aria-label="搜索当前列表" placeholder="标题或作者" value={search} onChange={event => onSearch(event.target.value)} /></label>
      <Button variant="quiet" iconOnly className="icon-button" aria-label="刷新收藏播放进度" title="刷新收藏播放进度" disabled={overview.loading} onClick={() => void overview.reload()}><RefreshCw size={15} className={overview.loading ? 'spin' : undefined} /></Button>
      {overview.loading && <span className="visually-hidden" role="status">正在读取进度…</span>}
    </div>
    {overview.error && <p className="progress-overview-error" role="alert"><AlertCircle size={15} />{overview.error} 显示全部收藏，进度筛选暂不可用。</p>}
  </section>
}

export function CardProgress({ progress, supported }: { progress: ReadingProgress | null | undefined; supported: boolean }) {
  const label = !supported ? '暂不支持播放进度' : progress === undefined ? '进度暂不可用' : !progress || progress.positionSeconds === 0 ? '未开始' : progress.completed ? '看到结尾' : `进行中 · ${formatDuration(progress.positionSeconds)} / ${formatDuration(progress.durationSeconds)}`
  return <span className="card-progress"><span>{label}</span>{progress && progress.positionSeconds > 0 && <progress aria-label="已保存播放进度" value={progress.positionSeconds} max={progress.durationSeconds} />}</span>
}
