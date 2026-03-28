import { createContext, useContext, useState, useCallback } from 'react'

interface DashboardParamsContextValue {
  params: Record<string, string>
  setParam: (name: string, value: string) => void
}

const DashboardParamsContext = createContext<DashboardParamsContextValue>({
  params: {},
  setParam: () => {},
})

export function DashboardParamsProvider({ children }: { children: React.ReactNode }) {
  const [params, setParams] = useState<Record<string, string>>({})
  const setParam = useCallback((name: string, value: string) => {
    setParams(prev => ({ ...prev, [name]: value }))
  }, [])
  return (
    <DashboardParamsContext.Provider value={{ params, setParam }}>
      {children}
    </DashboardParamsContext.Provider>
  )
}

export function useDashboardParams() {
  return useContext(DashboardParamsContext)
}
