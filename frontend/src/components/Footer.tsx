import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Typography,
  Link,
  useTheme,
  useMediaQuery,
  Theme,
} from '@mui/material';
import {
  Description as DocsIcon,
  GitHub as GitHubIcon,
  BugReport as BugIcon,
} from '@mui/icons-material';
import DiscordIcon from './icons/DiscordIcon';
import { getVersionInfo } from '../api/version';

interface FooterProps {
  drawerOpen: boolean;
}

const Footer: React.FC<FooterProps> = ({ drawerOpen }) => {
  const { t } = useTranslation('common');
  const [version, setVersion] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'));
  
  const drawerWidth = 240;
  
  useEffect(() => {
    const fetchVersion = async () => {
      try {
        const versionInfo = await getVersionInfo();
        setVersion(versionInfo.release || versionInfo.backend || '');
      } catch (error) {
        console.error('Failed to fetch version:', error);
        setVersion('');
      } finally {
        setLoading(false);
      }
    };

    fetchVersion();
  }, []);

  const currentYear = new Date().getFullYear();
  const copyrightText = `© 2024-${currentYear} ZerkerEOD`;

  return (
    <Box
      component="footer"
      sx={{
        position: 'fixed',
        bottom: 0,
        left: isMobile ? 0 : (drawerOpen ? drawerWidth : (theme: Theme) => theme.spacing(7)),
        right: 0,
        backgroundColor: theme.palette.background.paper,
        borderTop: `1px solid ${theme.palette.divider}`,
        py: 1.5,
        px: 3,
        zIndex: theme.zIndex.drawer - 1,
        display: 'flex',
        flexDirection: isMobile ? 'column' : 'row',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: isMobile ? 1 : 2,
        transition: theme.transitions.create(['left'], {
          easing: theme.transitions.easing.sharp,
          duration: theme.transitions.duration.enteringScreen,
        }),
      }}
    >
      {/* Copyright */}
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ 
          display: 'flex',
          alignItems: 'center',
          minWidth: isMobile ? 'auto' : '200px',
        }}
      >
        {copyrightText}
      </Typography>

      {/* Links */}
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 3,
        }}
      >
        <Link
          href="https://zerkereod.github.io/krakenhashes/"
          target="_blank"
          rel="noopener noreferrer"
          color="text.secondary"
          underline="hover"
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 0.5,
            fontSize: '0.75rem',
            '&:hover': {
              color: 'primary.main',
            },
          }}
        >
          <DocsIcon sx={{ fontSize: 16 }} />
          {t('footer.documentation')}
        </Link>

        <Link
          href="https://discord.com/invite/taafA9cSFV"
          target="_blank"
          rel="noopener noreferrer"
          color="text.secondary"
          underline="hover"
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 0.5,
            fontSize: '0.75rem',
            '&:hover': {
              color: 'primary.main',
            },
          }}
        >
          <DiscordIcon sx={{ fontSize: 16 }} />
          Discord
        </Link>

        <Link
          href="https://github.com/ZerkerEOD/krakenhashes"
          target="_blank"
          rel="noopener noreferrer"
          color="text.secondary"
          underline="hover"
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 0.5,
            fontSize: '0.75rem',
            '&:hover': {
              color: 'primary.main',
            },
          }}
        >
          <GitHubIcon sx={{ fontSize: 16 }} />
          GitHub
        </Link>

        <Link
          href="https://github.com/ZerkerEOD/krakenhashes/issues"
          target="_blank"
          rel="noopener noreferrer"
          color="text.secondary"
          underline="hover"
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 0.5,
            fontSize: '0.75rem',
            '&:hover': {
              color: 'primary.main',
            },
          }}
        >
          <BugIcon sx={{ fontSize: 16 }} />
          {t('footer.issues')}
        </Link>
      </Box>

      {/* Version */}
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{
          minWidth: isMobile ? 'auto' : '200px',
          textAlign: isMobile ? 'center' : 'right',
        }}
      >
        {loading ? t('footer.loading') : version ? t('footer.release', { version }) : t('footer.versionUnavailable')}
      </Typography>
    </Box>
  );
};

export default Footer;