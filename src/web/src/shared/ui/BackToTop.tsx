import { Button } from '../../components/ui'
import { ArrowUp } from 'lucide-react'
import { useEffect, useState } from 'react'

interface BackToTopProps {
  threshold?: number
}

export function BackToTop({ threshold = 300 }: BackToTopProps) {
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const handleScroll = () => setVisible(window.scrollY > threshold)
    window.addEventListener('scroll', handleScroll, { passive: true })
    handleScroll()
    return () => window.removeEventListener('scroll', handleScroll)
  }, [threshold])

  return <Button variant="quiet" className={`back-to-top ${visible ? 'visible' : ''}`} onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })} aria-label="返回顶部">
    <ArrowUp size={20} aria-hidden="true" />
  </Button>
}
