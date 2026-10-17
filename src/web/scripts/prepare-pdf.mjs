import { cp, mkdir, readFile, writeFile } from 'node:fs/promises'
const root = new URL('../', import.meta.url)
const target = new URL('public/pdfjs/', root)
await mkdir(target, { recursive: true })
for (const name of ['cmaps', 'standard_fonts', 'wasm', 'LICENSE']) {
  await cp(new URL('node_modules/pdfjs-dist/' + name, root), new URL(name, target), { recursive: true })
}
// Reuse the installed PDF.js text-layer styles without importing its entire
// viewer UI. Preserve its Apache-2.0 notice and fail if the upstream layout moves.
const css = await readFile(new URL('node_modules/pdfjs-dist/web/pdf_viewer.css', root), 'utf8')
const start = css.indexOf('\n.textLayer {'), end = css.indexOf('\n.textLayerImages {', start)
const noticeEnd = css.indexOf('*/') + 2
if (start < 0 || end < 0 || noticeEnd < 2) throw new Error('PDF.js text-layer CSS layout changed; review the installed version')
await writeFile(new URL('text_layer.css', target), css.slice(0, noticeEnd) + '\n' + css.slice(start, end))
