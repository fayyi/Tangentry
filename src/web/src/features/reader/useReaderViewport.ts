import { useLayoutEffect } from 'react'

export function useReaderViewport(active: boolean) {
  useLayoutEffect(() => {
    if (!active) return
    const root = document.querySelector<HTMLElement>('.immersive-reader')
    if (!root) return
    let frame = 0, stopped = false
    let owner: HTMLElement | null = null
    const observed = new Set<Element>()
    const measure = () => {
      if (stopped) return
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        const columns = root.querySelector<HTMLElement>('.reading-columns:has(.subtitle-reader)')
        if (!columns) return
        const top = columns.getBoundingClientRect().top + window.scrollY
        const value = `${Math.ceil(top + 16)}px`
        if (root.style.getPropertyValue('--reader-viewport-offset') !== value) {
          root.style.setProperty('--reader-viewport-offset', value)
        }
        const primary = columns.querySelector<HTMLElement>('.reader-primary')
        const subtitles = primary?.querySelector<HTMLElement>('.subtitle-reader')
        const cues = subtitles?.querySelector<HTMLElement>('.subtitle-cues')
        const player = primary?.querySelector<HTMLElement>(':scope > .online-player, :scope > .local-player, :scope > video.local-video')
        const media = player?.matches('video') ? player : player?.querySelector<HTMLElement>('video, iframe, .online-play')
        if (owner !== primary) {
          owner?.style.removeProperty('--reader-player-height')
          owner = primary ?? null
        }
        const targets = new Set<Element>([root, columns])
        if (primary) targets.add(primary)
        if (player) targets.add(player)
        if (subtitles) {
          targets.add(subtitles)
          for (const child of subtitles.children) if (child !== cues) targets.add(child)
        }
        for (const target of observed) if (!targets.has(target)) { resize.unobserve(target); observed.delete(target) }
        for (const target of targets) if (!observed.has(target)) { resize.observe(target); observed.add(target) }
        if (!primary || !subtitles || !cues || !media || !window.matchMedia('(min-width: 961px)').matches) {
          primary?.style.removeProperty('--reader-player-height')
          return
        }
        // Reserve transcript space after measuring real controls and messages.
        // Cue highlighting and the playback clock do not trigger this observer.
        const cueBox = cues.getBoundingClientRect()
        const chrome = cueBox.top - columns.getBoundingClientRect().top - media.getBoundingClientRect().height
          + Math.max(0, subtitles.getBoundingClientRect().bottom - cueBox.bottom)
        const height = Math.floor(Math.min(innerHeight * .4, Math.max(96, innerHeight - Math.ceil(top + 16) - chrome - 120)))
        const budget = `${height}px`
        if (primary.style.getPropertyValue('--reader-player-height') !== budget) primary.style.setProperty('--reader-player-height', budget)
      })
    }
    const resize = new ResizeObserver(measure)
    const mutations = new MutationObserver(measure)
    mutations.observe(root, { childList: true, subtree: true })
    window.addEventListener('resize', measure)
    void document.fonts.ready.then(measure)
    measure()
    return () => {
      stopped = true
      cancelAnimationFrame(frame)
      resize.disconnect()
      mutations.disconnect()
      window.removeEventListener('resize', measure)
      root.style.removeProperty('--reader-viewport-offset')
      owner?.style.removeProperty('--reader-player-height')
    }
  }, [active])
}
