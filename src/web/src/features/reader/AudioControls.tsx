import { Pause, Play, RotateCcw, RotateCw, Volume2, VolumeX } from 'lucide-react'
import { useEffect, useState } from 'react'
import type { RefObject } from 'react'

const clock = (seconds: number) => {
  const value = Math.max(0, Math.floor(seconds))
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`
}

export function AudioControls({ media, identity }: { media: RefObject<HTMLMediaElement | null>; identity: string }) {
  const [state, setState] = useState({ time: 0, duration: 0, playing: false, muted: false, rate: 1 })
  const [error, setError] = useState('')
  useEffect(() => {
    const node = media.current
    if (!node) return
    const sync = () => setState({ time: Number.isFinite(node.currentTime) ? node.currentTime : 0, duration: Number.isFinite(node.duration) ? node.duration : 0, playing: !node.paused && !node.ended, muted: node.muted, rate: node.playbackRate })
    const events = ['timeupdate', 'durationchange', 'loadedmetadata', 'play', 'pause', 'ended', 'volumechange', 'ratechange', 'seeked']
    events.forEach(event => node.addEventListener(event, sync))
    setError(''); sync()
    return () => events.forEach(event => node.removeEventListener(event, sync))
  }, [media, identity])
  async function toggle() {
    const node = media.current
    if (!node) return
    setError('')
    if (!node.paused) node.pause()
    else try { await node.play() } catch { setError('播放未能开始，请重试或打开原内容。') }
  }
  function skip(delta: number) {
    const node = media.current
    if (node && state.duration > 0) node.currentTime = Math.max(0, Math.min(state.duration, node.currentTime + delta))
  }
  return <div className="podcast-console" role="group" aria-label="播客播放控制">
    <div className="podcast-console-main"><button type="button" className="podcast-play" aria-label={state.playing ? '暂停播客' : '播放播客'} title={state.playing ? '暂停播客' : '播放播客'} onClick={() => void toggle()}>{state.playing ? <Pause size={20}/> : <Play size={20}/>}</button><div className="podcast-timeline"><div className="podcast-clock"><output>{clock(state.time)}</output>
    <div className="podcast-console-tools"><button type="button" className="icon-button" aria-label="后退15秒" title="后退15秒" disabled={!state.duration} onClick={() => skip(-15)}><RotateCcw size={16}/></button><button type="button" className="icon-button" aria-label="前进30秒" title="前进30秒" disabled={!state.duration} onClick={() => skip(30)}><RotateCw size={16}/></button><button type="button" className="podcast-rate" aria-label={`播放速度 ${state.rate} 倍`} title="切换播放速度" onClick={() => { const rates = [1, 1.25, 1.5, 1.75, 2]; if (media.current) media.current.playbackRate = rates[(rates.indexOf(state.rate) + 1) % rates.length] }}>{state.rate}x</button><button type="button" className="icon-button" aria-label={state.muted ? '打开播客声音' : '静音播客'} title={state.muted ? '打开声音' : '静音'} onClick={() => { if (media.current) media.current.muted = !media.current.muted }}>{state.muted ? <VolumeX size={16}/> : <Volume2 size={16}/>}</button></div>
    <span>{clock(state.duration)}</span></div><input type="range" aria-label="播客播放进度" min={0} max={state.duration || 1} step={0.1} value={Math.min(state.time, state.duration || 1)} disabled={!state.duration} onChange={event => { if (media.current) media.current.currentTime = Number(event.target.value) }}/></div></div>
    {error && <p role="alert">{error}</p>}
  </div>
}
