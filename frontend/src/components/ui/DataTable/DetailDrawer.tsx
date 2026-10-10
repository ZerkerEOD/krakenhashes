import React from 'react';
import { Box, Drawer, IconButton, Toolbar, Typography } from '@mui/material';
import CloseIcon from '@mui/icons-material/Close';
import { useTranslation } from 'react-i18next';

interface DetailDrawerProps {
  open: boolean;
  onClose: () => void;
  title?: React.ReactNode;
  width?: number;
  children?: React.ReactNode;
}

/** Right-hand panel used by DataTable's `detail.mode = 'drawer'`. */
const DetailDrawer: React.FC<DetailDrawerProps> = ({ open, onClose, title, width = 560, children }) => {
  const { t } = useTranslation('common');
  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      PaperProps={{ sx: { width: { xs: '100%', sm: width }, maxWidth: '100%', borderLeft: 1, borderColor: 'divider' } }}
    >
      <Toolbar />
      <Box sx={{ display: 'flex', alignItems: 'center', px: 2.5, py: 1.5, borderBottom: 1, borderColor: 'divider' }}>
        <Typography variant="h6" component="h3" sx={{ flexGrow: 1 }} noWrap>
          {title}
        </Typography>
        <IconButton size="small" onClick={onClose} aria-label={t('buttons.close') as string}>
          <CloseIcon fontSize="small" />
        </IconButton>
      </Box>
      <Box sx={{ p: 2.5, overflow: 'auto' }}>{children}</Box>
    </Drawer>
  );
};

export default DetailDrawer;
