import { Fragment } from 'react'
import LinkifyIt from 'linkify-it'
import { SourceText } from './SourceText'

const links = new LinkifyIt().set({ fuzzyEmail: false })

export function DescriptionText({ text, onSeek }: { text: string; onSeek?: (seconds: number) => void }) {
  if (!onSeek) return <SourceText text={text}/>
  const urls = links.match(text) ?? []
  const timestamps = [...text.matchAll(/(?<![\p{L}\p{N}:])(?:(\d{1,2}):)?(\d{1,3}):([0-5]\d)(?![\p{L}\p{N}:])/gu)].flatMap(match => {
    const seconds = Number(match[1] ?? 0) * 3600 + Number(match[2]) * 60 + Number(match[3])
    // Recognize inline and bracketed chapters without changing timestamps in URLs.
    if (seconds >= 7 * 86400 || match[1] && Number(match[2]) >= 60 || urls.some(url => match.index < url.lastIndex && match.index + match[0].length > url.index)) return []
    return [{ text: match[0], start: match.index, seconds }]
  })
  let offset = 0
  return <>{timestamps.map(timestamp => {
    const before = text.slice(offset, timestamp.start)
    offset = timestamp.start + timestamp.text.length
    return <Fragment key={timestamp.start}><SourceText text={before}/><button type="button" className="description-time" title={`播放至 ${timestamp.text}`} onClick={() => onSeek(timestamp.seconds)}>{timestamp.text}</button></Fragment>
  })}<SourceText text={text.slice(offset)}/></>
}
