import { Button } from '../components/ui'
import { BookOpen, X } from 'lucide-react'

export function ResumePanel({
  resume,
  onResume,
  onDismissResume
}: {
  resume: { title: string } | null
  onResume: () => void
  onDismissResume: () => void
}) {
  if (!resume) return null

  return (
    <aside className="experience-resume" aria-label="继续阅读">
      <Button variant="quiet"
        type="button"
        className="experience-resume-open"
        title={resume.title}
        onClick={onResume}
      >
        <BookOpen size={15} aria-hidden="true"/>
        <span>回到</span>
        <strong>{resume.title}</strong>
      </Button>
      <Button variant="quiet" iconOnly
        type="button"
        className="icon-button"
        aria-label="不再显示继续阅读"
        title="关闭"
        onClick={onDismissResume}
      >
        <X size={14}/>
      </Button>
    </aside>
  )
}
