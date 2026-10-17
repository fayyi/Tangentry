// The Tangentry mark: a lowercase t whose foot runs out along a tangent to a point.
export function BrandMark({ className = 'brand-mark' }: { className?: string }) {
  return <span className={className} aria-hidden="true"><svg viewBox="0 0 64 64" focusable="false">
    <g fill="none" stroke="currentColor" strokeWidth="6.5" strokeLinecap="round" strokeLinejoin="round"><path d="M26 13v26a10 10 0 0 0 10 10h2"/><path d="M18 24h20"/></g>
    <circle cx="47.5" cy="49" r="4" fill="currentColor"/>
  </svg></span>
}
