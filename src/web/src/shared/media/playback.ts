// One user-initiated gallery playback at a time, including embedded videos.
export const mediaStartEvent = 'tangentry:media-start'
export function announcePlayback(owner: string) { window.dispatchEvent(new CustomEvent(mediaStartEvent, { detail: owner })) }
