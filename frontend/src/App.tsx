/**
 * App - Root application component with routing and authentication
 * 
 * Features:
 *   - Protected routes
 *   - Authentication state management
 *   - Theme provider
 *   - Global layout
 * 
 * Dependencies:
 *   - react-router-dom for routing
 *   - @mui/material for theming
 *   - ./services/auth for authentication
 *   - ./components/* for page components
 * 
 * Error Scenarios:
 *   - Authentication failures
 *   - Route access errors
 *   - Component loading errors
 *   - Theme initialization failures
 * 
 * Usage Example:
 * ```tsx
 * // In index.tsx
 * ReactDOM.render(
 *   <React.StrictMode>
 *     <App />
 *   </React.StrictMode>,
 *   document.getElementById('root')
 * );
 * ```
 * 
 * Performance Considerations:
 *   - Lazy loading of routes
 *   - Optimized authentication checks
 *   - Memoized theme provider
 * 
 * @returns {JSX.Element} Root application component
 */

import React, { useState, Suspense, lazy } from 'react';
import { BrowserRouter as Router, Routes, Route, Navigate, Outlet, useLocation } from 'react-router-dom';
import { ThemeProvider } from '@mui/material/styles';
import { CssBaseline, CircularProgress, Box } from '@mui/material';
import theme from './styles/theme';
import Layout from './components/Layout';
import PrivateRoute from './components/PrivateRoute';
import CertificateCheck from './components/CertificateCheck';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import { SnackbarProvider, useSnackbar } from 'notistack';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { getCookie } from './utils/cookies';

// Create a client instance
const queryClient = new QueryClient();

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
const ProfileSettingsPage = lazy(() => import('./pages/settings/ProfileSettings'));
const AgentDetailsPage = lazy(() => import('./pages/AgentDetails'));
const PotPage = lazy(() => import('./pages/Pot'));
const PotHashlistPage = lazy(() => import('./pages/PotHashlist'));
const PotClientPage = lazy(() => import('./pages/PotClient'));
const PotJobPage = lazy(() => import('./pages/PotJob'));
const AnalyticsPage = lazy(() => import('./pages/Analytics'));

// Lazy load pages - Clients page moved to regular auth section
const ClientsPage = lazy(() => import('./pages/AdminClients').then(module => ({ default: module.AdminClients })));

// Lazy load Admin Pages
const PresetJobListPage = lazy(() => import('./pages/admin/PresetJobList'));
const PresetJobFormPage = lazy(() => import('./pages/admin/PresetJobForm'));
const JobWorkflowListPage = lazy(() => import('./pages/admin/JobWorkflowList'));
const JobWorkflowFormPage = lazy(() => import('./pages/admin/JobWorkflowForm'));
const AdminAuthSettingsPage = lazy(() => import('./pages/admin/AuthSettings'));
const AdminSSOSettingsPage = lazy(() => import('./pages/admin/SSOSettings'));
const AdminUserListPage = lazy(() => import('./pages/admin/UserList'));
const AdminUserDetailPage = lazy(() => import('./pages/admin/UserDetail'));
const AdminSettingsIndexPage = lazy(() => import('./pages/AdminSettings').then(module => ({ default: module.AdminSettings })));
const AdminEmailSettingsIndexPage = lazy(() => import('./pages/AdminSettings/EmailSettings').then(module => ({ default: module.EmailSettings })));
const AdminEmailProviderConfigPage = lazy(() => import('./pages/AdminSettings/EmailSettings/ProviderConfig').then(module => ({ default: module.ProviderConfig })));
const AdminEmailTemplateEditorPage = lazy(() => import('./pages/AdminSettings/EmailSettings/TemplateEditor').then(module => ({ default: module.TemplateEditor })));

const App: React.FC = () => {
  const [certVerified, setCertVerified] = useState(() => {
    // Check if we have the ignore cookie
    const ignoreSSL = getCookie('ignoreSSL');
    if (ignoreSSL === 'true') {
      return true;
    }
    // Otherwise check if we've already verified the cert
    return localStorage.getItem('cert_valid') === 'true';
  });

  // Use snackbar for notifications
  const { enqueueSnackbar } = useSnackbar();

  const handleNotification = (message: string, variant: 'success' | 'error' | 'warning' | 'info') => {
    enqueueSnackbar(message, { variant });
  };

  if (!certVerified) {
    return (
      <ThemeProvider theme={theme}>
        <CssBaseline />
        <CertificateCheck onCertVerified={() => setCertVerified(true)} />
      </ThemeProvider>
    );
  }

  return (
    <AuthProvider>
        <QueryClientProvider client={queryClient}>
          <ThemeProvider theme={theme}>
            <CssBaseline />
            <SnackbarProvider maxSnack={3}>
              <Router>
                <Suspense fallback={
                  <Box display="flex" justifyContent="center" alignItems="center" minHeight="100vh">
                    <CircularProgress />
                  </Box>
                }>
                <Routes>
                  <Route path="/login" element={<LoginPage />} />

                  {/* Authenticated Routes */}
                  <Route element={<RequireAuth><Layout /></RequireAuth>}>
                    <Route path="/dashboard" element={<DashboardPage />} />
                    <Route path="/jobs" element={<JobsPage />} />
                    <Route path="/jobs/:id" element={<JobDetails />} />
                    <Route path="/agents" element={<AgentManagementPage />} />
                    <Route path="/agents/:id" element={<AgentDetailsPage />} />
                    <Route path="/hashlists" element={<HashlistsPage />} />
                    <Route path="/hashlists/:id" element={<HashlistDetailViewPage />} />
                    <Route path="/wordlists" element={<WordlistsManagementPage />} />
                    <Route path="/rules" element={<RulesManagementPage />} />
                    <Route path="/clients" element={<ClientsPage />} />
                    <Route path="/analytics" element={<AnalyticsPage />} />
                    <Route path="/pot" element={<PotPage />} />
                    <Route path="/pot/hashlist/:id" element={<PotHashlistPage />} />
                    <Route path="/pot/client/:id" element={<PotClientPage />} />
                    <Route path="/pot/job/:id" element={<PotJobPage />} />
                    <Route path="/about" element={<AboutPage />} />
                    <Route path="/settings/profile" element={<ProfileSettingsPage />} />

                    {/* Admin Section Routes */}
                    <Route path="/admin" element={<RequireAdmin><Outlet /></RequireAdmin>}>
                      <Route index element={<Navigate to="auth-settings" replace />} />
                      <Route path="preset-jobs" element={<PresetJobListPage />} />
                      <Route path="preset-jobs/new" element={<PresetJobFormPage />} />
                      <Route path="preset-jobs/:presetJobId/edit" element={<PresetJobFormPage />} />
                      <Route path="job-workflows" element={<JobWorkflowListPage />} />
                      <Route path="job-workflows/new" element={<JobWorkflowFormPage />} />
                      <Route path="job-workflows/:jobWorkflowId/edit" element={<JobWorkflowFormPage />} />
                      <Route path="auth-settings" element={<AdminAuthSettingsPage />} />
                      <Route path="sso-settings" element={<AdminSSOSettingsPage />} />
                      <Route path="users" element={<AdminUserListPage />} />
                      <Route path="users/:id" element={<AdminUserDetailPage />} />
                      <Route path="settings" element={<AdminSettingsIndexPage />} />
                      <Route path="settings/email" element={<AdminEmailSettingsIndexPage />} />
                <Route 
                        path="settings/email/provider" 
                        element={<AdminEmailProviderConfigPage onNotification={handleNotification} />} 
                      />
                <Route
                        path="settings/email/templates" 
                        element={<AdminEmailTemplateEditorPage onNotification={handleNotification} />} 
                      />
                    </Route>

                    {/* Catch-all for authenticated users */}
                    <Route path="*" element={<Navigate to="/dashboard" replace />} />
                  </Route>

                  {/* Redirect root based on auth */}
                  <Route path="/" element={<AuthRedirect />} />
                </Routes>
                </Suspense>
              </Router>
            </SnackbarProvider>
          </ThemeProvider>
        </QueryClientProvider>
    </AuthProvider>
  );
};

// Helper component to redirect from root based on authentication status
const AuthRedirect = () => {
  const { isAuth, isLoading } = useAuth();
  
  if (isLoading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight="100vh">
        <CircularProgress />
      </Box>
    );
  }
  
  return <Navigate to={isAuth ? "/dashboard" : "/login"} replace />;
};

// Define RequireAuth and RequireAdmin components
const RequireAuth: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuth, isLoading } = useAuth();
  const location = useLocation();

  if (isLoading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight="100vh">
        <CircularProgress />
      </Box>
    );
  }

  if (!isAuth) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }
  return <>{children}</>;
};

const RequireAdmin: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { userRole } = useAuth();
  if (userRole !== 'admin') {
    return <Navigate to="/dashboard" replace />;
  }
  return <>{children}</>;
};

export default App; 