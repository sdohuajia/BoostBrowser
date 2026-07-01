import type { DashboardStats } from './types'

const getBindings = async () => {
  try {
    return await import('../../wailsjs/go/main/App')
  } catch {
    return null
  }
}

export async function fetchDashboardStats(): Promise<DashboardStats> {
  const bindings: any = await getBindings()
  if (bindings?.GetDashboardStats) {
    try {
      const data = await bindings.GetDashboardStats()
      const licenseStatus = bindings.GetLicenseStatus ? await bindings.GetLicenseStatus() : { maxLimit: 20 }
      return {
        totalInstances: data?.totalInstances ?? 0,
        runningInstances: data?.runningInstances ?? 0,
        proxyCount: data?.proxyCount ?? 0,
        coreCount: data?.coreCount ?? 0,
        memUsedMB: data?.memUsedMB ?? 0,
        maxProfileLimit: licenseStatus?.maxLimit ?? 20,
        appVersion: data?.appVersion ?? 'unknown',
      }
    } catch (e) {
      console.error('fetchDashboardStats error:', e)
    }
  }
  return { totalInstances: 0, runningInstances: 0, proxyCount: 0, coreCount: 0, memUsedMB: 0, maxProfileLimit: 20, appVersion: 'unknown' }
}

export async function redeemCDKey(cdkey: string): Promise<{ success: boolean, message?: string }> {
  const bindings: any = await getBindings()
  if (bindings?.RedeemCDKey) {
    try {
      await bindings.RedeemCDKey(cdkey)
      return { success: true }
    } catch (e: any) {
      return { success: false, message: e.message || '兑换失败' }
    }
  }
  return { success: false, message: '系统 API 未就绪' }
}

export async function redeemGithubStar(): Promise<{ success: boolean, message?: string }> {
  const bindings: any = await getBindings()
  if (bindings?.RedeemGithubStar) {
    try {
      await bindings.RedeemGithubStar()
      return { success: true }
    } catch (e: any) {
      return { success: false, message: e.message || '领取失败' }
    }
  }
  return { success: false, message: '系统 API 未就绪' }
}

export async function reloadConfig(): Promise<void> {
  const bindings: any = await getBindings()
  if (bindings?.ReloadConfig) {
    try {
      await bindings.ReloadConfig()
    } catch (e) {
      console.error('reloadConfig error:', e)
    }
  }
}

export async function generateCDKeys(count: number): Promise<{ success: boolean, keys: string[], message?: string }> {
  const bindings: any = await getBindings()
  if (bindings?.GenerateCDKeys) {
    try {
      const keys = await bindings.GenerateCDKeys(count)
      return { success: true, keys: keys || [] }
    } catch (e: any) {
      return { success: false, keys: [], message: e.message || '生成失败' }
    }
  }
  return { success: false, keys: [], message: '系统 API 未就绪' }
}
