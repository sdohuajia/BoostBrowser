import { ReactNode } from 'react'
import { CheckCircle, XCircle, AlertCircle, Info, X } from 'lucide-react'
import clsx from 'clsx'

type AlertType = 'success' | 'error' | 'warning' | 'info'

interface AlertProps {
  type?: AlertType
  title?: string
  message: ReactNode
  closable?: boolean
  onClose?: () => void
  showIcon?: boolean
  className?: string
}

const icons = {
  success: CheckCircle,
  error: XCircle,
  warning: AlertCircle,
  info: Info,
}

const styles = {
  success: 'bg-[var(--color-success)]/15 border-[var(--color-success)]/30 text-[var(--color-success)]',
  error: 'bg-[var(--color-error)]/15 border-[var(--color-error)]/30 text-[var(--color-error)]',
  warning: 'bg-[var(--color-warning)]/15 border-[var(--color-warning)]/30 text-[var(--color-warning)]',
  info: 'bg-[var(--color-accent)]/15 border-[var(--color-accent)]/30 text-[var(--color-accent)]',
}

export function Alert({
  type = 'info',
  title,
  message,
  closable = false,
  onClose,
  showIcon = true,
  className,
}: AlertProps) {
  const Icon = icons[type]

  return (
    <div
      className={clsx(
        'flex gap-3 p-4 rounded-lg border',
        styles[type],
        className
      )}
    >
      {showIcon && <Icon className="w-5 h-5 flex-shrink-0 mt-0.5" />}
      
      <div className="flex-1 min-w-0">
        {title && (
          <h4 className="font-semibold mb-1">{title}</h4>
        )}
        <div className="text-sm text-[var(--color-text-secondary)]">
          {message}
        </div>
      </div>

      {closable && (
        <button
          onClick={onClose}
          className="p-0.5 rounded hover:bg-black/10 transition-colors flex-shrink-0"
        >
          <X className="w-4 h-4" />
        </button>
      )}
    </div>
  )
}
