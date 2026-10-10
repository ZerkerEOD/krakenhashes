import React, { useState } from 'react';
import { Box, Collapse, Divider, IconButton, LinearProgress, Paper, Stack, Typography } from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import ErrorState from './ErrorState';

export interface SectionCardProps {
  title?: React.ReactNode;
  subtitle?: React.ReactNode;
  /** Right-aligned header content (buttons, chips). */
  actions?: React.ReactNode;
  icon?: React.ReactNode;
  collapsible?: boolean;
  defaultExpanded?: boolean;
  /** Thin progress bar under the header; content stays visible. */
  loading?: boolean;
  error?: unknown;
  onRetry?: () => void;
  /** Remove body padding (tables, lists). */
  flush?: boolean;
  /** Stretch to the parent's height (grid/flex cell); the body takes the remaining space and children can scroll. */
  fill?: boolean;
  dense?: boolean;
  /** Visually flag the card (e.g. danger zone). */
  tone?: 'default' | 'danger' | 'warning';
  id?: string;
  sx?: any;
  children?: React.ReactNode;
}

/** Titled outlined surface: the standard container for a group of settings or a panel. */
const SectionCard: React.FC<SectionCardProps> = ({
  title,
  subtitle,
  actions,
  icon,
  collapsible,
  defaultExpanded = true,
  loading,
  error,
  onRetry,
  flush,
  fill,
  dense,
  tone = 'default',
  id,
  sx,
  children,
}) => {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const hasHeader = Boolean(title || subtitle || actions);
  const pad = dense ? 2 : 2.5;
  const toneSx =
    tone === 'danger'
      ? { borderColor: 'error.main' }
      : tone === 'warning'
      ? { borderColor: 'warning.main' }
      : {};

  return (
    <Paper
      id={id}
      variant="outlined"
      sx={{
        overflow: 'hidden',
        ...(fill ? { height: '100%', display: 'flex', flexDirection: 'column', minHeight: 0 } : {}),
        ...toneSx,
        ...sx,
      }}
    >
      {hasHeader && (
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1.5,
            px: pad,
            py: dense ? 1.25 : 1.75,
            cursor: collapsible ? 'pointer' : undefined,
          }}
          onClick={collapsible ? () => setExpanded((v) => !v) : undefined}
        >
          {icon && <Box sx={{ display: 'flex', color: tone === 'danger' ? 'error.main' : 'text.secondary' }}>{icon}</Box>}
          <Box sx={{ minWidth: 0, flexGrow: 1 }}>
            {title && (
              <Typography variant="h6" component="h3" sx={{ color: tone === 'danger' ? 'error.main' : 'text.primary' }}>
                {title}
              </Typography>
            )}
            {subtitle && (
              <Typography variant="caption" color="text.secondary" component="div">
                {subtitle}
              </Typography>
            )}
          </Box>
          {actions && (
            <Stack direction="row" spacing={1} alignItems="center" onClick={(e) => e.stopPropagation()}>
              {actions}
            </Stack>
          )}
          {collapsible && (
            <IconButton
              size="small"
              aria-expanded={expanded}
              sx={{ transform: expanded ? 'rotate(180deg)' : 'none', transition: 'transform 200ms' }}
            >
              <ExpandMoreIcon fontSize="small" />
            </IconButton>
          )}
        </Box>
      )}
      {hasHeader && <Divider />}
      {loading && <LinearProgress sx={{ height: 2 }} />}
      {fill ? (
        // No Collapse in fill mode: it would break the flex height chain.
        <Box sx={{ p: flush ? 0 : pad, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          {error ? <ErrorState error={error} onRetry={onRetry} compact /> : children}
        </Box>
      ) : (
        <Collapse in={!collapsible || expanded} unmountOnExit={false}>
          {error ? (
            <ErrorState error={error} onRetry={onRetry} compact />
          ) : (
            <Box sx={{ p: flush ? 0 : pad }}>{children}</Box>
          )}
        </Collapse>
      )}
    </Paper>
  );
};

export default SectionCard;
