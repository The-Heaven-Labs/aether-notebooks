import React from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useAuth, useAuthProvider, AuthContext } from './hooks/useAuth'
import { LoginPage } from './pages/LoginPage'
import { HomePage } from './pages/HomePage'
import { NotebookPage } from './pages/NotebookPage'
import { ConnectorsPage } from './pages/ConnectorsPage'
import { DashboardsPage } from './pages/DashboardsPage'
import { DashboardEditorPage } from './pages/DashboardEditorPage'
import { DashboardPage } from './pages/DashboardPage'
import { AuditPage } from './pages/AuditPage'
import { MembersPage } from './pages/MembersPage'
import { AdminPage } from './pages/AdminPage'
import { PublicDashboardPage } from './pages/PublicDashboardPage'
import { PresentationPage } from './pages/PresentationPage'
import { OrgOnboardingPage } from './pages/OrgOnboardingPage'
import './styles/theme.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 30_000 },
  },
})

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth()
  return isAuthenticated ? <>{children}</> : <Navigate to="/login" replace />
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <ProtectedRoute>
            <HomePage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/notebooks/:id"
        element={
          <ProtectedRoute>
            <NotebookPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/connectors"
        element={
          <ProtectedRoute>
            <ConnectorsPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/dashboards"
        element={
          <ProtectedRoute>
            <DashboardsPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/dashboards/:id"
        element={
          <ProtectedRoute>
            <DashboardEditorPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/dashboards/:id/view"
        element={
          <ProtectedRoute>
            <DashboardPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/audit"
        element={
          <ProtectedRoute>
            <AuditPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/members"
        element={
          <ProtectedRoute>
            <MembersPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/admin"
        element={
          <ProtectedRoute>
            <AdminPage />
          </ProtectedRoute>
        }
      />
      <Route path="/onboarding" element={<OrgOnboardingPage />} />
      <Route path="/public/dashboards/:token" element={<PublicDashboardPage />} />
      <Route path="/notebooks/:id/present" element={<PresentationPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

function AuthProvider({ children }: { children: React.ReactNode }) {
  const auth = useAuthProvider()
  return <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AuthProvider>
          <AppRoutes />
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
