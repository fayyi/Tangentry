
export function SkeletonCard() {
  return (
    <div className="skeleton-card">
      <div className="skeleton-thumbnail" />
      <div className="skeleton-content">
        <div className="skeleton skeleton-title" />
        <div className="skeleton skeleton-title-line2" />
        <div className="skeleton-author">
          <div className="skeleton skeleton-avatar" />
          <div className="skeleton-author-info">
            <div className="skeleton skeleton-author-name" />
            <div className="skeleton skeleton-author-meta" />
          </div>
        </div>
      </div>
    </div>
  )
}

export function SkeletonGallery({ count = 8 }: { count?: number }) {
  return (
    <div className="skeleton-gallery">
      {Array.from({ length: count }, (_, i) => (
        <SkeletonCard key={i} />
      ))}
    </div>
  )
}

export function LoadingDots() {
  return (
    <span className="loading-dots" aria-label="加载中">
      <span />
      <span />
      <span />
    </span>
  )
}
