import { create } from 'zustand'

interface ImportStatePayload {
  inProgress: boolean
  progress?: number
  message?: string
}

interface BackupState {
  importInProgress: boolean
  importProgress: number
  importMessage: string
  setImportState: (payload: ImportStatePayload) => void
  clearImportState: () => void
}

export const useBackupStore = create<BackupState>((set) => ({
  importInProgress: false,
  importProgress: 0,
  importMessage: '',
  setImportState: ({ inProgress, progress = 0, message = '' }) =>
    set({
      importInProgress: inProgress,
      importProgress: Math.max(0, Math.min(100, Math.round(progress))),
      importMessage: message.trim(),
    }),
  clearImportState: () =>
    set({
      importInProgress: false,
      importProgress: 0,
      importMessage: '',
    }),
}))
