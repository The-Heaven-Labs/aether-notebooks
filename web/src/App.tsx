import { lazy, Suspense, useEffect } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useAuth, useAuthProvider, AuthContext } from './hooks/useAuth'
import { LoadingPage } from './components/LoadingPage'
import './styles/theme.css'

// Route-level code splitting: each page loads on first visit instead of
// shipping the entire app (including ECharts) in one 3MB bundle.
const LoginPage = lazy(() => import('./pages/LoginPage').then(m => ({ default: m.LoginPage })))
const OAuthConsentPage = lazy(() => import('./pages/OAuthConsentPage').then(m => ({ default: m.OAuthConsentPage })))
const HomePage = lazy(() => import('./pages/HomePage').then(m => ({ default: m.HomePage })))
const ChatPage = lazy(() => import('./pages/ChatPage').then(m => ({ default: m.ChatPage })))
const NotebookPage = lazy(() => import('./pages/NotebookPage').then(m => ({ default: m.NotebookPage })))
const ConnectorsPage = lazy(() => import('./pages/ConnectorsPage').then(m => ({ default: m.ConnectorsPage })))
const WarehouseSettingsPage = lazy(() => import('./pages/WarehouseSettingsPage').then(m => ({ default: m.WarehouseSettingsPage })))
const DashboardsPage = lazy(() => import('./pages/DashboardsPage').then(m => ({ default: m.DashboardsPage })))
const DashboardEditorPage = lazy(() => import('./pages/DashboardEditorPage').then(m => ({ default: m.DashboardEditorPage })))
const DashboardPage = lazy(() => import('./pages/DashboardPage').then(m => ({ default: m.DashboardPage })))
const AuditPage = lazy(() => import('./pages/AuditPage').then(m => ({ default: m.AuditPage })))
const MembersPage = lazy(() => import('./pages/MembersPage').then(m => ({ default: m.MembersPage })))
const AdminPage = lazy(() => import('./pages/AdminPage').then(m => ({ default: m.AdminPage })))
const PublicDashboardPage = lazy(() => import('./pages/PublicDashboardPage').then(m => ({ default: m.PublicDashboardPage })))
const PublicNotebookPage = lazy(() => import('./pages/PublicNotebookPage').then(m => ({ default: m.PublicNotebookPage })))
const EmbedPage = lazy(() => import('./pages/EmbedPage').then(m => ({ default: m.EmbedPage })))
const PresentationPage = lazy(() => import('./pages/PresentationPage').then(m => ({ default: m.PresentationPage })))
const OrgOnboardingPage = lazy(() => import('./pages/OrgOnboardingPage').then(m => ({ default: m.OrgOnboardingPage })))
const ProfilePage = lazy(() => import('./pages/ProfilePage').then(m => ({ default: m.ProfilePage })))
const GroupsPage = lazy(() => import('./pages/GroupsPage').then(m => ({ default: m.GroupsPage })))
const OrgSettingsPage = lazy(() => import('./pages/OrgSettingsPage').then(m => ({ default: m.OrgSettingsPage })))
const AgentsPage = lazy(() => import('./pages/AgentsPage').then(m => ({ default: m.AgentsPage })))
const StatsPage = lazy(() => import('./pages/StatsPage').then(m => ({ default: m.StatsPage })))
const AboutPage = lazy(() => import('./pages/AboutPage').then(m => ({ default: m.AboutPage })))
const ModelsPage = lazy(() => import('./pages/ModelsPage').then(m => ({ default: m.ModelsPage })))
const SkillsPage = lazy(() => import('./pages/SkillsPage').then(m => ({ default: m.SkillsPage })))
const ToolsPage = lazy(() => import('./pages/ToolsPage').then(m => ({ default: m.ToolsPage })))
const TrashPage = lazy(() => import('./pages/TrashPage').then(m => ({ default: m.TrashPage })))
const MCPPage = lazy(() => import('./pages/MCPPage').then(m => ({ default: m.MCPPage })))
const JoinPage = lazy(() => import('./pages/JoinPage').then(m => ({ default: m.JoinPage })))

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 30_000 },
  },
})

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth()
  const location = useLocation()
  if (!isAuthenticated) {
    sessionStorage.setItem('aether_redirect_after_login', location.pathname + location.search)
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

function AppRoutes() {
  return (
    <Suspense fallback={<LoadingPage />}>
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/oauth/authorize" element={<OAuthConsentPage />} />
      <Route path="/join" element={<JoinPage />} />
      <Route path="/" element={<ProtectedRoute><HomePage /></ProtectedRoute>} />
      <Route
        path="/notebooks/:id"
        element={
          <ProtectedRoute>
            <NotebookPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/chats/:id"
        element={
          <ProtectedRoute>
            <ChatPage />
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
        path="/warehouses"
        element={
          <ProtectedRoute>
            <WarehouseSettingsPage />
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
      <Route path="/profile" element={<ProtectedRoute><ProfilePage /></ProtectedRoute>} />
      <Route path="/groups" element={<ProtectedRoute><GroupsPage /></ProtectedRoute>} />
      <Route path="/settings" element={<ProtectedRoute><OrgSettingsPage /></ProtectedRoute>} />
      <Route path="/agents" element={<ProtectedRoute><AgentsPage /></ProtectedRoute>} />
      <Route path="/agents/stats" element={<ProtectedRoute><StatsPage /></ProtectedRoute>} />
      <Route path="/about" element={<ProtectedRoute><AboutPage /></ProtectedRoute>} />
      <Route path="/models" element={<ProtectedRoute><ModelsPage /></ProtectedRoute>} />
      <Route path="/skills" element={<ProtectedRoute><SkillsPage /></ProtectedRoute>} />
      <Route path="/tools" element={<ProtectedRoute><ToolsPage /></ProtectedRoute>} />
      <Route path="/trash" element={<ProtectedRoute><TrashPage /></ProtectedRoute>} />
      <Route path="/mcps" element={<ProtectedRoute><MCPPage /></ProtectedRoute>} />
      <Route path="/onboarding" element={<OrgOnboardingPage />} />
      <Route path="/public/dashboards/:token" element={<PublicDashboardPage />} />
      <Route path="/public/:token" element={<PublicNotebookPage />} />
      <Route path="/embed/:token/:cellId" element={<EmbedPage />} />
      <Route path="/notebooks/:id/present" element={<PresentationPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
    </Suspense>
  )
}

function AuthProvider({ children }: { children: React.ReactNode }) {
  const auth = useAuthProvider()
  return <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>
}

export default function App() {
  useEffect(() => {
    const style = document.createElement('style')
    style.textContent = 'div[style*="height: 8px"][style*="border-top: 1px"] { display: none !important }'
    document.head.appendChild(style)
  }, [])

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
