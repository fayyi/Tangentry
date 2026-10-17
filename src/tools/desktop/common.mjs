import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { createReadStream, createWriteStream } from 'node:fs'
import { mkdtemp, rename, rm, stat } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { Readable } from 'node:stream'
import { pipeline } from 'node:stream/promises'

export const run = (command, args, options = {}) => execFileSync(command, args, {
  encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], ...options,
}).trim()

export const exists = async filename => stat(filename).then(() => true, () => false)

export async function sha256(filename) {
  const digest = createHash('sha256')
  for await (const chunk of createReadStream(filename)) digest.update(chunk)
  return digest.digest('hex')
}

export async function text(url) {
  const response = await fetch(url, { signal: AbortSignal.timeout(60_000) })
  if (!response.ok) throw new Error(`Cannot read ${url} (${response.status})`)
  return response.text()
}

// Downloads once into the cache and refuses files whose checksum differs.
export async function download(url, filename, expected) {
  if (await exists(filename) && await sha256(filename) === expected) return filename
  console.log(`Fetching ${path.basename(filename)}`)
  const response = await fetch(url, { signal: AbortSignal.timeout(900_000) })
  if (!response.ok || !response.body) throw new Error(`Download failed: ${url} (${response.status})`)
  const temporary = filename + '.partial'
  try {
    await pipeline(Readable.fromWeb(response.body), createWriteStream(temporary))
    if (await sha256(temporary) !== expected) throw new Error(`Checksum mismatch: ${url}`)
    await rename(temporary, filename)
  } finally {
    await rm(temporary, { force: true })
  }
  return filename
}

// The checksum of `name` in a `<sha256>  <name>` listing such as SHASUMS256.txt.
export function checksumOf(listing, name) {
  const line = listing.split('\n').find(value => value.trim().split(/\s+/).at(-1)?.replace(/^\*/, '') === name)
  const value = line?.trim().split(/\s+/)[0]
  if (!value || !/^[a-f0-9]{64}$/.test(value)) throw new Error(`Missing checksum for ${name}`)
  return value
}

// Extracts a .zip or .tar.* archive into a fresh temporary directory.
export async function extract(archive) {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'tangentry-runtime-'))
  // Windows' own tar (bsdtar) reads zip archives too.
  if (archive.endsWith('.zip') && process.platform !== 'win32') run('unzip', ['-q', archive, '-d', directory])
  else run('tar', ['-xf', archive, '-C', directory])
  return directory
}
