// ==UserScript==
// @name         Tangentry Bilibili playback sync
// @namespace    tangentry.local
// @version      1.1.1
// @description  Share the official player's real media clock with your local Tangentry reader.
// @match        https://player.bilibili.com/player.html*
// @grant        none
// @run-at       document-idle
// ==/UserScript==

(() => {
  'use strict'
  const id = new URL(location.href).searchParams.get('bvid')
  let peer = null, origin = '', token = '', sequence = 0, timer
  const video = () => document.querySelector('video')
  const stop = () => { clearTimeout(timer); timer = undefined }
  const report = () => {
    const node = video()
    if (!peer || !node || node.readyState < 1 || node.error || !Number.isFinite(node.duration) || node.duration <= 0) return
    peer.postMessage({ type: 'tangentry:bilibili:clock', token, id, sequence: ++sequence, position: node.currentTime, duration: node.duration, paused: node.paused, ended: node.ended, seeking: node.seeking }, origin)
  }
  const tick = () => {
    stop()
    if (!peer) return
    report()
    const node = video()
    // Retain a low-frequency handshake while paused or buffering.
    timer = setTimeout(tick, document.hidden || !node || node.paused || node.ended ? 1000 : 100)
  }
  window.addEventListener('message', event => {
    if (event.source !== parent || parent === window) return
    let url
    try { url = new URL(event.origin) } catch { return }
    if (!['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) || !['http:', 'https:'].includes(url.protocol)) return
    const data = event.data
    if (!data || data.id !== id || typeof data.token !== 'string' || data.token.length > 100) return
    if (data.type === 'tangentry:bilibili:connect') {
      if (token !== data.token) sequence = 0
      peer = event.source; origin = event.origin; token = data.token; tick()
    } else if (peer && data.token === token && data.type === 'tangentry:bilibili:seek') {
      const node = video()
      if (node && Number.isFinite(data.position) && data.position >= 0 && data.position <= node.duration) {
        try { node.currentTime = data.position; report() } catch { /* Report only the real clock when a source rejects seeking. */ }
      }
    }
  })
  for (const name of ['timeupdate', 'seeking', 'seeked', 'pause', 'play', 'ended', 'loadedmetadata']) document.addEventListener(name, report, true)
  document.addEventListener('visibilitychange', tick)
  window.addEventListener('pagehide', () => { report(); stop() })
})()
