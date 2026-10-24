import { chmod, cp, lstat, readdir, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { checksumOf, download, extract, text } from './common.mjs'

// Official prebuilt tools for the platforms Homebrew cannot supply.
// Windows gets BtbN FFmpeg-Builds, GPL shared builds: ffmpeg and ffprobe share
// one copy of the libraries, half the size of the static builds. Linux
// packages depend on the distribution's FFmpeg instead.
const ffmpegChannel = { release: 'n9.0', version: '9.0' }
const whisperRelease = 'b5454' // ggml-org/whisper.cpp CPU builds, checksums from the release
const platforms = {
  'windows-amd64': { ffmpeg: 'win64', whisper: ['whisper-bin-x64.zip', '6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb'] },
  'windows-arm64': { ffmpeg: 'winarm64', whisper: ['whisper-bin-win-cpu-arm64.zip', '28c37e7b598c3d9bbfef94f3bd67f2ee6f12b7c86f3da5edcf5e4308d615ff6d'] },
  'linux-amd64': { whisper: ['whisper-bin-ubuntu-x64.tar.gz', 'a72becf15d7917f990f6313867a52638b82b7f9ef237fb0c980dac56a135781c'] },
  'linux-arm64': { whisper: ['whisper-bin-ubuntu-arm64.tar.gz', '6b95ebfc60447df48e70ef00a73bdc3f41679ed2d01ef465827206a2ff325149'] },
}

export const supportsPrebuilt = target => target in platforms

// Finds the shallowest archive entry named `name`, such as the top-level
// LICENSE rather than one of a bundled dependency.
async function find(directory, name) {
  const matches = (await readdir(directory, { recursive: true, withFileTypes: true }))
    .filter(entry => entry.name === name && !entry.isDirectory())
    .map(entry => path.join(entry.parentPath, entry.name))
    .sort((a, b) => a.split(path.sep).length - b.split(path.sep).length)
  if (!matches.length) throw new Error(`${name} is missing from the downloaded archive`)
  return matches[0]
}

async function install(source, destination, windows) {
  await cp(source, destination, { verbatimSymlinks: true })
  // Library version symlinks keep their relative targets and need no mode.
  if (!windows && !(await lstat(destination)).isSymbolicLink()) await chmod(destination, 0o755)
}

export async function installPrebuiltTools(target, { bin, licenses, cache }) {
  const platform = platforms[target]
  const windows = target.startsWith('windows')
  const exe = windows ? '.exe' : ''
  const packages = []
  const extracted = []
  try {
    // FFmpeg and ffprobe with their DLLs beside them, on Windows only.
    if (windows) {
      const ffmpegRelease = 'https://github.com/BtbN/FFmpeg-Builds/releases/download/latest'
      const ffmpegName = `ffmpeg-${ffmpegChannel.release}-latest-${platform.ffmpeg}-gpl-shared-${ffmpegChannel.version}.zip`
      const ffmpegArchive = await download(`${ffmpegRelease}/${ffmpegName}`, path.join(cache, ffmpegName),
        checksumOf(await text(`${ffmpegRelease}/checksums.sha256`), ffmpegName))
      const ffmpeg = await extract(ffmpegArchive)
      extracted.push(ffmpeg)
      const ffmpegBin = path.dirname(await find(ffmpeg, 'ffmpeg.exe'))
      for (const name of ['ffmpeg', 'ffprobe']) await install(path.join(ffmpegBin, name + exe), path.join(bin, name + exe), windows)
      for (const name of await readdir(ffmpegBin)) {
        if (name.endsWith('.dll')) await install(path.join(ffmpegBin, name), path.join(bin, name), windows)
      }
      await cp(await find(ffmpeg, 'LICENSE.txt'), path.join(licenses, 'ffmpeg-LICENSE.txt'))
      packages.push({ name: 'ffmpeg', version: ffmpegChannel.release, source: ffmpegName, license: 'GPL-3.0-or-later' })
    }

    // whisper.cpp: the CLI with the shared libraries and CPU backends beside it.
    const [whisperName, whisperSHA] = platform.whisper
    const whisperArchive = await download(`https://github.com/ggml-org/whisper.cpp/releases/download/${whisperRelease}/${whisperName}`,
      path.join(cache, `${whisperRelease}-${whisperName}`), whisperSHA)
    const whisper = await extract(whisperArchive)
    extracted.push(whisper)
    for (const name of ['whisper-cli', 'whisper-server']) {
      const executable = await find(whisper, name + exe)
      await install(executable, path.join(bin, name + exe), windows)
    }
    const whisperBin = path.dirname(await find(whisper, 'whisper-cli' + exe))
    for (const name of await readdir(whisperBin)) {
      if (windows ? name.endsWith('.dll') && name !== 'SDL2.dll' : /^lib.+\.so(\.\d+)*$/.test(name)) {
        await install(path.join(whisperBin, name), path.join(bin, name), windows)
      }
    }
    await writeFile(path.join(licenses, 'whisper.cpp-LICENSE'),
      await text(`https://raw.githubusercontent.com/ggml-org/whisper.cpp/${whisperRelease}/LICENSE`))
    packages.push({ name: 'whisper.cpp', version: whisperRelease, source: whisperName, license: 'MIT' })
  } finally {
    await Promise.all(extracted.map(directory => rm(directory, { recursive: true, force: true })))
  }
  await writeFile(path.join(licenses, 'native-packages.json'), JSON.stringify(packages, null, 2))
}
