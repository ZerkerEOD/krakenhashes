import React from 'react';
import { Box, Grid, Paper, Skeleton, Stack } from '@mui/material';

export type PageSkeletonVariant = 'list' | 'detail' | 'form' | 'dashboard' | 'settings';

export interface PageSkeletonProps {
  variant?: PageSkeletonVariant;
  /** Omit the page padding when rendering inside an already-padded container. */
  inset?: boolean;
}

const Header = () => (
  <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
    <Box>
      <Skeleton width={220} height={32} />
      <Skeleton width={340} height={20} />
    </Box>
    <Skeleton variant="rounded" width={140} height={36} />
  </Box>
);

export const TableSkeleton: React.FC<{ rows?: number; columns?: number }> = ({ rows = 8, columns = 5 }) => (
  <Box sx={{ p: 1.5 }}>
    <Box sx={{ display: 'flex', gap: 2, mb: 1.5 }}>
      {Array.from({ length: columns }).map((_, i) => (
        <Skeleton key={i} height={18} sx={{ flex: i === 0 ? 2 : 1 }} />
      ))}
    </Box>
    {Array.from({ length: rows }).map((_, r) => (
      <Box key={r} sx={{ display: 'flex', gap: 2, py: 1 }}>
        {Array.from({ length: columns }).map((_, c) => (
          <Skeleton key={c} height={16} sx={{ flex: c === 0 ? 2 : 1 }} />
        ))}
      </Box>
    ))}
  </Box>
);

const Card: React.FC<{ lines?: number; height?: number }> = ({ lines = 4, height }) => (
  <Paper variant="outlined" sx={{ p: 2.5, height }}>
    <Skeleton width="40%" height={22} sx={{ mb: 1.5 }} />
    <Stack spacing={1}>
      {Array.from({ length: lines }).map((_, i) => (
        <Skeleton key={i} height={16} width={`${90 - i * 12}%`} />
      ))}
    </Stack>
  </Paper>
);

const PageSkeleton: React.FC<PageSkeletonProps> = ({ variant = 'list', inset }) => {
  let body: React.ReactNode;
  switch (variant) {
    case 'detail':
      body = (
        <Grid container spacing={3}>
          <Grid item xs={12} md={8}>
            <Stack spacing={3}>
              <Card lines={5} />
              <Paper variant="outlined">
                <TableSkeleton rows={5} />
              </Paper>
            </Stack>
          </Grid>
          <Grid item xs={12} md={4}>
            <Stack spacing={3}>
              <Card lines={3} />
              <Card lines={4} />
            </Stack>
          </Grid>
        </Grid>
      );
      break;
    case 'form':
      body = (
        <Stack spacing={3} sx={{ maxWidth: 720 }}>
          <Card lines={6} />
          <Card lines={4} />
        </Stack>
      );
      break;
    case 'dashboard':
      body = (
        <Grid container spacing={3}>
          {Array.from({ length: 4 }).map((_, i) => (
            <Grid item xs={6} md={3} key={i}>
              <Paper variant="outlined" sx={{ p: 2.5 }}>
                <Skeleton width="50%" height={16} />
                <Skeleton width="70%" height={36} />
              </Paper>
            </Grid>
          ))}
          <Grid item xs={12} md={8}>
            <Paper variant="outlined">
              <TableSkeleton rows={6} />
            </Paper>
          </Grid>
          <Grid item xs={12} md={4}>
            <Card lines={6} />
          </Grid>
        </Grid>
      );
      break;
    case 'settings':
      body = (
        <Box sx={{ display: 'flex', gap: 3 }}>
          <Stack spacing={1} sx={{ width: 220, flexShrink: 0 }}>
            {Array.from({ length: 7 }).map((_, i) => (
              <Skeleton key={i} height={32} />
            ))}
          </Stack>
          <Stack spacing={3} sx={{ flexGrow: 1 }}>
            <Card lines={5} />
            <Card lines={3} />
          </Stack>
        </Box>
      );
      break;
    case 'list':
    default:
      body = (
        <Paper variant="outlined">
          <Box sx={{ display: 'flex', gap: 2, p: 2, borderBottom: 1, borderColor: 'divider' }}>
            <Skeleton variant="rounded" width={260} height={36} />
            <Box sx={{ flexGrow: 1 }} />
            <Skeleton variant="rounded" width={110} height={36} />
          </Box>
          <TableSkeleton />
        </Paper>
      );
  }

  return (
    <Box sx={{ p: inset ? 0 : 3 }} aria-busy="true" aria-live="polite">
      <Header />
      {body}
    </Box>
  );
};

export default PageSkeleton;
