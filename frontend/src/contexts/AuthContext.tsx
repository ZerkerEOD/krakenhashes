import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import { User } from '../types/auth';
import { getUserProfile } from '../services/user';
import { isAuthenticated, refreshToken } from '../services/auth';

interface AuthContextType {
  isAuth: boolean;
  setAuth: (isAuth: boolean) => void;
  user: User | null;
  setUser: (user: User | null) => void;
  userRole: string | null;
  setUserRole: (role: string | null) => void;
  checkAuthStatus: () => Promise<boolean>;
  isLoading: boolean;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [isAuth, setAuth] = useState(false);
  const [user, setUser] = useState<User | null>(null);
  const [userRole, setUserRole] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  const checkAuthStatus = useCallback(async (attemptRefresh = true): Promise<boolean> => {
    try {
      const authCheck = await isAuthenticated();
      setAuth(authCheck.authenticated);
      setUserRole(authCheck.role || null);
      
      if (authCheck.authenticated) {
        const profile = await getUserProfile();
        setUser(profile);
        return true;
      } else {
        // If not authenticated and refresh allowed, try to refresh token
        if (attemptRefresh) {
          try {
            console.debug('[Auth] Attempting token refresh...');
            await refreshToken();
            // Retry auth check without refresh to avoid infinite loop
            return await checkAuthStatus(false);
          } catch (refreshError) {
            console.debug('[Auth] Token refresh failed:', refreshError);
          }
        }
        
        setUser(null);
        setUserRole(null);
        return false;
      }
    } catch (error) {
      console.error('[Auth] Auth check failed:', error);
      setAuth(false);
      setUser(null);
      setUserRole(null);
      return false;
    }
  }, []);

  // Initial auth check
  useEffect(() => {
    let isMounted = true;
    
    const performInitialCheck = async () => {
      if (isMounted) {
        await checkAuthStatus();
        setIsLoading(false);
      }
    };
    
    performInitialCheck();
    
    return () => {
      isMounted = false;
    };
  }, []); // Empty dependency array for initial check only

  // Check auth on window focus
  useEffect(() => {
    const handleFocus = () => {
      checkAuthStatus();
    };

    window.addEventListener('focus', handleFocus);
    return () => window.removeEventListener('focus', handleFocus);
  }, []); // Remove checkAuthStatus dependency

  // Check auth on visibility change (tab switch)
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        checkAuthStatus();
      }
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange);
  }, []); // Remove checkAuthStatus dependency

  // Periodic auth check when authenticated
  useEffect(() => {
    if (!isAuth) return;

    const interval = setInterval(() => {
      checkAuthStatus();
    }, 5 * 60 * 1000); // Check every 5 minutes

    return () => clearInterval(interval);
  }, [isAuth]); // Remove checkAuthStatus dependency

  // Fallback token refresh for idle tabs
  // Note: The backend now handles primary session extension via throttled refresh on user activity.
  // This timer is a safety net for users who leave a tab open but idle - it ensures the session
  // stays alive even without direct API calls. The backend will only refresh if the token is
  // past its refresh threshold (1/3 of session time), so frequent calls here are efficient.
  useEffect(() => {
    if (!isAuth) return;

    const refreshInterval = setInterval(async () => {
      try {
        console.debug('[Auth] Performing fallback token refresh for idle tab...');
        await refreshToken(true); // Pass true to indicate automatic refresh (won't update last_activity)
        console.debug('[Auth] Fallback token refresh successful');
      } catch (error) {
        console.error('[Auth] Fallback token refresh failed:', error);
        // Force auth check which may trigger login redirect
        checkAuthStatus();
      }
    }, 15 * 60 * 1000); // Refresh every 15 minutes as safety net

    return () => clearInterval(refreshInterval);
  }, [isAuth]); // Remove checkAuthStatus dependency

  return (
    <AuthContext.Provider 
      value={{ 
        isAuth, 
        setAuth, 
        user, 
        setUser, 
        userRole, 
        setUserRole,
        checkAuthStatus,
        isLoading
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}; 