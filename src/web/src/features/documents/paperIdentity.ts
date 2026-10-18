const arxivID = '(?:\\d{4}\\.\\d{4,5}|[a-z-]+(?:\\.[A-Z]{2})?/\\d{7})'
const arxivPattern = new RegExp(`^(${arxivID})(?:v([1-9]\\d*))?$`, 'i')

export interface PaperIdentity { arxivId: string; version?: number }

export function parsePaperIdentity(input: string): PaperIdentity | null {
  const value = input.trim()
  if (!value) return null

  let candidate = value.replace(/^arxiv:\s*/i, '')
  if (!/^https?:\/\//i.test(candidate) && /^(?:www\.)?(?:arxiv\.org|huggingface\.co|alphaxiv\.org)\//i.test(candidate)) candidate = `https://${candidate}`
  if (/^https?:\/\//i.test(candidate)) {
    try {
      const url = new URL(candidate)
      const host = url.hostname.toLowerCase().replace(/^www\./, '')
      if (host === 'arxiv.org' || host === 'export.arxiv.org') {
        const match = url.pathname.match(/^\/(?:abs|pdf)\/(.+?)(?:\.pdf)?\/?$/i)
        if (!match) return null
        candidate = decodeURIComponent(match[1])
      } else if (host === 'huggingface.co' || host === 'www.huggingface.co' || host === 'alphaxiv.org' || host === 'www.alphaxiv.org') {
        const match = url.pathname.match(/^\/(?:papers|abs|pdf)\/(.+?)(?:\.pdf)?\/?$/i)
        if (!match) return null
        candidate = decodeURIComponent(match[1])
      } else return null
    } catch { return null }
  }

  candidate = candidate.replace(/\.pdf$/i, '').replace(/\/$/, '')
  const match = candidate.match(arxivPattern)
  if (!match) return null
  return { arxivId: match[1], ...(match[2] ? { version: Number(match[2]) } : {}) }
}

export function isPaperQuery(input: string): boolean { return parsePaperIdentity(input) !== null }
