import React from 'react';
import { Box, IconButton, Stack, Typography } from '@mui/material';
import CloseIcon from '@mui/icons-material/Close';
import { useTranslation } from 'react-i18next';

interface BulkActionBarProps {
  count: number;
  onClear: () => void;
  children: React.ReactNode;
}

const BulkActionBar: React.FC<BulkActionBarProps> = ({ count, onClear, children }) => {
  const { t } = useTranslation('common');
  return (
    <Box
      role="toolbar"
      aria-label={t('table.selectedCount', { count }) as string}
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        px: 2,
        py: 1,
        minHeight: 61,
        bgcolor: 'surface.selected',
        borderBottom: 1,
        borderColor: 'divider',
      }}
    >
      <IconButton size="small" onClick={onClear} aria-label={t('table.clearSelection') as string}>
        <CloseIcon fontSize="small" />
      </IconButton>
      <Typography variant="subtitle2" sx={{ mr: 1 }}>
        {t('table.selectedCount', { count }) as string}
      </Typography>
      <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
        {children}
      </Stack>
    </Box>
  );
};

export default BulkActionBar;
