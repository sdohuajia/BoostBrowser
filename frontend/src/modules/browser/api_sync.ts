// Window sync API bindings

const getBindings = async () => {
  try {
    return await import('../../wailsjs/go/main/App')
  } catch {
    return null
  }
}

// ============================================================================
// Sync API — 窗口同步
// ============================================================================

export interface SyncProfileInfo {
  profileId: string
  profileName: string
  pid: number
  debugPort: number
  hwnd: number
  running: boolean
  status: string // "running" | "no_window" | "stopped"
}

export interface SyncStatus {
  active: boolean
  masterId: string
  followerIds: string[]
  mouseEnabled: boolean
  keyEnabled: boolean
}

export type TileLayoutMode = 'grid' | 'horizontal' | 'vertical'

export interface TileWindowsResult {
  count: number
  tiledIds: string[]
  layout: TileLayoutMode
}

export async function getSyncProfiles(): Promise<SyncProfileInfo[]> {
  const bindings: any = await getBindings()
  if (bindings?.GetSyncProfiles) {
    try {
      return (await bindings.GetSyncProfiles()) || []
    } catch {
      return []
    }
  }
  return []
}

export async function startInputSync(masterProfileId: string, followerProfileIds: string[]): Promise<string | null> {
  const bindings: any = await getBindings()
  if (bindings?.StartInputSync) {
    try {
      await bindings.StartInputSync(masterProfileId, followerProfileIds)
      return null
    } catch (e: any) {
      return e?.message || String(e)
    }
  }
  return 'Wails 绑定不可用'
}

export async function stopInputSync(): Promise<string | null> {
  const bindings: any = await getBindings()
  if (bindings?.StopInputSync) {
    try {
      await bindings.StopInputSync()
      return null
    } catch (e: any) {
      return e?.message || String(e)
    }
  }
  return 'Wails 绑定不可用'
}

export async function getSyncStatus(): Promise<SyncStatus | null> {
  const bindings: any = await getBindings()
  if (bindings?.GetSyncStatus) {
    try {
      return await bindings.GetSyncStatus()
    } catch {
      return null
    }
  }
  return null
}

export async function updateSyncConfig(mouseEnabled: boolean, keyEnabled: boolean): Promise<string | null> {
  const bindings: any = await getBindings()
  if (bindings?.UpdateSyncConfig) {
    try {
      await bindings.UpdateSyncConfig(mouseEnabled, keyEnabled)
      return null
    } catch (e: any) {
      return e?.message || String(e)
    }
  }
  return 'Wails 绑定不可用'
}

export async function syncTileWindows(
  profileIds: string[],
  masterProfileId?: string,
  layoutMode: TileLayoutMode = 'grid',
): Promise<TileWindowsResult | null> {
  const bindings: any = await getBindings()
  if (bindings?.SyncTileWindows) {
    try {
      return await bindings.SyncTileWindows(profileIds, masterProfileId || '', layoutMode)
    } catch {
      return null
    }
  }
  return null
}

export async function syncCloseAll(profileIds: string[]): Promise<string[]> {
  const bindings: any = await getBindings()
  if (bindings?.SyncCloseAll) {
    try {
      return (await bindings.SyncCloseAll(profileIds)) || []
    } catch {
      return []
    }
  }
  return []
}