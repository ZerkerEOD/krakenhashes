import React from 'react';
import { Box, CircularProgress, Fade, InputAdornment, Tooltip } from '@mui/material';
import CheckIcon from '@mui/icons-material/Check';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import { useTranslation } from 'react-i18next';
import type { FieldSaveStatus } from './context';

interface Props {
  status: FieldSaveStatus;
  /** Render inline (after a switch label) instead of as an input adornment. */
  inline?: boolean;
}

/**
 * The per-field save indicator: spinner while saving, a green tick that fades
 * after two seconds, a red mark with the server's message on failure. Also
 * announces the state to screen readers.
 */
const FieldSaveAdornment: React.FC<Props> = ({ status, inline }) => {
  const { t } = useTranslation('admin');
  const { state, error } = status;

  const content = (
    <>
      <Box
        component="span"
        sx={{
          position: 'absolute',
          width: 1,
          height: 1,
          overflow: 'hidden',
          clip: 'rect(0 0 0 0)',
          whiteSpace: 'nowrap',
        }}
        aria-live="polite"
      >
        {state === 'saving' && (t('fieldState.saving') as string)}
        {state === 'saved' && (t('fieldState.saved') as string)}
        {state === 'error' && `${t('fieldState.error') as string}: ${error ?? ''}`}
      </Box>
      <Fade in={state !== 'idle'} timeout={{ enter: 150, exit: 400 }}>
        <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', minWidth: 20, justifyContent: 'center' }}>
          {state === 'saving' && <CircularProgress size={16} />}
          {state === 'saved' && <CheckIcon fontSize="small" color="success" />}
          {state === 'error' && (
            <Tooltip title={error ?? (t('fieldState.error') as string)}>
              <ErrorOutlineIcon fontSize="small" color="error" />
            </Tooltip>
          )}
        </Box>
      </Fade>
    </>
  );

  if (inline) {
    return (
      <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', ml: 1 }}>
        {content}
      </Box>
    );
  }
  return <InputAdornment position="end">{content}</InputAdornment>;
};

export default FieldSaveAdornment;
