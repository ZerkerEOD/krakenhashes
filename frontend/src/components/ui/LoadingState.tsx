import React from 'react';
import { Box, CircularProgress, Typography } from '@mui/material';

export interface LoadingStateProps {
  variant?: 'spinner' | 'inline' | 'overlay';
  label?: React.ReactNode;
  minHeight?: number | string;
  size?: number;
  sx?: any;
}

/** The one sanctioned spinner. Prefer `PageSkeleton`/`TableSkeleton` for page loads. */
const LoadingState: React.FC<LoadingStateProps> = ({ variant = 'spinner', label, minHeight, size, sx }) => {
  if (variant === 'inline') {
    return (
      <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: 1, ...sx }}>
        <CircularProgress size={size ?? 16} />
        {label && (
          <Typography variant="body2" color="text.secondary" component="span">
            {label}
          </Typography>
        )}
      </Box>
    );
  }
  return (
    <Box
      role="status"
      aria-busy="true"
      sx={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 1.5,
        minHeight: minHeight ?? (variant === 'overlay' ? '100%' : 160),
        ...(variant === 'overlay'
          ? { position: 'absolute', inset: 0, bgcolor: 'background.paper', opacity: 0.85, zIndex: 1 }
          : {}),
        ...sx,
      }}
    >
      <CircularProgress size={size ?? 32} />
      {label && (
        <Typography variant="body2" color="text.secondary">
          {label}
        </Typography>
      )}
    </Box>
  );
};

export default LoadingState;
