import { spawnSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import process from 'node:process'

const require = createRequire(import.meta.url)
const SKIP_ENV = 'ANT_SKIP_ROLLUP_NATIVE_INSTALL'

function isMusl() {
  try {
    return !process.report?.getReport().header.glibcVersionRuntime
  } catch {
    return false
  }
}

function isMingw32() {
  try {
    return process.report?.getReport().header.osName.startsWith('MINGW32_NT')
  } catch {
    return false
  }
}

function resolveRollupPackageBase() {
  const platformArchMap = {
    android: {
      arm: { base: 'android-arm-eabi' },
      arm64: { base: 'android-arm64' },
    },
    darwin: {
      arm64: { base: 'darwin-arm64' },
      x64: { base: 'darwin-x64' },
    },
    freebsd: {
      arm64: { base: 'freebsd-arm64' },
      x64: { base: 'freebsd-x64' },
    },
    linux: {
      arm: { base: 'linux-arm-gnueabihf', musl: 'linux-arm-musleabihf' },
      arm64: { base: 'linux-arm64-gnu', musl: 'linux-arm64-musl' },
      loong64: { base: 'linux-loong64-gnu', musl: null },
      ppc64: { base: 'linux-ppc64-gnu', musl: null },
      riscv64: { base: 'linux-riscv64-gnu', musl: 'linux-riscv64-musl' },
      s390x: { base: 'linux-s390x-gnu', musl: null },
      x64: { base: 'linux-x64-gnu', musl: 'linux-x64-musl' },
    },
    openharmony: {
      arm64: { base: 'openharmony-arm64' },
    },
    win32: {
      arm64: { base: 'win32-arm64-msvc' },
      ia32: { base: 'win32-ia32-msvc' },
      x64: { base: isMingw32() ? 'win32-x64-gnu' : 'win32-x64-msvc' },
    },
  }

  const target = platformArchMap[process.platform]?.[process.arch]
  if (!target) {
    return null
  }

  if ('musl' in target && isMusl()) {
    return target.musl
  }

  return target.base
}

function getNpmInvocation() {
  if (process.env.npm_execpath) {
    return {
      command: process.execPath,
      args: [process.env.npm_execpath],
    }
  }

  return {
    command: process.platform === 'win32' ? 'npm.cmd' : 'npm',
    args: [],
  }
}

function loadRollupPackageJson() {
  const rollupEntry = require.resolve('rollup')
  const packagePath = join(dirname(rollupEntry), '..', 'package.json')
  return JSON.parse(readFileSync(packagePath, 'utf8'))
}

function ensureRollupNative() {
  if (process.env[SKIP_ENV] === '1') {
    return
  }

  let rollupPackage
  try {
    rollupPackage = loadRollupPackageJson()
  } catch {
    return
  }

  const packageBase = resolveRollupPackageBase()
  if (!packageBase) {
    return
  }

  const packageName = `@rollup/rollup-${packageBase}`
  const version = rollupPackage.optionalDependencies?.[packageName]
  if (!version) {
    return
  }

  try {
    require.resolve(packageName)
    return
  } catch {
    console.log(`[postinstall] Missing ${packageName}, installing ${packageName}@${version}`)
  }

  const npm = getNpmInvocation()
  const result = spawnSync(
    npm.command,
    [...npm.args, 'install', '--no-save', `${packageName}@${version}`],
    {
      cwd: process.cwd(),
      stdio: 'inherit',
      env: {
        ...process.env,
        [SKIP_ENV]: '1',
      },
    }
  )

  if (result.status !== 0) {
    process.exit(result.status ?? 1)
  }
}

ensureRollupNative()
