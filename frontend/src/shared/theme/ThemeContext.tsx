import { createContext, useContext, useEffect, useState, ReactNode } from 'react'
import { ThemeType, DEFAULT_THEME } from './types'

interface ThemeContextValue {
  theme: ThemeType
  setTheme: (theme: ThemeType) => void
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

const THEME_STORAGE_KEY = 'app-theme'

interface ThemeProviderProps {
  children: ReactNode
  defaultTheme?: ThemeType
}

export function ThemeProvider({ children, defaultTheme = DEFAULT_THEME }: ThemeProviderProps) {
  const [theme, setThemeState] = useState<ThemeType>(() => {
    // 从 localStorage 读取保存的主题
    const saved = localStorage.getItem(THEME_STORAGE_KEY)
    if (saved && ['dark', 'light', 'cream', 'mint', 'ocean'].includes(saved)) {
      return saved as ThemeType
    }
    return defaultTheme
  })

  const setTheme = (newTheme: ThemeType) => {
    setThemeState(newTheme)
    localStorage.setItem(THEME_STORAGE_KEY, newTheme)
  }

  // 应用主题到 document
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
  }, [theme])

  return (
    <ThemeContext.Provider value={{ theme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return context
}
