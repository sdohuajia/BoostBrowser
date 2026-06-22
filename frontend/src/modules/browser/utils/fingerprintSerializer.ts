// 指纹参数序列化/反序列化工具

/**
 * 获取系统当前时区
 * @returns IANA 时区标识符，如 "Asia/Shanghai"
 */
export function getSystemTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

export interface FingerprintConfig {
  // 指纹种子（核心）
  seed?: string            // --fingerprint=<seed>  控制所有随机噪声的根种子

  // 基础身份
  brand?: string           // --fingerprint-brand=
  platform?: string        // --fingerprint-platform=
  lang?: string            // --lang=
  timezone?: string        // --timezone=

  // 屏幕与窗口
  resolution?: string      // --window-size=（预设值或 'custom'）
  customResolution?: string // 当 resolution === 'custom' 时使用
  colorDepth?: string      // --fingerprint-color-depth=

  // 硬件信息
  hardwareConcurrency?: string  // --fingerprint-hardware-concurrency=
  deviceMemory?: string         // --fingerprint-device-memory=

  // 渲染指纹
  canvasNoise?: boolean         // --fingerprint-canvas-noise=
  webglVendor?: string          // --fingerprint-webgl-vendor=
  webglRenderer?: string        // --fingerprint-webgl-renderer=
  audioNoise?: boolean          // --fingerprint-audio-noise=

  // 字体
  fonts?: string                // --fingerprint-fonts=

  // 网络与隐私
  webrtcPolicy?: string         // --webrtc-ip-handling-policy=
  doNotTrack?: boolean          // --fingerprint-do-not-track=

  // 媒体设备
  mediaDevices?: string         // --fingerprint-media-devices= (格式: "2,1,0" 摄像头,麦克风,扬声器)

  // 触摸
  touchPoints?: string          // --fingerprint-touch-points=

  unknownArgs?: string[]        // 无法识别的原始参数，原样保留
}

export const PRESET_RESOLUTIONS = ['1920,1080', '1440,900', '1366,768', '2560,1440', '1280,800', '1600,900']

// CLI 参数前缀 → FingerprintConfig 字段映射
export const KEY_MAP: Record<string, keyof FingerprintConfig> = {
  '--fingerprint': 'seed',
  '--fingerprint-brand': 'brand',
  '--fingerprint-platform': 'platform',
  '--lang': 'lang',
  '--timezone': 'timezone',
  '--window-size': 'resolution',
  '--fingerprint-color-depth': 'colorDepth',
  '--fingerprint-hardware-concurrency': 'hardwareConcurrency',
  '--fingerprint-device-memory': 'deviceMemory',
  '--fingerprint-canvas-noise': 'canvasNoise',
  '--fingerprint-webgl-vendor': 'webglVendor',
  '--fingerprint-webgl-renderer': 'webglRenderer',
  '--fingerprint-audio-noise': 'audioNoise',
  '--fingerprint-fonts': 'fonts',
  '--webrtc-ip-handling-policy': 'webrtcPolicy',
  '--fingerprint-do-not-track': 'doNotTrack',
  '--fingerprint-media-devices': 'mediaDevices',
  '--fingerprint-touch-points': 'touchPoints',
}

// FingerprintConfig → string[]
export function serialize(config: FingerprintConfig): string[] {
  const args: string[] = []
  if (config.seed) args.push(`--fingerprint=${config.seed}`)
  if (config.brand) args.push(`--fingerprint-brand=${config.brand}`)
  if (config.platform) args.push(`--fingerprint-platform=${config.platform}`)
  if (config.lang) args.push(`--lang=${config.lang}`)
  if (config.timezone) {
    // 如果是 system，替换为实际系统时区
    const tz = config.timezone === 'system' ? getSystemTimezone() : config.timezone
    args.push(`--timezone=${tz}`)
  }

  const res = config.resolution === 'custom' ? config.customResolution : config.resolution
  if (res) args.push(`--window-size=${res}`)

  if (config.colorDepth) args.push(`--fingerprint-color-depth=${config.colorDepth}`)
  if (config.hardwareConcurrency) args.push(`--fingerprint-hardware-concurrency=${config.hardwareConcurrency}`)
  if (config.deviceMemory) args.push(`--fingerprint-device-memory=${config.deviceMemory}`)

  if (config.canvasNoise !== undefined) args.push(`--fingerprint-canvas-noise=${config.canvasNoise}`)
  if (config.webglVendor) args.push(`--fingerprint-webgl-vendor=${config.webglVendor}`)
  if (config.webglRenderer) args.push(`--fingerprint-webgl-renderer=${config.webglRenderer}`)
  if (config.audioNoise !== undefined) args.push(`--fingerprint-audio-noise=${config.audioNoise}`)

  if (config.fonts) args.push(`--fingerprint-fonts=${config.fonts}`)

  if (config.webrtcPolicy) args.push(`--webrtc-ip-handling-policy=${config.webrtcPolicy}`)
  if (config.doNotTrack !== undefined) args.push(`--fingerprint-do-not-track=${config.doNotTrack}`)
  if (config.mediaDevices) args.push(`--fingerprint-media-devices=${config.mediaDevices}`)
  if (config.touchPoints) args.push(`--fingerprint-touch-points=${config.touchPoints}`)

  return [...args, ...(config.unknownArgs ?? [])]
}

// string[] → FingerprintConfig
export function deserialize(args: string[]): FingerprintConfig {
  const config: FingerprintConfig = { unknownArgs: [] }

  for (const arg of args) {
    const eqIdx = arg.indexOf('=')
    if (eqIdx === -1) {
      config.unknownArgs!.push(arg)
      continue
    }
    const key = arg.slice(0, eqIdx)
    const val = arg.slice(eqIdx + 1)
    const field = KEY_MAP[key]

    if (!field) {
      config.unknownArgs!.push(arg)
      continue
    }

    if (field === 'canvasNoise' || field === 'audioNoise' || field === 'doNotTrack') {
      (config as Record<string, unknown>)[field] = val === 'true'
    } else if (field === 'resolution') {
      if (PRESET_RESOLUTIONS.includes(val)) {
        config.resolution = val
      } else {
        config.resolution = 'custom'
        config.customResolution = val
      }
    } else {
      (config as Record<string, unknown>)[field] = val
    }
  }

  return config
}

// 生成随机指纹种子（32位正整数）
export function randomFingerprintSeed(): string {
  return String(Math.floor(Math.random() * 2147483647) + 1)
}

// ─── 完全随机身份 ────────────────────────────────────────────────────────────
// 每个 family 内部的字段相互兼容（避免 mac+NVIDIA 这类不真实组合）
interface FpFamily {
  platform: 'windows' | 'mac' | 'linux'
  langs: string[]
  timezones: string[]
  resolutions: string[]
  cores: string[]
  memories: string[]
  colorDepths: string[]
  webglVendors: string[]
  webglRenderers: Record<string, string[]>
  fontsList: string[]
  touchPoints: string[]
}

const FP_FAMILIES: FpFamily[] = [
  {
    platform: 'windows',
    langs: ['en-US', 'en-GB', 'zh-CN', 'ja-JP', 'ko-KR', 'fr-FR', 'de-DE'],
    timezones: ['America/New_York', 'America/Los_Angeles', 'America/Chicago', 'Europe/London', 'Europe/Paris', 'Europe/Berlin', 'Asia/Shanghai', 'Asia/Tokyo', 'Asia/Seoul'],
    resolutions: ['1920,1080', '1366,768', '1440,900', '1600,900', '2560,1440', '1280,800'],
    cores: ['4', '6', '8', '12', '16'],
    memories: ['4', '8', '16', '32'],
    colorDepths: ['24', '30'],
    webglVendors: ['Intel', 'NVIDIA', 'AMD'],
    webglRenderers: {
      Intel: ['Intel(R) UHD Graphics 630', 'Intel(R) UHD Graphics 620', 'Intel(R) HD Graphics 520', 'Intel(R) Iris(R) Xe Graphics'],
      NVIDIA: ['NVIDIA GeForce RTX 3080', 'NVIDIA GeForce RTX 3060', 'NVIDIA GeForce GTX 1660', 'NVIDIA GeForce GTX 1080 Ti'],
      AMD: ['AMD Radeon RX 6600', 'AMD Radeon RX 580', 'AMD Radeon Vega 8'],
    },
    fontsList: [
      'Arial,Helvetica,Times New Roman,Courier New,Verdana,Georgia,Tahoma',
      'Arial,Microsoft YaHei,SimSun,SimHei,Helvetica,Times New Roman',
      'Arial,Helvetica,Calibri,Cambria,Consolas,Times New Roman',
    ],
    touchPoints: ['0'],
  },
  {
    platform: 'mac',
    langs: ['en-US', 'zh-CN', 'ja-JP', 'fr-FR'],
    timezones: ['America/New_York', 'America/Los_Angeles', 'Europe/London', 'Asia/Shanghai', 'Asia/Tokyo'],
    resolutions: ['1440,900', '2560,1440', '1920,1080', '2880,1800'],
    cores: ['8', '10', '12', '16'],
    memories: ['8', '16', '32'],
    colorDepths: ['24', '30'],
    webglVendors: ['Apple', 'Intel'],
    webglRenderers: {
      Apple: ['Apple M1', 'Apple M2', 'Apple M3'],
      Intel: ['Intel(R) Iris(R) Plus Graphics', 'Intel(R) UHD Graphics 630'],
    },
    fontsList: [
      'Arial,Helvetica,PingFang SC,Hiragino Sans GB,STHeiti,Times New Roman',
      'Arial,Helvetica Neue,Lucida Grande,Times New Roman,Courier',
      'SF Pro Display,Helvetica,Arial,Times New Roman',
    ],
    touchPoints: ['0'],
  },
  {
    platform: 'linux',
    langs: ['en-US', 'zh-CN', 'de-DE', 'fr-FR'],
    timezones: ['Europe/Berlin', 'Europe/Paris', 'America/New_York', 'Asia/Shanghai'],
    resolutions: ['1920,1080', '1366,768', '1600,900', '2560,1440'],
    cores: ['4', '8', '12', '16'],
    memories: ['4', '8', '16', '32'],
    colorDepths: ['24'],
    webglVendors: ['Intel', 'NVIDIA', 'AMD'],
    webglRenderers: {
      Intel: ['Mesa Intel(R) UHD Graphics 630', 'Mesa Intel(R) HD Graphics 520'],
      NVIDIA: ['GeForce RTX 3060/PCIe/SSE2', 'GeForce GTX 1660/PCIe/SSE2'],
      AMD: ['AMD Radeon RX 580 (POLARIS10, DRM 3.42.0, 5.15.0, LLVM 13.0.1)'],
    },
    fontsList: [
      'DejaVu Sans,Liberation Sans,Ubuntu,Arial,Times New Roman',
      'Noto Sans,DejaVu Sans,Arial,Helvetica,Times New Roman',
    ],
    touchPoints: ['0'],
  },
]

function pickRand<T>(arr: T[]): T {
  return arr[Math.floor(Math.random() * arr.length)]
}

/**
 * 生成完全随机的指纹配置（家族级，platform 与 GPU/语言/时区互相匹配）
 * 同时刷新种子。返回的 config 直接覆盖到 FingerprintPanel。
 */
export function randomFingerprintConfig(preserveUnknownArgs?: string[]): FingerprintConfig {
  const fam = pickRand(FP_FAMILIES)
  const vendor = pickRand(fam.webglVendors)
  const renderer = fam.webglRenderers[vendor] ? pickRand(fam.webglRenderers[vendor]) : ''
  const resolution = pickRand(fam.resolutions)

  return {
    seed: randomFingerprintSeed(),
    brand: 'Chrome',
    platform: fam.platform,
    lang: pickRand(fam.langs),
    timezone: pickRand(fam.timezones),
    resolution: PRESET_RESOLUTIONS.includes(resolution) ? resolution : 'custom',
    customResolution: PRESET_RESOLUTIONS.includes(resolution) ? undefined : resolution,
    colorDepth: pickRand(fam.colorDepths),
    hardwareConcurrency: pickRand(fam.cores),
    deviceMemory: pickRand(fam.memories),
    canvasNoise: true,
    audioNoise: true,
    webglVendor: vendor,
    webglRenderer: renderer || undefined,
    fonts: pickRand(fam.fontsList),
    webrtcPolicy: 'disable_non_proxied_udp',
    doNotTrack: false,
    touchPoints: pickRand(fam.touchPoints),
    unknownArgs: preserveUnknownArgs ?? [],
  }
}

// ─── 预设指纹配置 ────────────────────────────────────────────────────────────

export interface FingerprintPreset {
  id: string
  name: string
  description: string
  config: Partial<FingerprintConfig>
}

export const FINGERPRINT_PRESETS: FingerprintPreset[] = [
  {
    id: 'win-chrome-office',
    name: 'Windows / Chrome / 办公',
    description: '模拟国内办公室 Windows 用户，中文环境，1920x1080',
    config: {
      brand: 'Chrome',
      platform: 'windows',
      lang: 'zh-CN',
      timezone: 'Asia/Shanghai',
      resolution: '1920,1080',
      colorDepth: '24',
      hardwareConcurrency: '4',
      deviceMemory: '8',
      canvasNoise: true,
      audioNoise: true,
      webglVendor: 'Intel',
      webglRenderer: 'Intel(R) UHD Graphics 630',
      fonts: 'Arial,Microsoft YaHei,SimSun,SimHei,Helvetica,Times New Roman',
      webrtcPolicy: 'disable_non_proxied_udp',
      doNotTrack: false,
      touchPoints: '0',
    },
  },
  {
    id: 'win-chrome-gaming',
    name: 'Windows / Chrome / 游戏主机',
    description: '模拟高配游戏 PC，NVIDIA 显卡，2560x1440',
    config: {
      brand: 'Chrome',
      platform: 'windows',
      lang: 'en-US',
      timezone: 'America/New_York',
      resolution: '2560,1440',
      colorDepth: '24',
      hardwareConcurrency: '16',
      deviceMemory: '16',
      canvasNoise: true,
      audioNoise: true,
      webglVendor: 'NVIDIA',
      webglRenderer: 'NVIDIA GeForce RTX 3080',
      fonts: 'Arial,Helvetica,Times New Roman,Courier New,Verdana',
      webrtcPolicy: 'disable_non_proxied_udp',
      doNotTrack: false,
      touchPoints: '0',
    },
  },
  {
    id: 'mac-chrome-designer',
    name: 'macOS / Chrome / 设计师',
    description: '模拟 Mac 设计师用户，Apple GPU，Retina 分辨率',
    config: {
      brand: 'Chrome',
      platform: 'mac',
      lang: 'zh-CN',
      timezone: 'Asia/Shanghai',
      resolution: '2560,1440',
      colorDepth: '30',
      hardwareConcurrency: '10',
      deviceMemory: '16',
      canvasNoise: true,
      audioNoise: true,
      webglVendor: 'Apple',
      webglRenderer: 'Apple M2',
      fonts: 'Arial,Helvetica,PingFang SC,Hiragino Sans GB,STHeiti,Times New Roman',
      webrtcPolicy: 'disable_non_proxied_udp',
      doNotTrack: true,
      touchPoints: '0',
    },
  },
  {
    id: 'win-edge-enterprise',
    name: 'Windows / Edge / 企业',
    description: '模拟企业 Windows 用户，Edge 浏览器，标准配置',
    config: {
      brand: 'Edge',
      platform: 'windows',
      lang: 'zh-CN',
      timezone: 'Asia/Shanghai',
      resolution: '1366,768',
      colorDepth: '24',
      hardwareConcurrency: '4',
      deviceMemory: '4',
      canvasNoise: true,
      audioNoise: false,
      webglVendor: 'Intel',
      webglRenderer: 'Intel(R) HD Graphics 520',
      fonts: 'Arial,Microsoft YaHei,Calibri,Segoe UI,Times New Roman',
      webrtcPolicy: 'default_public_interface_only',
      doNotTrack: false,
      touchPoints: '0',
    },
  },
  {
    id: 'win-chrome-us-user',
    name: 'Windows / Chrome / 美国用户',
    description: '模拟美国普通用户，英文环境，AMD 显卡',
    config: {
      brand: 'Chrome',
      platform: 'windows',
      lang: 'en-US',
      timezone: 'America/Los_Angeles',
      resolution: '1920,1080',
      colorDepth: '24',
      hardwareConcurrency: '8',
      deviceMemory: '8',
      canvasNoise: true,
      audioNoise: true,
      webglVendor: 'AMD',
      webglRenderer: 'AMD Radeon RX 6600',
      fonts: 'Arial,Helvetica,Times New Roman,Courier New,Georgia',
      webrtcPolicy: 'disable_non_proxied_udp',
      doNotTrack: false,
      touchPoints: '0',
    },
  },
  {
    id: 'mac-safari-jp',
    name: 'macOS / Safari / 日本用户',
    description: '模拟日本 Mac 用户，Safari 风格，日语环境',
    config: {
      brand: 'Safari',
      platform: 'mac',
      lang: 'ja-JP',
      timezone: 'Asia/Tokyo',
      resolution: '1440,900',
      colorDepth: '24',
      hardwareConcurrency: '8',
      deviceMemory: '8',
      canvasNoise: true,
      audioNoise: true,
      webglVendor: 'Apple',
      webglRenderer: 'Apple M1',
      fonts: 'Arial,Helvetica,Hiragino Kaku Gothic ProN,Yu Gothic,Times New Roman',
      webrtcPolicy: 'disable_non_proxied_udp',
      doNotTrack: true,
      touchPoints: '0',
    },
  },
  {
    id: 'win-chrome-uk-office',
    name: 'Windows / Chrome / 英国-办公',
    description: '模拟英国办公室 Windows 用户，英文环境 (en-GB)',
    config: {
      brand: 'Chrome',
      platform: 'windows',
      lang: 'en-GB',
      timezone: 'Europe/London',
      resolution: '1920,1080',
      colorDepth: '24',
      hardwareConcurrency: '8',
      deviceMemory: '8',
      canvasNoise: true,
      audioNoise: true,
      webglVendor: 'Intel',
      webglRenderer: 'Intel(R) UHD Graphics 630',
      fonts: 'Arial,Helvetica,Times New Roman,Courier New,Verdana',
      webrtcPolicy: 'disable_non_proxied_udp',
      doNotTrack: false,
      touchPoints: '0',
    },
  },
  {
    id: 'mac-chrome-us-edu',
    name: 'macOS / Chrome / 美国-教育',
    description: '模拟美国大学教育网 Mac 用户，英文环境 (en-US)',
    config: {
      brand: 'Chrome',
      platform: 'mac',
      lang: 'en-US',
      timezone: 'America/New_York',
      resolution: '1440,900',
      colorDepth: '24',
      hardwareConcurrency: '8',
      deviceMemory: '8',
      canvasNoise: true,
      audioNoise: true,
      webglVendor: 'Apple',
      webglRenderer: 'Apple M1',
      fonts: 'Arial,Helvetica,Times New Roman,Courier New,Georgia',
      webrtcPolicy: 'disable_non_proxied_udp',
      doNotTrack: false,
      touchPoints: '0',
    },
  },
]
