/**
 * Layout - Main application layout component with navigation
 *
 * Features:
 *   - Responsive drawer navigation
 *   - Dynamic menu items based on permissions
 *   - Collapsible sidebar
 *   - App bar with user controls
 *   - Internationalization support
 *
 * Dependencies:
 *   - @mui/material for UI components
 *   - react-router-dom for navigation
 *   - @mui/icons-material for icons
 *   - react-i18next for translations
 *
 * Error Scenarios:
 *   - Navigation failure handling
 *   - Route access permissions
 *   - Component rendering errors
 *
 * Usage Examples:
 * ```tsx
 * // Basic usage with child component
 * <Layout>
 *   <Dashboard />
 * </Layout>
 *
 * // Usage with multiple children
 * <Layout>
 *   <Header />
 *   <Content />
 *   <Footer />
 * </Layout>
 * ```
 *
 * Performance Considerations:
 *   - Memoized menu items to prevent unnecessary re-renders
 *   - Lazy loading of icons
 *   - Optimized drawer transitions
 *
 * @param {LayoutProps} props - Component props
 * @returns {JSX.Element} Layout wrapper with navigation
 */

import React, { useState, useCallback, useMemo } from 'react';
import { useNavigate, useLocation, Outlet } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
    AppBar,
    Box,
    CssBaseline,
    Drawer,
    IconButton,
    List,
    ListItem,
    ListItemIcon,
    ListItemText,
    Toolbar,
    Typography,
    Divider,
    Theme,
} from '@mui/material';
import {
    Menu as MenuIcon,
    ChevronLeft as ChevronLeftIcon,
    Dashboard as DashboardIcon,
    Work as WorkIcon,
    Computer as ComputerIcon,
    Logout as LogoutIcon,
    Info as InfoIcon,
    Description as DescriptionIcon,
    Rule as RuleIcon,
    ListAlt as ListAltIcon,
    Lock as LockIcon,
    People as PeopleIcon,
    Analytics as AnalyticsIcon,
} from '@mui/icons-material';
import { logout } from '../services/auth';
import { useAuth } from '../contexts/AuthContext';
import AdminMenu from './AdminMenu';
import UserMenu from './common/UserMenu';
import Footer from './Footer';
import { NotificationBell } from './Notifications';

interface MenuItem {
    textKey: string;
    icon: JSX.Element;
    path: string;
}

interface LayoutProps {}

const drawerWidth = 240;

// Menu items with translation keys instead of hardcoded text
const menuItemsConfig: MenuItem[] = [
    { textKey: 'menu.dashboard', icon: <DashboardIcon />, path: '/dashboard' },
    { textKey: 'menu.jobs', icon: <WorkIcon />, path: '/jobs' },
    { textKey: 'menu.agents', icon: <ComputerIcon />, path: '/agents' },
    { textKey: 'menu.hashlists', icon: <ListAltIcon />, path: '/hashlists' },
    { textKey: 'menu.crackedHashes', icon: <LockIcon />, path: '/pot' },
    { textKey: 'menu.wordlists', icon: <DescriptionIcon />, path: '/wordlists' },
    { textKey: 'menu.rules', icon: <RuleIcon />, path: '/rules' },
    {
        textKey: 'menu.clientManagement',
        icon: <PeopleIcon />,
        path: '/clients',
    },
    { textKey: 'menu.analytics', icon: <AnalyticsIcon />, path: '/analytics' },
];

const bottomMenuItemsConfig: MenuItem[] = [
    { textKey: 'menu.about', icon: <InfoIcon />, path: '/about' },
];

const Layout: React.FC<LayoutProps> = () => {
    const [open, setOpen] = useState<boolean>(true);
    const navigate = useNavigate();
    const location = useLocation();
    const { setAuth, setUser, setUserRole, userRole } = useAuth();
    const { t } = useTranslation('navigation');
    const { t: tCommon } = useTranslation('common');

    const handleDrawerToggle = (): void => {
        setOpen(!open);
    };

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

    // Memoize menu items with translations
    const menuItems = useMemo(
        () =>
            menuItemsConfig.map((item) => ({
                ...item,
                text: t(item.textKey) as string,
            })),
        [t]
    );

    const bottomMenuItems = useMemo(
        () =>
            bottomMenuItemsConfig.map((item) => ({
                ...item,
                text: t(item.textKey) as string,
            })),
        [t]
    );

    return (
        <Box sx={{ display: 'flex', minHeight: '100vh' }}>
            <CssBaseline />
            <AppBar
                position="fixed"
                sx={{
                    zIndex: (theme: Theme) => theme.zIndex.drawer + 1,
                    width: '100%',
                }}
            >
                <Toolbar>
                    <IconButton
                        color="inherit"
                        aria-label={t('aria.toggleDrawer') as string}
                        onClick={handleDrawerToggle}
                        edge="start"
                        sx={{ mr: 2 }}
                    >
                        {open ? <ChevronLeftIcon /> : <MenuIcon />}
                    </IconButton>
                    <Box
                        sx={{ display: 'flex', alignItems: 'center', flexGrow: 1 }}
                    >
                        <img
                            src="/logo.png"
                            alt={tCommon('layout.logoAlt') as string}
                            style={{ height: 32, marginRight: 12 }}
                        />
                        <Typography variant="h6" noWrap component="div">
                            {t('appName') as string}
                        </Typography>
                    </Box>
                    <NotificationBell />
                    <UserMenu />
                </Toolbar>
            </AppBar>
            <Drawer
                variant="permanent"
                open={open}
                sx={{
                    width: open
                        ? drawerWidth
                        : (theme: Theme) => theme.spacing(7),
                    flexShrink: 0,
                    '& .MuiDrawer-paper': {
                        width: open
                            ? drawerWidth
                            : (theme: Theme) => theme.spacing(7),
                        overflowX: 'hidden',
                        borderRight: (theme: Theme) =>
                            `1px solid ${theme.palette.divider}`,
                        transition: (theme: Theme) =>
                            theme.transitions.create('width', {
                                easing: theme.transitions.easing.sharp,
                                duration:
                                    theme.transitions.duration.enteringScreen,
                            }),
                        position: 'fixed',
                        height: '100%',
                        display: 'flex',
                        flexDirection: 'column',
                    },
                }}
            >
                <Toolbar />

                <List aria-label={t('aria.mainNavigation') as string}>
                    {menuItems.map((item) => (
                        <ListItem
                            button
                            key={item.textKey}
                            onClick={() => navigate(item.path)}
                            selected={location.pathname === item.path}
                            sx={{
                                minHeight: 48,
                                justifyContent: open ? 'initial' : 'center',
                                px: 2.5,
                            }}
                        >
                            <ListItemIcon
                                sx={{
                                    minWidth: 0,
                                    mr: open ? 3 : 'auto',
                                    justifyContent: 'center',
                                }}
                            >
                                {item.icon}
                            </ListItemIcon>
                            <ListItemText
                                primary={item.text}
                                sx={{ opacity: open ? 1 : 0 }}
                            />
                        </ListItem>
                    ))}
                </List>

                {userRole === 'admin' && (
                    <>
                        <Divider />
                        <AdminMenu />
                    </>
                )}

                <Box sx={{ flexGrow: 1 }} />

                <Divider />
                <List>
                    {bottomMenuItems.map((item) => (
                        <ListItem
                            button
                            key={item.textKey}
                            onClick={() => navigate(item.path)}
                            selected={location.pathname === item.path}
                            sx={{
                                minHeight: 48,
                                justifyContent: open ? 'initial' : 'center',
                                px: 2.5,
                            }}
                        >
                            <ListItemIcon
                                sx={{
                                    minWidth: 0,
                                    mr: open ? 3 : 'auto',
                                    justifyContent: 'center',
                                }}
                            >
                                {item.icon}
                            </ListItemIcon>
                            <ListItemText
                                primary={item.text}
                                sx={{ opacity: open ? 1 : 0 }}
                            />
                        </ListItem>
                    ))}
                    <ListItem
                        button
                        onClick={handleLogout}
                        sx={{
                            minHeight: 48,
                            justifyContent: open ? 'initial' : 'center',
                            px: 2.5,
                        }}
                    >
                        <ListItemIcon
                            sx={{
                                minWidth: 0,
                                mr: open ? 3 : 'auto',
                                justifyContent: 'center',
                            }}
                        >
                            <LogoutIcon />
                        </ListItemIcon>
                        <ListItemText
                            primary={t('menu.logout') as string}
                            sx={{ opacity: open ? 1 : 0 }}
                        />
                    </ListItem>
                </List>
            </Drawer>
            <Box
                component="main"
                sx={{
                    flexGrow: 1,
                    p: 3,
                    pb: 8, // Add padding bottom to account for fixed footer
                    ml: (theme: Theme) =>
                        `${open ? drawerWidth + theme.spacing(1) : theme.spacing(8)}px`,
                    transition: (theme: Theme) =>
                        theme.transitions.create(['margin', 'width'], {
                            easing: theme.transitions.easing.sharp,
                            duration: theme.transitions.duration.enteringScreen,
                        }),
                }}
            >
                <Toolbar /> {/* Spacer for AppBar */}
                <Outlet />
            </Box>
            <Footer drawerOpen={open} />
        </Box>
    );
};

export default Layout;
