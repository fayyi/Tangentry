// Build-time tooling only. No package manager or downloads run on the user's machine.
//
//   node src/tools/desktop/prepare.mjs [--platform darwin/arm64,windows/amd64,linux/amd64,...]
//
// Writes each target's runtime to resources/<os>-<arch>/runtime, which MyGo
// ships to that platform's app only, and the shared icon to resources/icon.png.
// The runtime holds only the native tools the app cannot fetch later: yt-dlp,
// its JavaScript runtime and the Whisper model are downloaded and verified by
// the app on first use, and Linux packages depend on the system FFmpeg.
import { execFileSync } from 'node:child_process'
import { mkdir, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { text } from './common.mjs'
import { iconPNG } from './icon.mjs'
import { bundleMacTools } from './macos.mjs'
import { installPrebuiltTools, supportsPrebuilt } from './prebuilt.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const resources = path.join(root, 'resources')
const cache = path.join(root, '.mygo-resources', 'cache')

const host = `${process.platform === 'win32' ? 'windows' : process.platform}/${process.arch === 'x64' ? 'amd64' : process.arch}`
const flag = process.argv.indexOf('--platform')
const targets = (flag > 0 ? process.argv[flag + 1] : host).split(',').map(value => value.trim()).filter(Boolean)
for (const target of targets) {
  const name = target.replace('/', '-')
  if (name !== 'darwin-arm64' && !supportsPrebuilt(name)) throw new Error(`Unsupported desktop target: ${target}`)
  if (name === 'darwin-arm64' && host !== 'darwin/arm64') throw new Error('The macOS runtime bundles Homebrew tools and needs an Apple silicon Mac.')
}

await mkdir(cache, { recursive: true })
const sharedLicenses = await Promise.all([
  ['mygo-LICENSE', 'https://raw.githubusercontent.com/egoist/mygo/v0.5.5/LICENSE'],
].map(async ([name, url]) => [name, await text(url)]))

for (const target of targets) {
  const name = target.replace('/', '-')
  const windows = name.startsWith('windows')
  const exe = windows ? '.exe' : ''
  const runtime = path.join(resources, name, 'runtime')
  const bin = path.join(runtime, 'bin')
  const lib = path.join(runtime, 'lib')
  const licenses = path.join(runtime, 'licenses')
  console.log(`Preparing the ${target} runtime`)
  // Rebuild tools from a clean staging tree; downloads stay in the cache.
  await rm(runtime, { recursive: true, force: true })
  await Promise.all([bin, licenses].map(dir => mkdir(dir, { recursive: true })))
  for (const [license, content] of sharedLicenses) await writeFile(path.join(licenses, license), content)

  let minimumSystemVersion
  await mkdir(lib, { recursive: true })
  if (name === 'darwin-arm64') ({ minimumSystemVersion } = await bundleMacTools({ bin, lib, licenses }))
  else await installPrebuiltTools(name, { bin, licenses, cache })

  await writeFile(path.join(runtime, 'manifest.json'), JSON.stringify({
    platform: name, minimumSystemVersion,
  }, null, 2))
  if (target === host) selfTest(runtime, exe)
  if (minimumSystemVersion) console.log(`Minimum macOS version required by the bundled tools: ${minimumSystemVersion}`)
}

await writeFile(path.join(resources, 'icon.png'), iconPNG())
console.log(`Runtime prepared for ${targets.join(', ')}.`)

// With a minimal PATH: the packaged app must never rely on the build machine's tools.
function selfTest(runtime, exe) {
  const bin = path.join(runtime, 'bin')
  const env = process.platform === 'win32'
    ? { PATH: bin, SystemRoot: process.env.SystemRoot, USERPROFILE: process.env.USERPROFILE }
    : { PATH: `${bin}:/usr/bin:/bin`, HOME: process.env.HOME, GGML_METAL_PATH_RESOURCES: path.join(runtime, 'lib') }
  const tools = [['whisper-cli', ['--help']], ['whisper-server', ['--help']]]
  if (process.platform !== 'linux') tools.push(['ffmpeg', ['-version']], ['ffprobe', ['-version']])
  for (const [name, args] of tools) execFileSync(path.join(bin, name + exe), args, { env, stdio: 'pipe' })
}
