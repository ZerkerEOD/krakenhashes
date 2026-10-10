import React from 'react';
import { Box, Grid, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import SectionCard from '../ui/SectionCard';
import LanguageSelector from '../common/LanguageSelector';
import ThemeModeToggle from './ThemeModeToggle';

/** Per-browser preferences: language and appearance. */
const PreferencesCard: React.FC = () => {
  const { t } = useTranslation('settings');
  return (
    <SectionCard title={t('preferences.title') as string}>
      <Grid container spacing={3}>
        <Grid item xs={12} sm={6}>
          <Typography variant="subtitle2" gutterBottom>
            {t('preferences.language') as string}
          </Typography>
          <Box sx={{ display: 'inline-flex' }}>
            <LanguageSelector />
          </Box>
          <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1 }}>
            {t('preferences.languageHelper') as string}
          </Typography>
        </Grid>
        <Grid item xs={12} sm={6}>
          <Typography variant="subtitle2" gutterBottom>
            {t('preferences.theme') as string}
          </Typography>
          <ThemeModeToggle size="medium" />
          <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1 }}>
            {t('preferences.themeHelper') as string}
          </Typography>
        </Grid>
      </Grid>
    </SectionCard>
  );
};

export default PreferencesCard;
