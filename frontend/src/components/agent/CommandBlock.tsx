import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Box, Typography, IconButton, Tooltip } from '@mui/material';
import { ContentCopy as CopyIcon, Check as CheckIcon } from '@mui/icons-material';

interface CommandBlockProps {
  /** Optional caption shown in the header bar (e.g. "curl", "wget"). */
  label?: string;
  /** The (possibly multi-line) command to display + copy. */
  command: string;
}

/**
 * CommandBlock renders a monospace, copy-to-clipboard command snippet styled
 * like a code card (header bar + body). Shared by the agent install wizard.
 */
export default function CommandBlock({ label, command }: CommandBlockProps) {
  const { t } = useTranslation('agents');
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error('Failed to copy command:', err);
    }
  };

  return (
    <Box
      sx={{
        mb: 1.5,
        borderRadius: 1,
        border: 1,
        borderColor: 'divider',
        overflow: 'hidden',
      }}
    >
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          px: 1.5,
          py: 0.25,
          bgcolor: 'action.hover',
          borderBottom: 1,
          borderColor: 'divider',
        }}
      >
        <Typography
          variant="caption"
          sx={{ fontWeight: 600, color: 'text.secondary', textTransform: 'uppercase', letterSpacing: 0.5 }}
        >
          {label || ' '}
        </Typography>
        <Tooltip title={(copied ? t('install.copied') : t('install.copy')) as string}>
          <IconButton size="small" onClick={handleCopy} color={copied ? 'success' : 'default'}>
            {copied ? <CheckIcon fontSize="small" /> : <CopyIcon fontSize="small" />}
          </IconButton>
        </Tooltip>
      </Box>
      <Box
        component="pre"
        sx={{
          m: 0,
          px: 1.5,
          py: 1.25,
          overflowX: 'auto',
          fontSize: '0.8rem',
          fontFamily: 'monospace',
          lineHeight: 1.7,
          whiteSpace: 'pre',
          bgcolor: (theme) => (theme.palette.mode === 'dark' ? 'rgba(0,0,0,0.35)' : 'grey.50'),
        }}
      >
        {command}
      </Box>
    </Box>
  );
}
