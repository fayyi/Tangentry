import { useEffect, useState } from 'react'

export type GalleryColumns = 'auto' | '2' | '3' | '4' | '5'
const key = 'tangentry-gallery-columns'

export function useGalleryLayout() {
  const [columns, setColumns] = useState<GalleryColumns>(() => {
    return 'auto'
  })
  useEffect(() => {
    document.documentElement.dataset.galleryColumns = columns
    try { localStorage.removeItem(key) } catch { /* Automatic layout does not require storage. */ }
  }, [columns])
  useEffect(() => {
    const sync = (event: StorageEvent) => {
      if (event.key === key) setColumns('auto')
    }
    window.addEventListener('storage', sync)
    return () => window.removeEventListener('storage', sync)
  }, [])
  return { columns, setColumns }
}
