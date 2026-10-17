// Starts a browser download for content generated or fetched by the page.
export function saveFile(data: Blob | string, filename: string, type = 'application/octet-stream') {
  const url = URL.createObjectURL(typeof data === 'string' ? new Blob([data], { type }) : data)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.append(anchor); anchor.click(); anchor.remove()
  // Allow browsers time to start their download before releasing the object URL.
  setTimeout(() => URL.revokeObjectURL(url), 30_000)
}
