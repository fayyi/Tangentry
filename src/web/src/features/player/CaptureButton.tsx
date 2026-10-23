import { Button } from '../../components/ui'
import { Camera } from 'lucide-react'
import { ReaderActions } from '../../shared/reader/ReaderActions'
import './capture.css'

// Two entry points for one action: on the video while watching, and in the reader bar.
export function PlayerCaptureOverlay({ onCapture }: { onCapture: () => void }) {
  return <Button variant="quiet" type="button" className="player-capture" title="截屏记录（⌥S）" aria-label="截屏当前画面" onClick={onCapture}><Camera size={16} aria-hidden="true"/><span>截屏</span></Button>
}

export function ReaderCaptureAction({ onCapture }: { onCapture: () => void }) {
  return <ReaderActions><Button variant="secondary" type="button" className="button button-secondary reader-capture" title="截屏当前画面，关联时间与字幕原句（⌥S）" onClick={onCapture}><Camera size={14} aria-hidden="true"/>截屏记录</Button></ReaderActions>
}
