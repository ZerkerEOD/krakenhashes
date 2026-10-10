import React from 'react';
import { Box, Button, Typography } from '@mui/material';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import { useTranslation } from 'react-i18next';
import { getErrorMessage } from '../../utils/errors';

export interface ErrorStateProps {
  error?: unknown;
  title?: React.ReactNode;
  onRetry?: () => void;
  compact?: boolean;
  sx?: any;
}

const ErrorState: React.FC<ErrorStateProps> = ({ error, title, onRetry, compact, sx }) => {
  const { t } = useTranslation('common');
  const message = error !== undefined ? getErrorMessage(error, t('errors.unknownError') as string) : undefined;

  return (
    <Box
      role="alert"
      sx={{
        display: 'flex',
        flexDirection: compact ? 'row' : 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: compact ? 1.5 : 1,
        textAlign: compact ? 'left' : 'center',
        py: compact ? 2 : 6,
        px: 3,
        color: 'text.secondary',
        ...sx,
      }}
    >
      <ErrorOutlineIcon color="error" sx={{ fontSize: compact ? 24 : 40 }} />
      <Box sx={{ flexGrow: compact ? 1 : 0 }}>
        <Typography variant={compact ? 'body2' : 'subtitle1'} color="text.primary" fontWeight={600}>
          {title ?? (t('labels.error') as string)}
        </Typography>
        {message && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, wordBreak: 'break-word' }}>
            {message}
          </Typography>
        )}
      </Box>
      {onRetry && (
        <Button size="small" variant="outlined" color="inherit" onClick={onRetry} sx={{ mt: compact ? 0 : 1.5 }}>
          {t('buttons.retry') as string}
        </Button>
      )}
    </Box>
  );
};

export default ErrorState;
