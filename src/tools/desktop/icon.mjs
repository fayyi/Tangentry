import { deflateSync } from 'node:zlib'

// Rasterize the favicon's stroked mark as a 1024px PNG, without an icon dependency.
export function iconPNG() {
  function crc32(buffer) {
    let crc = 0xffffffff
    for (const byte of buffer) {
      crc ^= byte
      for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1))
    }
    return (crc ^ 0xffffffff) >>> 0
  }
  function chunk(type, data) {
    const name = Buffer.from(type)
    const size = Buffer.alloc(4)
    size.writeUInt32BE(data.length)
    const crc = Buffer.alloc(4)
    crc.writeUInt32BE(crc32(Buffer.concat([name, data])))
  return Buffer.concat([size, name, data, crc])
  }
  const size = 1024
  const pixels = Buffer.alloc(size * (1 + size * 4))
  // Same geometry as web/public/favicon.svg, in its 64-unit grid.
  const strokes = [[[26, 13], [26, 39]], [[36, 49], [38, 49]], [[18, 24], [38, 24]]]
  const arc = { x: 36, y: 39, r: 10 }
  const halfStroke = 3.25
  const segment = (px, py, [[ax, ay], [bx, by]]) => {
    const dx = bx - ax, dy = by - ay
    const t = Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)))
    return Math.hypot(px - ax - t * dx, py - ay - t * dy)
  }
  const arcDistance = (px, py) => {
    // Quarter arc from the stem's foot (left of centre) to the base (below centre).
    const angle = Math.atan2(py - arc.y, px - arc.x)
    if (angle >= Math.PI / 2) return Math.abs(Math.hypot(px - arc.x, py - arc.y) - arc.r)
    return Math.min(Math.hypot(px - 26, py - 39), Math.hypot(px - 36, py - 49))
  }
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const sx = (x + 0.5) / 16
      const sy = (y + 0.5) / 16
      const cornerX = Math.max(14 - sx, sx - 50, 0)
      const cornerY = Math.max(14 - sy, sy - 50, 0)
      const coverage = Math.max(0, Math.min(1, (14 - Math.hypot(cornerX, cornerY)) * 16 + 0.5))
      const stroke = Math.min(arcDistance(sx, sy), ...strokes.map(line => segment(sx, sy, line))) - halfStroke
      const dot = Math.hypot(sx - 47.5, sy - 49) - 4
      const mark = Math.max(0, Math.min(1, -Math.min(stroke, dot) * 16 + 0.5))
      const offset = y * (1 + size * 4) + 1 + x * 4
      pixels[offset] = pixels[offset + 1] = pixels[offset + 2] = Math.round(23 + mark * 232)
      pixels[offset + 3] = Math.round(coverage * 255)
    }
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(size, 0)
  header.writeUInt32BE(size, 4)
  header[8] = 8
  header[9] = 6
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk('IHDR', header), chunk('IDAT', deflateSync(pixels)), chunk('IEND', Buffer.alloc(0)),
  ])
}
