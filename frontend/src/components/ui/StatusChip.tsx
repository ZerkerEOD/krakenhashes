import React from 'react';
import { Box, Chip, ChipProps, Tooltip } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { StatusEntity, getStatusTone, humanizeStatus, isActiveStatus } from './statusMaps';
import { pulse, useReducedMotion } from './motion';
import type { StatusTone } from '../../styles/palette';

export interface StatusChipProps extends Omit<ChipProps, 'color' | 'label' | 'variant'> {
  entity: StatusEntity;
  status: string | null | undefined;
  /** Override the label; defaults to the translated status. */
  label?: React.ReactNode;
  variant?: 'filled' | 'outlined' | 'dot';
  /** Animate the dot/chip for active statuses (off under reduced motion). */
  pulse?: boolean;
  tooltip?: React.ReactNode;
}

const toneToChipColor = (tone: StatusTone): ChipProps['color'] => (tone === 'default' ? 'default' : tone);

export const useStatusLabel = () => {
  const { t } = useTranslation('common');
  return (entity: StatusEntity, status: string | null | undefined): string => {
    if (!status) return t('labels.unknown') as string;
    const key = String(status).toLowerCase();
    return t(`status.${entity}.${key}`, { defaultValue: t(`status.generic.${key}`, { defaultValue: humanizeStatus(key) }) }) as string;
  };
};

/**
 * The one way to render a status. Colour comes from `statusMaps`, label from
 * `common:status.<entity>.<status>` (falls back to a humanised key).
 */
const StatusChip: React.FC<StatusChipProps> = ({
  entity,
  status,
  label,
  variant = 'outlined',
  pulse: pulseProp,
  tooltip,
  size = 'small',
  sx,
  ...rest
}) => {
  const statusLabel = useStatusLabel();
  const reduced = useReducedMotion();
  const tone = getStatusTone(entity, status);
  const active = isActiveStatus(entity, status);
  const shouldPulse = (pulseProp ?? active) && !reduced;
  const text = label ?? statusLabel(entity, status);

  if (variant === 'dot') {
    const dot = (
      <Box
        component="span"
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.75,
          fontSize: '0.8125rem',
          whiteSpace: 'nowrap',
          ...sx,
        }}
      >
        <Box
          component="span"
          sx={{
            width: 8,
            height: 8,
            borderRadius: '50%',
            flexShrink: 0,
            bgcolor: tone === 'default' ? 'text.disabled' : `${tone}.main`,
            animation: shouldPulse ? `${pulse} 1.6s ease-in-out infinite` : undefined,
          }}
        />
        {text}
      </Box>
    );
    return tooltip ? <Tooltip title={tooltip}>{dot}</Tooltip> : dot;
  }

  const chip = (
    <Chip
      size={size}
      label={text}
      color={toneToChipColor(tone)}
      variant={variant === 'filled' && tone !== 'idle' && tone !== 'default' ? 'filled' : 'outlined'}
      sx={{
        fontWeight: 600,
        // Leading status dot on every chip (one consistent look); it pulses only
        // while the status is active (running, processing, …).
        '&::before': {
          content: '""',
          width: 6,
          height: 6,
          borderRadius: '50%',
          bgcolor: 'currentColor',
          ml: 1,
          mr: -0.5,
          flexShrink: 0,
          animation: shouldPulse ? `${pulse} 1.6s ease-in-out infinite` : undefined,
        },
        ...sx,
      }}
      {...rest}
    />
  );
  return tooltip ? <Tooltip title={tooltip}>{chip}</Tooltip> : chip;
};

export default StatusChip;
