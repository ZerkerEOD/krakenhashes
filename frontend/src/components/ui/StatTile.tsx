import React from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { Box, ButtonBase, Paper, Skeleton, Typography } from '@mui/material';
import TrendingUpIcon from '@mui/icons-material/TrendingUp';
import TrendingDownIcon from '@mui/icons-material/TrendingDown';
import TrendingFlatIcon from '@mui/icons-material/TrendingFlat';
import type { StatusTone } from '../../styles/palette';

export interface StatTileProps {
  label: React.ReactNode;
  value: React.ReactNode;
  icon?: React.ReactNode;
  tone?: StatusTone | 'neutral' | 'primary';
  delta?: { value: number; direction?: 'up' | 'down' | 'flat'; label?: React.ReactNode; good?: boolean };
  caption?: React.ReactNode;
  loading?: boolean;
  to?: string;
  onClick?: () => void;
  dense?: boolean;
  sx?: any;
}

/** Dashboard / summary number with optional trend, icon and link. */
const StatTile: React.FC<StatTileProps> = ({ label, value, icon, tone = 'neutral', delta, caption, loading, to, onClick, dense, sx }) => {
  const accent = tone === 'neutral' || tone === 'default' ? 'text.secondary' : `${tone}.main`;
  const interactive = Boolean(to || onClick);

  const body = (
    <Paper
      variant="outlined"
      sx={{
        p: dense ? 1.75 : 2.25,
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        gap: 0.5,
        position: 'relative',
        overflow: 'hidden',
        transition: 'border-color 150ms, transform 150ms',
        ...(interactive ? { '&:hover': { borderColor: accent, transform: 'translateY(-1px)' } } : {}),
        ...sx,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 1 }}>
        <Typography variant="overline" color="text.secondary" noWrap>
          {label}
        </Typography>
        {icon && <Box sx={{ display: 'flex', color: accent, '& svg': { fontSize: 20 } }}>{icon}</Box>}
      </Box>
      {loading ? (
        <Skeleton width="60%" height={dense ? 30 : 38} />
      ) : (
        <Typography
          variant={dense ? 'h3' : 'h2'}
          component="div"
          sx={{ fontVariantNumeric: 'tabular-nums', lineHeight: 1.1, color: 'text.primary' }}
        >
          {value}
        </Typography>
      )}
      {(delta || caption) && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minHeight: 18 }}>
          {delta && (
            <Box
              component="span"
              sx={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 0.25,
                fontSize: '0.75rem',
                fontWeight: 600,
                color: delta.good === undefined ? 'text.secondary' : delta.good ? 'success.main' : 'error.main',
              }}
            >
              {delta.direction === 'down' ? (
                <TrendingDownIcon sx={{ fontSize: 16 }} />
              ) : delta.direction === 'flat' ? (
                <TrendingFlatIcon sx={{ fontSize: 16 }} />
              ) : (
                <TrendingUpIcon sx={{ fontSize: 16 }} />
              )}
              {delta.value > 0 ? '+' : ''}
              {delta.value}
              {delta.label && <Box component="span" sx={{ color: 'text.secondary', fontWeight: 400, ml: 0.5 }}>{delta.label}</Box>}
            </Box>
          )}
          {caption && (
            <Typography variant="caption" color="text.secondary" noWrap>
              {caption}
            </Typography>
          )}
        </Box>
      )}
    </Paper>
  );

  if (!interactive) return body;
  return (
    <ButtonBase
      {...(to ? { component: RouterLink, to } : {})}
      onClick={onClick}
      sx={{ display: 'block', width: '100%', textAlign: 'left', borderRadius: 2, height: '100%' }}
    >
      {body}
    </ButtonBase>
  );
};

export default StatTile;
