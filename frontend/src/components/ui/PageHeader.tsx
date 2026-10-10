import React from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { Box, Breadcrumbs, IconButton, Link, Skeleton, Stack, Typography } from '@mui/material';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';

export interface Crumb {
  label: React.ReactNode;
  to?: string;
}

export interface PageHeaderProps {
  title: React.ReactNode;
  description?: React.ReactNode;
  /** Primary actions, rendered top-right (CLAUDE.md layout standard). */
  actions?: React.ReactNode;
  breadcrumbs?: Crumb[];
  backTo?: string;
  /** Chips/status shown inline after the title. */
  status?: React.ReactNode;
  loading?: boolean;
  /** Smaller variant for section headers inside a page. */
  size?: 'page' | 'section';
  sx?: any;
}

const PageHeader: React.FC<PageHeaderProps> = ({
  title,
  description,
  actions,
  breadcrumbs,
  backTo,
  status,
  loading,
  size = 'page',
  sx,
}) => {
  const isPage = size === 'page';
  return (
    <Box sx={{ mb: isPage ? 3 : 2, ...sx }}>
      {breadcrumbs && breadcrumbs.length > 0 && (
        <Breadcrumbs sx={{ mb: 1, fontSize: '0.8125rem' }} aria-label="breadcrumb">
          {breadcrumbs.map((c, i) =>
            c.to && i < breadcrumbs.length - 1 ? (
              <Link key={i} component={RouterLink} to={c.to} underline="hover" color="text.secondary">
                {c.label}
              </Link>
            ) : (
              <Typography key={i} color="text.primary" fontSize="inherit">
                {c.label}
              </Typography>
            )
          )}
        </Breadcrumbs>
      )}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 2, flexWrap: 'wrap' }}>
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1, minWidth: 0 }}>
          {backTo && (
            <IconButton component={RouterLink} to={backTo} size="small" sx={{ mt: 0.25 }} aria-label="back">
              <ArrowBackIcon fontSize="small" />
            </IconButton>
          )}
          <Box sx={{ minWidth: 0 }}>
            <Stack direction="row" alignItems="center" spacing={1.5} flexWrap="wrap" useFlexGap>
              {loading ? (
                <Skeleton width={240} height={32} />
              ) : (
                <Typography variant={isPage ? 'h4' : 'h5'} component={isPage ? 'h1' : 'h2'} sx={{ wordBreak: 'break-word' }}>
                  {title}
                </Typography>
              )}
              {status}
            </Stack>
            {description && (
              <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                {description}
              </Typography>
            )}
          </Box>
        </Box>
        {actions && (
          <Stack direction="row" spacing={1} alignItems="center" flexShrink={0}>
            {actions}
          </Stack>
        )}
      </Box>
    </Box>
  );
};

export default PageHeader;
