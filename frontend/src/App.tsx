/**
 * App - root component: provider stack and the route table.
 *
 * Routing uses a data router (`createBrowserRouter`) so pages can use
 * `useBlocker` for unsaved-changes guards. Every page is lazy and wrapped in a
 * `RouteBoundary` (page-shaped skeleton while loading, error state with retry
 * if it throws, fade on entry).
 */
import React, { lazy } from 'react';
import {
  createBrowserRouter,
  createRoutesFromElements,
  Navigate,
  Outlet,
  Route,
  RouterProvider,
  useLocation,
} from 'react-router-dom';
import { QueryClientProvider } from '@tanstack/react-query';
import Layout from './components/Layout';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import { TeamFilterProvider } from './contexts/TeamFilterContext';
import { NotificationProvider } from './contexts/NotificationContext';
import { DeletionProgressProvider } from './contexts/DeletionProgressContext';
import { PollingProvider } from './contexts/PollingContext';
import { queryClient } from './services/queryClient';
import { ToastProvider } from './components/ui/toast';
import { ConfirmProvider } from './components/ui/ConfirmProvider';
import RouteBoundary from './components/ui/RouteBoundary';
import PageSkeleton, { PageSkeletonVariant } from './components/ui/PageSkeleton';
import ErrorBoundary from './components/ui/ErrorBoundary';

// Lazy load pages
const LoginPage = lazy(() => import('./pages/Login'));
const DashboardPage = lazy(() => import('./pages/Dashboard'));
const JobsPage = lazy(() => import('./pages/Jobs'));
const JobDetails = lazy(() => import('./pages/Jobs/JobDetails'));
const AgentManagementPage = lazy(() => import('./pages/AgentManagement'));
const WordlistsManagementPage = lazy(() => import('./pages/WordlistsManagement'));
const RulesManagementPage = lazy(() => import('./pages/RulesManagement'));
const HashlistsPage = lazy(() => import('./pages/Hashlists'));
const HashlistDetailViewPage = lazy(() => import('./components/hashlist/HashlistDetailView'));
const AboutPage = lazy(() => import('./pages/About'));
const UserSettingsPage = lazy(() => import('./pages/settings/UserSettings'));
const AgentDetailsPage = lazy(() => import('./pages/AgentDetails'));
const PotPage = lazy(() => import('./pages/Pot'));
const PotHashlistPage = lazy(() => import('./pages/PotHashlist'));
const PotClientPage = lazy(() => import('./pages/PotClient'));
const PotJobPage = lazy(() => import('./pages/PotJob'));
const AnalyticsPage = lazy(() => import('./pages/Analytics'));
const NotificationCenterPage = lazy(() => import('./pages/Notifications/NotificationCenter'));
const ClientsPage = lazy(() => import('./pages/AdminClients').then((m) => ({ default: m.AdminClients })));
const TeamListPage = lazy(() => import('./pages/teams/TeamList'));
const ClientDetailPage = lazy(() => import('./pages/clients/ClientDetail'));
const TeamDetailPage = lazy(() => import('./pages/teams/TeamDetail'));

// Admin pages
const PresetJobListPage = lazy(() => import('./pages/admin/PresetJobList'));
const PresetJobFormPage = lazy(() => import('./pages/admin/PresetJobForm'));
const JobWorkflowListPage = lazy(() => import('./pages/admin/JobWorkflowList'));
const JobWorkflowFormPage = lazy(() => import('./pages/admin/JobWorkflowForm'));
const AdminUserListPage = lazy(() => import('./pages/admin/UserList'));
const AdminUserDetailPage = lazy(() => import('./pages/admin/UserDetail'));
const AdminSettingsIndexPage = lazy(() => import('./pages/AdminSettings').then((m) => ({ default: m.AdminSettings })));
const CustomCharsetListPage = lazy(() => import('./pages/admin/CustomCharsetList'));
const DiagnosticsPage = lazy(() => import('./pages/admin/Diagnostics'));
const AdminAuditLogPage = lazy(() => import('./pages/AdminAuditLog'));
const JobAnalyticsPage = lazy(() => import('./pages/admin/JobAnalytics'));
const CloudFleetPage = lazy(() => import('./pages/admin/CloudFleet'));
const BinariesPage = lazy(() => import('./pages/admin/Binaries'));
const HashTypesPage = lazy(() => import('./pages/admin/HashTypes'));
const VouchersPage = lazy(() => import('./pages/admin/Vouchers'));

/** Wrap a lazy page in its route boundary with the matching skeleton shape. */
const page = (Component: React.LazyExoticComponent<React.ComponentType<any>>, skeleton: PageSkeletonVariant = 'list') => (
  <RouteBoundary skeleton={skeleton}>
    <Component />
  </RouteBoundary>
);

// Helper component to redirect from root based on authentication status
const AuthRedirect: React.FC = () => {
  const { isAuth, isLoading } = useAuth();
  if (isLoading) return <PageSkeleton variant="dashboard" />;
  return <Navigate to={isAuth ? '/dashboard' : '/login'} replace />;
};

const RequireAuth: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuth, isLoading } = useAuth();
  const location = useLocation();
  if (isLoading) return <PageSkeleton variant="dashboard" />;
  if (!isAuth) return <Navigate to="/login" state={{ from: location }} replace />;
  return <>{children}</>;
};

const RequireAdmin: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { userRole } = useAuth();
  if (userRole !== 'admin') return <Navigate to="/dashboard" replace />;
  return <>{children}</>;
};

/**
 * Root route element: the provider stack that needs router context sits here,
 * everything else is mounted once around the RouterProvider.
 */
const RootShell: React.FC = () => (
  <ErrorBoundary>
    <Outlet />
  </ErrorBoundary>
);

const router = createBrowserRouter(
  createRoutesFromElements(
    <Route element={<RootShell />}>
      <Route path="/login" element={page(LoginPage, 'form')} />

      {/* Authenticated routes */}
      <Route element={<RequireAuth><Layout /></RequireAuth>}>
        <Route path="/dashboard" element={page(DashboardPage, 'dashboard')} />
        <Route path="/jobs" element={page(JobsPage)} />
        <Route path="/jobs/:id" element={page(JobDetails, 'detail')} />
        <Route path="/agents" element={page(AgentManagementPage)} />
        <Route path="/agents/:id" element={page(AgentDetailsPage, 'detail')} />
        <Route path="/hashlists" element={page(HashlistsPage)} />
        <Route path="/hashlists/:id" element={page(HashlistDetailViewPage, 'detail')} />
        <Route path="/wordlists" element={page(WordlistsManagementPage)} />
        <Route path="/rules" element={page(RulesManagementPage)} />
        <Route path="/clients" element={page(ClientsPage)} />
        <Route path="/clients/:id" element={page(ClientDetailPage, 'detail')} />
        <Route path="/analytics" element={page(AnalyticsPage, 'detail')} />
        <Route path="/pot" element={page(PotPage)} />
        <Route path="/pot/hashlist/:id" element={page(PotHashlistPage)} />
        <Route path="/pot/client/:id" element={page(PotClientPage)} />
        <Route path="/pot/job/:id" element={page(PotJobPage)} />
        <Route path="/teams" element={page(TeamListPage)} />
        <Route path="/teams/:teamId" element={page(TeamDetailPage, 'detail')} />
        <Route path="/about" element={page(AboutPage, 'form')} />
        <Route path="/settings/*" element={page(UserSettingsPage, 'settings')} />
        <Route path="/notifications" element={page(NotificationCenterPage)} />

        {/* Admin section */}
        <Route path="/admin" element={<RequireAdmin><Outlet /></RequireAdmin>}>
          <Route index element={<Navigate to="settings" replace />} />
          <Route path="custom-charsets" element={page(CustomCharsetListPage)} />
          <Route path="preset-jobs" element={page(PresetJobListPage)} />
          <Route path="preset-jobs/new" element={page(PresetJobFormPage, 'form')} />
          <Route path="preset-jobs/:presetJobId/edit" element={page(PresetJobFormPage, 'form')} />
          <Route path="job-workflows" element={page(JobWorkflowListPage)} />
          <Route path="job-workflows/new" element={page(JobWorkflowFormPage, 'form')} />
          <Route path="job-workflows/:jobWorkflowId/edit" element={page(JobWorkflowFormPage, 'form')} />
          <Route path="users" element={page(AdminUserListPage)} />
          <Route path="users/:id" element={page(AdminUserDetailPage, 'detail')} />
          <Route path="settings/*" element={page(AdminSettingsIndexPage, 'settings')} />
          {/* Legacy standalone pages now live inside Admin Settings */}
          <Route path="auth-settings" element={<Navigate to="/admin/settings" replace />} />
          <Route path="sso-settings" element={<Navigate to="/admin/settings" replace />} />
          <Route path="diagnostics" element={page(DiagnosticsPage, 'detail')} />
          <Route path="audit-log" element={page(AdminAuditLogPage)} />
          <Route path="job-analytics" element={page(JobAnalyticsPage, 'dashboard')} />
          <Route path="cloud/fleet" element={page(CloudFleetPage)} />
          <Route path="binaries" element={page(BinariesPage)} />
          <Route path="hash-types" element={page(HashTypesPage)} />
          <Route path="vouchers" element={page(VouchersPage)} />
        </Route>

        {/* Catch-all for authenticated users */}
        <Route path="*" element={<Navigate to="/dashboard" replace />} />
      </Route>

      {/* Redirect root based on auth */}
      <Route path="/" element={<AuthRedirect />} />
    </Route>
  )
);

const App: React.FC = () => (
  <AuthProvider>
    <TeamFilterProvider>
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <ConfirmProvider>
            <NotificationProvider>
              <DeletionProgressProvider>
                <PollingProvider>
                  <RouterProvider router={router} fallbackElement={<PageSkeleton variant="dashboard" />} />
                </PollingProvider>
              </DeletionProgressProvider>
            </NotificationProvider>
          </ConfirmProvider>
        </ToastProvider>
      </QueryClientProvider>
    </TeamFilterProvider>
  </AuthProvider>
);

export default App;
