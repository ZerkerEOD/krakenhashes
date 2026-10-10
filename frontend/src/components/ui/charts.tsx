import React from 'react';
import { Box, useTheme } from '@mui/material';

/** Recharts colours from the active theme (series, grid, axis text). */
export const useChartColors = () => {
  const theme = useTheme();
  return {
    series: theme.palette.chart,
    grid: theme.palette.divider,
    axis: theme.palette.text.secondary,
    primary: theme.palette.primary.main,
    success: theme.palette.success.main,
    error: theme.palette.error.main,
    warning: theme.palette.warning.main,
    info: theme.palette.info.main,
  };
};

/** Container for a custom Recharts tooltip, readable in both modes. */
export const ChartTooltipBox: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <Box
    sx={{
      bgcolor: 'surface.raised',
      color: 'text.primary',
      border: 1,
      borderColor: 'divider',
      borderRadius: 1,
      p: 1.25,
      boxShadow: 4,
    }}
  >
    {children}
  </Box>
);
