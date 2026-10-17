import type { CapturedFrame } from './types'

const maxWidth = 1600
const maxHeight = 1920
const maxBytes = 1_000_000

// Draws the displayed frame into a canvas. Same-origin media (local files and
// the playback proxy) can be read back; cross-origin media taints the canvas.
// A paused video that was just seeked may not have decoded the new frame yet.
function decodedFrame(video: HTMLVideoElement) {
  if (video.readyState >= 2 || video.error) return Promise.resolve()
  return new Promise<void>(resolve => {
    const done = () => { clearTimeout(timer); for (const name of ['loadeddata', 'seeked', 'canplay', 'error']) video.removeEventListener(name, done); resolve() }
    const timer = setTimeout(done, 2000)
    for (const name of ['loadeddata', 'seeked', 'canplay', 'error']) video.addEventListener(name, done)
  })
}

export async function captureVideoFrame(video: HTMLVideoElement | null | undefined): Promise<CapturedFrame> {
  if (video) await decodedFrame(video)
  if (!video || video.readyState < 2 || !video.videoWidth || !video.videoHeight || video.error) throw new Error('视频画面还没有准备好，请等播放器显示画面后再截屏。')
  const position = video.currentTime
  if (!Number.isFinite(position) || position < 0) throw new Error('无法读取当前播放时间，请稍后再截屏。')
  const scale = Math.min(1, maxWidth / video.videoWidth, maxHeight / video.videoHeight)
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(video.videoWidth * scale))
  canvas.height = Math.max(1, Math.round(video.videoHeight * scale))
  const context = canvas.getContext('2d')
  if (!context) throw new Error('当前环境无法生成截图。')
  context.drawImage(video, 0, 0, canvas.width, canvas.height)
  let data = ''
  try {
    for (const quality of [0.9, 0.82, 0.72, 0.6, 0.45]) {
      data = canvas.toDataURL('image/jpeg', quality).slice('data:image/jpeg;base64,'.length)
      if (data.length * 3 / 4 <= maxBytes) break
    }
  } catch {
    throw new Error('这个来源的视频不允许读取画面。下载到本机后即可截屏。')
  }
  if (!data || data.length * 3 / 4 > maxBytes) throw new Error('截图过大，暂时无法保存。')
  return { data, width: canvas.width, height: canvas.height, positionSeconds: position }
}

/**
 * Captures a visible player element through the browser/desktop screen capture
 * picker. This is the only browser-sanctioned way to read pixels from a
 * cross-origin iframe (YouTube/Bilibili); iframe APIs intentionally expose no
 * video pixels. The user must choose the current tab/window in the picker.
 */
export async function captureDisplayedElement(element: HTMLElement | null | undefined, positionSeconds: number | null, controlsHeight = 48): Promise<CapturedFrame> {
  if (!element) throw new Error('播放器画面还没有显示，请稍后再截屏。')
  const rect = element.getBoundingClientRect()
  if (!rect.width || !rect.height) throw new Error('播放器画面还没有显示，请稍后再截屏。')
  // The snapshot contains only the visible page. Clamping an off-screen iframe
  // would otherwise silently crop unrelated pixels into the saved frame.
  if (rect.left < 0 || rect.top < 0 || rect.right > window.innerWidth || rect.bottom > window.innerHeight) {
    throw new Error('播放器没有完整显示在窗口内，请先滚动到播放器后再截屏。')
  }
  const native = (window as Window & { mygo?: { call<T>(method: string, ...args: unknown[]): Promise<T> } }).mygo
  if (native) {
    try {
      // Embedded players generally hide their transient controls after the
      // pointer leaves the iframe. Let that compositor state settle before the
      // native page snapshot; this preserves bottom subtitles/danmu unlike a
      // fixed crop.
      await waitForPlayerOverlayToHide()
      const png = await native.call<string>('DesktopCapture.Capture')
      const image = new Image()
      image.src = `data:image/png;base64,${png}`
      await image.decode()
      const scaleX = image.naturalWidth / Math.max(1, window.innerWidth)
      const scaleY = image.naturalHeight / Math.max(1, window.innerHeight)
      const crop = getCaptureCrop(rect, scaleX, scaleY, controlsHeight, image.naturalWidth, image.naturalHeight)
      return encodeCrop(image, crop.sx, crop.sy, crop.sw, crop.sh, positionSeconds)
    } catch (error) {
      if (!navigator.mediaDevices?.getDisplayMedia) throw error instanceof Error ? error : new Error('桌面页面截图失败，请下载视频到本机后再截屏。')
    }
  }
  if (!navigator.mediaDevices?.getDisplayMedia) throw new Error('当前环境不支持页面截屏，请下载视频到本机后再截屏。')
  const displayOptions = {
    video: { displaySurface: 'browser' },
    audio: false,
    preferCurrentTab: true,
    selfBrowserSurface: 'include',
    monitorTypeSurfaces: 'exclude',
  } as DisplayMediaStreamOptions & Record<string, unknown>
  const stream = await navigator.mediaDevices.getDisplayMedia(displayOptions)
  const track = stream.getVideoTracks()[0]
  if (!track) { stream.getTracks().forEach(item => item.stop()); throw new Error('没有获得可用的屏幕画面。') }
  // Window/monitor captures include browser chrome and OS decorations, so the
  // iframe's viewport coordinates cannot be mapped to their pixels reliably.
  // Ask for the tab instead of saving a plausible-looking but wrong crop.
  const displaySurface = track.getSettings().displaySurface
  if (displaySurface && displaySurface !== 'browser') {
    stream.getTracks().forEach(item => item.stop())
    throw new Error('请在截屏选择器中选择 Tangentry 当前标签页，窗口或整个屏幕无法准确裁出播放器。')
  }
  const source = document.createElement('video')
  source.muted = true
  source.playsInline = true
  source.srcObject = stream
  source.style.position = 'fixed'
  source.style.left = '-10000px'
  source.style.width = '1px'
  source.style.height = '1px'
  document.body.append(source)
  try {
    await waitForPlayerOverlayToHide()
    await new Promise<void>((resolve, reject) => {
      const done = () => { source.removeEventListener('loadedmetadata', done); source.removeEventListener('error', fail); resolve() }
      const fail = () => { source.removeEventListener('loadedmetadata', done); source.removeEventListener('error', fail); reject(new Error('屏幕画面暂时不可用。')) }
      source.addEventListener('loadedmetadata', done, { once: true }); source.addEventListener('error', fail, { once: true }); source.play().catch(fail)
    })
    if (source.readyState < 2) await new Promise<void>(resolve => source.addEventListener('canplay', () => resolve(), { once: true }))
    // Give the capture stream one compositor tick to present the current frame.
    await new Promise<void>(resolve => {
      if ('requestVideoFrameCallback' in source) source.requestVideoFrameCallback(() => resolve())
      else requestAnimationFrame(() => resolve())
    })
    const scaleX = source.videoWidth / Math.max(1, window.innerWidth)
    const scaleY = source.videoHeight / Math.max(1, window.innerHeight)
    const { sx, sy, sw, sh } = getCaptureCrop(rect, scaleX, scaleY, controlsHeight, source.videoWidth, source.videoHeight)
    if (sw <= 0 || sh <= 0) throw new Error('截屏区域不在当前屏幕画面内，请选择当前应用窗口。')
    return encodeCrop(source, sx, sy, sw, sh, positionSeconds)
  } finally {
    stream.getTracks().forEach(item => item.stop()); source.pause(); source.srcObject = null; source.remove()
  }
}

/** Maps viewport coordinates to capture pixels; controlsHeight is opt-in. */
export function getCaptureCrop(rect: Pick<DOMRect, 'left' | 'top' | 'width' | 'height'>, scaleX: number, scaleY: number, controlsHeight: number, sourceWidth: number, sourceHeight: number) {
  const sx = Math.max(0, Math.floor(rect.left * scaleX))
  const sy = Math.max(0, Math.floor(rect.top * scaleY))
  const sw = Math.min(sourceWidth - sx, Math.max(1, Math.floor(rect.width * scaleX)))
  const sh = Math.min(sourceHeight - sy, Math.max(1, Math.floor((rect.height - Math.max(0, controlsHeight)) * scaleY)))
  return { sx, sy, sw, sh }
}

function waitForPlayerOverlayToHide(): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, 450))
}

function encodeCrop(source: CanvasImageSource & { width?: number; height?: number; videoWidth?: number; videoHeight?: number }, sx: number, sy: number, sw: number, sh: number, positionSeconds: number | null): CapturedFrame {
  const sourceWidth = source.videoWidth ?? source.width ?? 0
  const sourceHeight = source.videoHeight ?? source.height ?? 0
  sx = Math.max(0, Math.min(sourceWidth, sx)); sy = Math.max(0, Math.min(sourceHeight, sy))
  sw = Math.min(sourceWidth - sx, sw); sh = Math.min(sourceHeight - sy, sh)
  if (sw <= 0 || sh <= 0) throw new Error('截屏区域不在当前页面内，请选择当前应用窗口。')
  const scale = Math.min(1, maxWidth / sw, maxHeight / sh)
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(sw * scale)); canvas.height = Math.max(1, Math.round(sh * scale))
  const context = canvas.getContext('2d')
  if (!context) throw new Error('当前环境无法生成截图。')
  context.drawImage(source, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height)
  let data = ''
  for (const quality of [0.9, 0.82, 0.72, 0.6, 0.45]) {
    data = canvas.toDataURL('image/jpeg', quality).slice('data:image/jpeg;base64,'.length)
    if (data.length * 3 / 4 <= maxBytes) break
  }
  if (!data || data.length * 3 / 4 > maxBytes) throw new Error('截图过大，暂时无法保存。')
  return { data, width: canvas.width, height: canvas.height, positionSeconds: Number.isFinite(positionSeconds) && positionSeconds! >= 0 ? positionSeconds! : 0 }
}
