import { useEffect, useState } from 'react'

export function useCardCover(thumbnailUrl: string) {
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [thumbnailUrl])
  return { failed, setFailed }
}
