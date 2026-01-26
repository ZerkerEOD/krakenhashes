/**
 * Login - Authentication component for KrakenHashes frontend
 * 
 * Features:
 *   - User authentication
 *   - Password strength validation
 *   - Remember me functionality
 *   - Rate limiting protection
 * 
 * Dependencies:
 *   - react-router-dom for navigation
 *   - @mui/material for UI components
 *   - ../services/auth for authentication
 *   - ../types/auth for type definitions
 * 
 * Browser Support:
 *   - Chrome/Chromium based (Chrome, Edge, Brave)
 *   - Firefox
 *   - Mobile responsive design
 * 
 * Error Scenarios:
 *   - Invalid credentials
 *   - Network failures
 *   - Rate limit exceeded
 *   - Password policy violations
 * 
 * TODOs:
 *   - Implement forgot password functionality (requires email service)
 *   - Add 2FA support
 *   - Implement CAPTCHA for failed login attempts
 * 
 * @param {LoginProps} props - Component properties
 * @returns {JSX.Element} Login form component
 */

import React, { useState, useCallback, useRef, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import {
  Box,
  Button,
  TextField,
  Typography,
  Container,
  FormControlLabel,
  Checkbox,
  CircularProgress,
  Divider,
  Alert,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions
} from '@mui/material';
import AccountTreeIcon from '@mui/icons-material/AccountTree';
import SecurityIcon from '@mui/icons-material/Security';
import VpnKeyIcon from '@mui/icons-material/VpnKey';
import { login } from '../services/auth';
import { useAuth } from '../contexts/AuthContext';
import { LoginCredentials } from '../types/auth';
import { SSOProviderDisplay, EnabledProvidersResponse, SSOLoginResponse } from '../types/sso';
import { getEnabledProviders, ldapLogin, startSAMLFlow, startOAuthFlow } from '../services/sso';
import MFAVerification from '../components/auth/MFAVerification';

// Rate limiting configuration
const RATE_LIMIT = {
  maxRequests: 10,
  timeWindow: 1000, // 1 second
};

const Login: React.FC = () => {
  const { setAuth, setUserRole, checkAuthStatus } = useAuth();
  const [credentials, setCredentials] = useState<LoginCredentials>({
    username: '',
    password: ''
  });
  const [error, setError] = useState<string>('');
  const [rememberMe, setRememberMe] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(false);
  const [mfaRequired, setMfaRequired] = useState<boolean>(false);
  const [mfaSession, setMfaSession] = useState<{
    sessionToken: string;
    mfaType: string[];
    preferredMethod: string;
    expiresAt?: string;
  } | null>(null);
  const requestCount = useRef<number>(0);
  const lastRequestTime = useRef<number>(Date.now());
  const navigate = useNavigate();
  const location = useLocation();

  // SSO state
  const [ssoProviders, setSsoProviders] = useState<SSOProviderDisplay[]>([]);
  const [localAuthEnabled, setLocalAuthEnabled] = useState<boolean>(true);
  const [ssoLoading, setSsoLoading] = useState<boolean>(true);
  const [ldapDialogOpen, setLdapDialogOpen] = useState<boolean>(false);
  const [selectedLdapProvider, setSelectedLdapProvider] = useState<SSOProviderDisplay | null>(null);
  const [ldapCredentials, setLdapCredentials] = useState<{ username: string; password: string }>({
    username: '',
    password: ''
  });
  const [ldapLoading, setLdapLoading] = useState<boolean>(false);

  // Fetch SSO providers on mount
  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await getEnabledProviders();
        setSsoProviders(response.providers || []);
        setLocalAuthEnabled(response.local_auth_enabled);
      } catch (err) {
        console.error('Failed to fetch SSO providers:', err);
        // Default to showing local auth if fetch fails
        setLocalAuthEnabled(true);
      } finally {
        setSsoLoading(false);
      }
    };
    fetchProviders();
  }, []);

  // Check for SSO callback errors in URL
  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const ssoError = params.get('sso_error');
    if (ssoError) {
      // Translate error codes to user-friendly messages
      let errorMessage: string;
      switch (ssoError) {
        case 'pending_approval':
          errorMessage = 'Your account has been created but requires administrator approval. Please contact your administrator.';
          break;
        case 'account_disabled':
          errorMessage = 'Your account has been disabled. Please contact your administrator.';
          break;
        default:
          errorMessage = decodeURIComponent(ssoError);
      }
      setError(errorMessage);
      // Clear the error from URL
      navigate('/login', { replace: true });
    }
  }, [location, navigate]);

  /**
   * Handles rate limiting for login attempts
   * 
   * @returns {boolean} Whether request should be allowed
   * @throws {Error} When rate limit is exceeded
   */
  const checkRateLimit = useCallback((): boolean => {
    const now = Date.now();
    if (now - lastRequestTime.current > RATE_LIMIT.timeWindow) {
      requestCount.current = 0;
      lastRequestTime.current = now;
    }
    
    if (requestCount.current >= RATE_LIMIT.maxRequests) {
      throw new Error('Too many login attempts. Please try again later.');
    }
    
    requestCount.current++;
    return true;
  }, []);

  /**
   * Handles form submission and authentication
   * 
   * @param {React.FormEvent} e - Form event
   * @returns {Promise<void>}
   */
  const handleSubmit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      checkRateLimit();

      const response = await login(credentials.username, credentials.password);
      
      // Check if MFA is required
      if (response.mfa_required) {
        // Verify required MFA fields are present
        if (!response.session_token || !response.mfa_type || !response.preferred_method) {
          throw new Error('Invalid MFA response from server');
        }
        
        setMfaRequired(true);
        setMfaSession({
          sessionToken: response.session_token,
          mfaType: response.mfa_type,
          preferredMethod: response.preferred_method,
          expiresAt: response.expires_at
        });
      } else if (response.token) {
        handleLoginSuccess(response.token);
      } else {
        setError(response.message || 'Login failed');
      }
    } catch (error) {
      setError(error instanceof Error ? error.message : 'An error occurred');
    } finally {
      setLoading(false);
    }
  };

  const handleMFASuccess = (token: string) => {
    handleLoginSuccess(token);
  };

  const handleLoginSuccess = (token: string) => {
    if (rememberMe) {
      localStorage.setItem('rememberMe', 'true');
    }
    setAuth(true);
    checkAuthStatus(); // This will fetch the user profile and set the role
    navigate('/dashboard', { replace: true });
  };

  const handleMFAError = (error: string) => {
    setError(error);
  };

  // SSO Login Handlers
  const handleSSOLogin = (provider: SSOProviderDisplay) => {
    setError('');
    if (provider.provider_type === 'ldap') {
      setSelectedLdapProvider(provider);
      setLdapCredentials({ username: '', password: '' });
      setLdapDialogOpen(true);
    } else if (provider.provider_type === 'saml') {
      startSAMLFlow(provider.id);
    } else {
      startOAuthFlow(provider.id);
    }
  };

  const handleLdapSubmit = async () => {
    if (!selectedLdapProvider) return;

    setLdapLoading(true);
    setError('');
    try {
      const response = await ldapLogin(selectedLdapProvider.id, ldapCredentials);
      if (response.mfa_required && response.session_token) {
        setLdapDialogOpen(false);
        setMfaRequired(true);
        setMfaSession({
          sessionToken: response.session_token,
          mfaType: response.mfa_type || ['email'],
          preferredMethod: response.preferred_method || 'email',
          expiresAt: response.expires_at
        });
      } else if (response.success && response.token) {
        setLdapDialogOpen(false);
        handleLoginSuccess(response.token);
      } else if (response.pending_approval) {
        setLdapDialogOpen(false);
        setError('Account created but pending admin approval. Please contact your administrator.');
      } else {
        setError(response.message || 'LDAP authentication failed');
      }
    } catch (err: any) {
      setError(err.message || 'LDAP authentication failed');
    } finally {
      setLdapLoading(false);
    }
  };

  const getProviderIcon = (type: string) => {
    switch (type) {
      case 'ldap':
        return <AccountTreeIcon />;
      case 'saml':
        return <SecurityIcon />;
      case 'oidc':
      case 'oauth2':
        return <VpnKeyIcon />;
      default:
        return <VpnKeyIcon />;
    }
  };

  const getProviderLabel = (provider: SSOProviderDisplay): string => {
    return `Sign in with ${provider.name}`;
  };

  if (mfaRequired && mfaSession) {
    return (
      <Container component="main" maxWidth="xs">
        <Box
          sx={{
            marginTop: 8,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
          }}
        >
          <MFAVerification
            sessionToken={mfaSession.sessionToken}
            mfaType={mfaSession.mfaType}
            preferredMethod={mfaSession.preferredMethod}
            onSuccess={handleMFASuccess}
            onError={handleMFAError}
            expiresAt={mfaSession.expiresAt}
          />
        </Box>
      </Container>
    );
  }

  return (
    <Container component="main" maxWidth="xs">
      <Box
        sx={{
          marginTop: 8,
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
        }}
      >
        <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', mb: 3 }}>
          <img 
            src="/logo.png" 
            alt="KrakenHashes Logo" 
            style={{ height: 80, marginBottom: 16 }}
          />
          <Typography component="h1" variant="h5">
            Log in to KrakenHashes
          </Typography>
        </Box>
        {/* Error Display */}
        {error && (
          <Alert severity="error" sx={{ mt: 2, width: '100%' }}>
            {error}
          </Alert>
        )}

        {/* Local Auth Form - only show if enabled */}
        {localAuthEnabled && (
          <Box component="form" onSubmit={handleSubmit} noValidate sx={{ mt: 1, width: '100%' }}>
          <TextField
            margin="normal"
            required
            fullWidth
            id="username"
            label="Username"
            name="username"
            autoComplete="username"
            autoFocus
            value={credentials.username}
            onChange={(e) => setCredentials((prev) => ({
              ...prev,
              username: e.target.value
            }))}
            disabled={loading}
          />
          <TextField
            margin="normal"
            required
            fullWidth
            name="password"
            label="Password"
            type="password"
            id="password"
            autoComplete="current-password"
            value={credentials.password}
            onChange={(e) => {
              setCredentials((prev) => ({
                ...prev,
                password: e.target.value
              }));
            }}
            disabled={loading}
          />
          <FormControlLabel
            control={
              <Checkbox
                value="remember"
                color="primary"
                checked={rememberMe}
                onChange={(e) => setRememberMe(e.target.checked)}
                disabled={loading}
              />
            }
            label="Remember me"
          />
            <Button
              type="submit"
              fullWidth
              variant="contained"
              sx={{ mt: 3, mb: 2 }}
              disabled={loading || !credentials.username || !credentials.password}
            >
              {loading ? <CircularProgress size={24} /> : 'Log In'}
            </Button>
          </Box>
        )}

        {/* SSO Providers */}
        {!ssoLoading && ssoProviders.length > 0 && (
          <Box sx={{ mt: 2, width: '100%' }}>
            {localAuthEnabled && (
              <Divider sx={{ my: 2 }}>
                <Typography variant="body2" color="text.secondary">
                  or continue with
                </Typography>
              </Divider>
            )}
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
              {ssoProviders.map((provider) => (
                <Button
                  key={provider.id}
                  fullWidth
                  variant="outlined"
                  startIcon={getProviderIcon(provider.provider_type)}
                  onClick={() => handleSSOLogin(provider)}
                  disabled={loading}
                >
                  {getProviderLabel(provider)}
                </Button>
              ))}
            </Box>
          </Box>
        )}

        {/* Show message if no auth methods available */}
        {!ssoLoading && !localAuthEnabled && ssoProviders.length === 0 && (
          <Alert severity="warning" sx={{ mt: 2 }}>
            No authentication methods are currently available. Please contact your administrator.
          </Alert>
        )}
      </Box>

      {/* LDAP Login Dialog */}
      <Dialog open={ldapDialogOpen} onClose={() => setLdapDialogOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>
          Sign in with {selectedLdapProvider?.name}
        </DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="Username"
            fullWidth
            variant="outlined"
            value={ldapCredentials.username}
            onChange={(e) => setLdapCredentials({ ...ldapCredentials, username: e.target.value })}
            disabled={ldapLoading}
          />
          <TextField
            margin="dense"
            label="Password"
            type="password"
            fullWidth
            variant="outlined"
            value={ldapCredentials.password}
            onChange={(e) => setLdapCredentials({ ...ldapCredentials, password: e.target.value })}
            disabled={ldapLoading}
            onKeyPress={(e) => {
              if (e.key === 'Enter' && ldapCredentials.username && ldapCredentials.password) {
                handleLdapSubmit();
              }
            }}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setLdapDialogOpen(false)} disabled={ldapLoading}>
            Cancel
          </Button>
          <Button
            onClick={handleLdapSubmit}
            variant="contained"
            disabled={ldapLoading || !ldapCredentials.username || !ldapCredentials.password}
          >
            {ldapLoading ? <CircularProgress size={24} /> : 'Sign In'}
          </Button>
        </DialogActions>
      </Dialog>
    </Container>
  );
};

export default Login; 