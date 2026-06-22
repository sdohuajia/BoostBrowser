import { ReactNode } from 'react'
import { Sidebar } from './Sidebar'
import { Topbar } from './Topbar'

interface LayoutProps {
  children: ReactNode
  syncPanelMode?: boolean
}

export function Layout({ children, syncPanelMode = false }: LayoutProps) {
  if (syncPanelMode) {
    return (
      <div className="h-screen overflow-hidden bg-transparent">
        <main className="h-full overflow-auto p-0">
          {children}
        </main>
      </div>
    )
  }

  return (
    <div className="flex h-screen bg-[var(--color-bg-base)]">
      <Sidebar />
      <div className="flex-1 flex flex-col overflow-hidden min-w-0">
        <Topbar />
        <main className="flex-1 overflow-auto p-5">
          {children}
        </main>
      </div>
    </div>
  )
}
