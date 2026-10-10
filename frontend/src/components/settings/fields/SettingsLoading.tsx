import React from 'react';
import { Box, Paper, Skeleton, Stack } from '@mui/material';

/** Skeleton for a settings section while its values load. */
const SettingsLoading: React.FC<{ panels?: number; fields?: number }> = ({ panels = 2, fields = 4 }) => (
  <Stack spacing={3} aria-busy="true">
    {Array.from({ length: panels }).map((_, p) => (
      <Paper key={p} variant="outlined" sx={{ p: 2.5 }}>
        <Skeleton width="30%" height={22} sx={{ mb: 2 }} />
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 2 }}>
          {Array.from({ length: fields }).map((_, f) => (
            <Skeleton key={f} variant="rounded" height={40} />
          ))}
        </Box>
      </Paper>
    ))}
  </Stack>
);

export default SettingsLoading;
