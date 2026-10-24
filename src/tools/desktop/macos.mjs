import { realpath, readdir, cp, chmod, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { exists, run } from './common.mjs'

const locate = name => run('/usr/bin/which', [name])

// Bundles Homebrew's FFmpeg and whisper.cpp for Apple silicon. Every
// non-system Mach-O dependency is copied and its load path rewritten, so the
// app never depends on /opt/homebrew at run time.
export async function bundleMacTools({ bin, lib, licenses }) {
  // Rewrite every non-system Mach-O dependency, including transitive libraries.
  // Copying Homebrew executables alone would still depend on /opt/homebrew.
  const installed = new Map()
  const names = new Map()
  const formulas = new Set()
  let minimumSystemVersion = '13.0'
  const versionParts = value => value.split('.').map(Number)
  const newerVersion = (left, right) => {
    const a = versionParts(left)
    const b = versionParts(right)
    for (let i = 0; i < Math.max(a.length, b.length); i++) {
      if ((a[i] || 0) !== (b[i] || 0)) return (a[i] || 0) > (b[i] || 0)
    }
    return false
  }
  function rpaths(source) {
    const lines = run('/usr/bin/otool', ['-arch', 'arm64', '-l', source])
    return [...lines.matchAll(/cmd LC_RPATH\s+cmdsize \d+\s+path (.+?) \(offset/g)].map(match => match[1])
  }
  function expand(value, source, executable) {
    return value.replace('@loader_path', path.dirname(source)).replace('@executable_path', path.dirname(executable))
  }
  async function resolveDependency(value, source, executable, inherited) {
    if (value.startsWith('/')) return value
    if (value.startsWith('@rpath/')) {
      for (const base of [...rpaths(source).map(v => expand(v, source, executable)), ...inherited]) {
        const candidate = path.join(base, value.slice(7))
        if (await exists(candidate)) return candidate
      }
      throw new Error(`Cannot resolve ${value} in ${source}`)
    }
    return expand(value, source, executable)
  }
  async function bundleMachO(input, destination, executable = input, inherited = []) {
    const source = await realpath(input)
    if (installed.has(source)) return installed.get(source)
    const name = path.basename(destination)
    if (names.has(name) && names.get(name) !== source) throw new Error(`Library name collision: ${name}`)
    installed.set(source, destination)
    names.set(name, source)
    const formula = source.match(/\/Cellar\/([^/]+)\/[^/]+\//)?.[1]
    if (formula) formulas.add(formula)
    const commands = run('/usr/bin/otool', ['-arch', 'arm64', '-l', source])
    const minimum = commands.match(/cmd LC_BUILD_VERSION[\s\S]*?\bminos ([\d.]+)/)?.[1]
      || commands.match(/cmd LC_VERSION_MIN_MACOSX[\s\S]*?\bversion ([\d.]+)/)?.[1]
    if (minimum && newerVersion(minimum, minimumSystemVersion)) minimumSystemVersion = minimum
    await cp(source, destination)
    await chmod(destination, 0o755)
    const search = [...rpaths(source).map(v => expand(v, source, executable)), ...inherited]
    const dependencies = run('/usr/bin/otool', ['-arch', 'arm64', '-L', source]).split('\n').slice(1)
      .map(line => line.trim().split(' (compatibility version')[0])
    for (const dependency of new Set(dependencies)) {
      if (dependency.startsWith('/usr/lib/') || dependency.startsWith('/System/Library/')) continue
      const resolved = await resolveDependency(dependency, source, executable, search)
      if (await realpath(resolved) === source) continue // the dylib's own install name
      const bundled = await bundleMachO(resolved, path.join(lib, path.basename(resolved)), executable, search)
      const relative = path.relative(path.dirname(destination), bundled)
      run('/usr/bin/install_name_tool', ['-change', dependency, `@loader_path/${relative}`, destination])
    }
    if (destination.endsWith('.dylib')) {
      run('/usr/bin/install_name_tool', ['-id', `@rpath/${name}`, destination])
    }
    // Remove build-machine search paths so missing dependencies cannot be masked.
    for (const value of rpaths(source)) {
      run('/usr/bin/install_name_tool', ['-delete_rpath', value, destination])
    }
    run('/usr/bin/codesign', ['--force', '--sign', '-', destination])
    return destination
  }

  for (const name of ['ffmpeg', 'ffprobe', 'whisper-cli', 'whisper-server']) {
    const source = locate(name)
    console.log(`Bundling ${name} and its native dependencies`)
    await bundleMachO(source, path.join(bin, name), source)
  }

  // GGML can load its CPU/Metal backend dynamically rather than through LC_LOAD_DYLIB.
  const ggmlPrefix = run('brew', ['--prefix', 'ggml'])
  for (const name of await readdir(path.join(ggmlPrefix, 'lib'))) {
    if (/^libggml-(cpu|metal).*\.dylib$/.test(name)) {
      await bundleMachO(path.join(ggmlPrefix, 'lib', name), path.join(lib, name))
    }
  }
  // GGML searches the executable's directory when its build-machine directory
  // does not exist. Homebrew installs these loadable backends as .so files.
  const backendDir = path.join(ggmlPrefix, 'libexec')
  if (await exists(backendDir)) {
    for (const name of await readdir(backendDir)) {
      if (/^libggml-.+\.so$/.test(name)) {
        await bundleMachO(path.join(backendDir, name), path.join(bin, name))
      }
    }
  }

  await writeFile(path.join(licenses, 'native-packages.json'), JSON.stringify(
    JSON.parse(run('brew', ['info', '--json=v2', ...formulas])).formulae.map(formula => ({
      name: formula.name, version: formula.versions.stable, license: formula.license, homepage: formula.homepage,
    })), null, 2,
  ))
  for (const formula of formulas) {
    const prefix = run('brew', ['--prefix', formula])
    for (const name of await readdir(prefix)) {
      if (/^(LICENSE|COPYING|NOTICE|AUTHORS)/i.test(name)) {
        await cp(path.join(prefix, name), path.join(licenses, `${formula}-${name}`), { recursive: true })
      }
    }
  }
  return { minimumSystemVersion }
}
