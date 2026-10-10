/**
 * Layout - application shell: app bar, collapsible grouped sidebar, content
 * outlet and footer. The sidebar's items come from `navigation/navConfig`;
 * the drawer state and group expansion persist in localStorage.
 */
import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate, useLocation, Outlet } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  AppBar,
  Box,
  Divider,
  Drawer,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Toolbar,
  Tooltip,
  Typography,
  Theme,
} from '@mui/material';
import MenuIcon from '@mui/icons-material/Menu';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import LogoutIcon from '@mui/icons-material/Logout';
import InfoIcon from '@mui/icons-material/Info';
import DownloadIcon from '@mui/icons-material/Download';
import { logout } from '../services/auth';
import { useAuth } from '../contexts/AuthContext';
import { useBranding, fallbackToStockLogo } from '../contexts/BrandingContext';
import UserMenu from './common/UserMenu';
import Footer from './Footer';
import { NotificationBell } from './Notifications';
import { TeamFilter } from './common/TeamFilter';
import NavList from './navigation/NavList';
import ErrorBoundary from './ui/ErrorBoundary';
import { ROUTES } from '../constants/routes';

const DRAWER_WIDTH = 240;
const STORAGE_KEY_OPEN = 'kh.nav.open';

const readOpen = (): boolean => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY_OPEN);
    return raw === null ? true : raw === 'true';
  } catch {
    return true;
  }
};

const Layout: React.FC = () => {
  const [open, setOpen] = useState<boolean>(readOpen);
  const navigate = useNavigate();
  const location = useLocation();
  const { setAuth, setUser, setUserRole } = useAuth();
  const { t } = useTranslation('navigation');
  const { t: tCommon } = useTranslation('common');
  const { branding } = useBranding();

  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY_OPEN, String(open));
    } catch {
      /* private mode */
    }
  }, [open]);

  const handleLogout = useCallback(async (): Promise<void> => {
    try {
      await logout();
      setAuth(false);
      setUser(null);
      setUserRole(null);
      navigate('/login', { replace: true });
    } catch (error) {
      console.error('Logout failed:', error);
    }
  }, [navigate, setAuth, setUser, setUserRole]);

  const railItem = (key: string, label: string, icon: React.ReactNode, onClick: () => void, selected = false) => {
    const button = (
      <ListItemButton
        key={key}
        onClick={onClick}
        selected={selected}
        sx={{ minHeight: 44, justifyContent: open ? 'initial' : 'center', px: 2.5 }}
      >
        <ListItemIcon sx={{ minWidth: 0, mr: open ? 2.5 : 'auto', justifyContent: 'center' }}>{icon}</ListItemIcon>
        <ListItemText primary={label} sx={{ opacity: open ? 1 : 0, whiteSpace: 'nowrap' }} />
      </ListItemButton>
    );
    return open ? button : (
      <Tooltip key={key} title={label} placement="right">
        {button}
      </Tooltip>
    );
  };

  const width = (theme: Theme) => (open ? DRAWER_WIDTH : parseInt(theme.spacing(7), 10));

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh' }}>
      <AppBar position="fixed" sx={{ zIndex: (theme: Theme) => theme.zIndex.drawer + 1, width: '100%' }}>
        <Toolbar>
          <IconButton
            color="inherit"
            aria-label={t('aria.toggleDrawer') as string}
            onClick={() => setOpen((o) => !o)}
            edge="start"
            sx={{ mr: 2 }}
          >
            {open ? <ChevronLeftIcon /> : <MenuIcon />}
          </IconButton>
          <Box sx={{ display: 'flex', alignItems: 'center', flexGrow: 1, minWidth: 0 }}>
            <img
              src={branding.logo_url ?? '/logo.png'}
              alt={
                branding.branded
                  ? (tCommon('layout.logoAltBranded', { appName: branding.app_name }) as string)
                  : (tCommon('layout.logoAlt') as string)
              }
              style={{ height: 32, maxWidth: 160, objectFit: 'contain', marginRight: 12 }}
              onError={fallbackToStockLogo}
            />
            <Box sx={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
              <Typography variant="h6" noWrap component="div" sx={{ lineHeight: 1.2 }}>
                {branding.branded ? branding.app_name : (t('appName') as string)}
              </Typography>
              {branding.branded && (
                <Typography variant="caption" noWrap component="div" sx={{ lineHeight: 1, opacity: 0.75 }}>
                  {branding.powered_by}
                </Typography>
              )}
            </Box>
          </Box>
          <TeamFilter />
          <Tooltip title={t('aria.downloadCaCert') as string}>
            <IconButton
              color="inherit"
              aria-label={t('aria.downloadCaCert') as string}
              onClick={() => window.open(`http://${window.location.hostname}:1337/ca.crt`, '_blank')}
              sx={{ ml: 1 }}
            >
              <DownloadIcon />
            </IconButton>
          </Tooltip>
          <Box sx={{ ml: 1 }}>
            <NotificationBell />
          </Box>
          <UserMenu />
        </Toolbar>
      </AppBar>

      <Drawer
        variant="permanent"
        open={open}
        sx={{
          width,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width,
            overflowX: 'hidden',
            borderRight: (theme: Theme) => `1px solid ${theme.palette.divider}`,
            transition: (theme: Theme) =>
              theme.transitions.create('width', {
                easing: theme.transitions.easing.sharp,
                duration: theme.transitions.duration.enteringScreen,
              }),
            position: 'fixed',
            height: '100%',
            display: 'flex',
            flexDirection: 'column',
          },
        }}
      >
        <Toolbar />
        <Box sx={{ flexGrow: 1, overflowY: 'auto', overflowX: 'hidden', pb: 1 }}>
          <NavList open={open} />
        </Box>
        <Divider />
        <List disablePadding>
          {railItem('about', t('menu.about') as string, <InfoIcon />, () => navigate(ROUTES.about), location.pathname === ROUTES.about)}
          {railItem('logout', t('menu.logout') as string, <LogoutIcon />, handleLogout)}
        </List>
      </Drawer>

      <Box
        component="main"
        sx={{
          flexGrow: 1,
          minWidth: 0,
          p: 3,
          pb: 8,
          // No left margin: the permanent drawer already takes its width in the flex row.
        }}
      >
        <Toolbar />
        <ErrorBoundary>
          <Outlet />
        </ErrorBoundary>
      </Box>
      <Footer drawerOpen={open} />
    </Box>
  );
};

export default Layout;
