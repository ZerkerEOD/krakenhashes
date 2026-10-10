import React from 'react';
import { ToggleButton, ToggleButtonGroup } from '@mui/material';
import LightModeIcon from '@mui/icons-material/LightMode';
import DarkModeIcon from '@mui/icons-material/DarkMode';
import SettingsBrightnessIcon from '@mui/icons-material/SettingsBrightness';
import { useTranslation } from 'react-i18next';
import { useThemeMode } from '../../contexts/ThemeModeContext';

/** Light / System / Dark selector bound to the theme-mode preference. */
const ThemeModeToggle: React.FC<{ size?: 'small' | 'medium' }> = ({ size = 'small' }) => {
  const { t } = useTranslation('common');
  const { preference, setPreference } = useThemeMode();
  return (
    <ToggleButtonGroup
      exclusive
      size={size}
      value={preference}
      onChange={(_e, v) => v && setPreference(v)}
      aria-label={t('theme.appearance') as string}
    >
      <ToggleButton value="light" aria-label={t('theme.light') as string}>
        <LightModeIcon fontSize="small" sx={{ mr: 0.5 }} />
        {t('theme.light') as string}
      </ToggleButton>
      <ToggleButton value="system" aria-label={t('theme.system') as string}>
        <SettingsBrightnessIcon fontSize="small" sx={{ mr: 0.5 }} />
        {t('theme.system') as string}
      </ToggleButton>
      <ToggleButton value="dark" aria-label={t('theme.dark') as string}>
        <DarkModeIcon fontSize="small" sx={{ mr: 0.5 }} />
        {t('theme.dark') as string}
      </ToggleButton>
    </ToggleButtonGroup>
  );
};

export default ThemeModeToggle;
