/**
 * CrackedPassword - renders a cracked password, making $HEX[...] values legible.
 *
 * Ordinary passwords render as-is. A $HEX[...] password (bytes that are not
 * valid UTF-8, GH #90) renders as a best-guess preview plus a "HEX" tag; the
 * tooltip shows the exact $HEX value and explains that the preview is a guess.
 * Callers copying the password should copy the original string, which is the
 * exact, hashcat-compatible value.
 */

import React from 'react';
import { Box, Tooltip } from '@mui/material';
import { useTranslation } from 'react-i18next';
import SensitiveData from './SensitiveData';
import { hexPlainPreview, parseHexPlain } from '../../utils/hexPlain';

interface CrackedPasswordProps {
  password: string;
}

const CrackedPassword: React.FC<CrackedPasswordProps> = ({ password }) => {
  const { t } = useTranslation('common');
  const bytes = parseHexPlain(password);

  if (!bytes) {
    return <SensitiveData>{password}</SensitiveData>;
  }

  return (
    <Tooltip title={t('hexPassword.tooltip', { value: password }) as string}>
      <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}>
        <SensitiveData>{hexPlainPreview(bytes)}</SensitiveData>
        <Box
          component="span"
          sx={{
            px: 0.5,
            border: 1,
            borderColor: 'warning.main',
            borderRadius: 0.5,
            color: 'warning.main',
            fontSize: '0.65rem',
            fontWeight: 'bold',
            lineHeight: 1.4,
            fontFamily: 'monospace',
          }}
        >
          {t('hexPassword.tag')}
        </Box>
      </Box>
    </Tooltip>
  );
};

export default CrackedPassword;
