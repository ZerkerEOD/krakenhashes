import React from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { Box, Button, Typography } from '@mui/material';
import InboxOutlinedIcon from '@mui/icons-material/InboxOutlined';

export interface EmptyStateProps {
  icon?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  action?: { label: React.ReactNode; onClick?: () => void; to?: string; icon?: React.ReactNode };
  size?: 'sm' | 'md';
  /** Dashed outline, for empty containers that invite adding something. */
  variant?: 'plain' | 'dashed';
  sx?: any;
}

const EmptyState: React.FC<EmptyStateProps> = ({ icon, title, description, action, size = 'md', variant = 'plain', sx }) => {
  const small = size === 'sm';
  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        textAlign: 'center',
        py: small ? 3 : 6,
        px: 3,
        gap: 0.75,
        color: 'text.secondary',
        ...(variant === 'dashed' ? { border: 1, borderStyle: 'dashed', borderColor: 'divider', borderRadius: 2 } : {}),
        ...sx,
      }}
    >
      <Box sx={{ color: 'text.disabled', '& svg': { fontSize: small ? 28 : 40 }, mb: 0.5 }}>
        {icon ?? <InboxOutlinedIcon />}
      </Box>
      <Typography variant={small ? 'body2' : 'subtitle1'} color="text.primary" fontWeight={600}>
        {title}
      </Typography>
      {description && (
        <Typography variant="body2" color="text.secondary" sx={{ maxWidth: 440 }}>
          {description}
        </Typography>
      )}
      {action && (
        <Button
          size="small"
          variant="outlined"
          startIcon={action.icon}
          onClick={action.onClick}
          {...(action.to ? { component: RouterLink, to: action.to } : {})}
          sx={{ mt: 1.5 }}
        >
          {action.label}
        </Button>
      )}
    </Box>
  );
};

export default EmptyState;
