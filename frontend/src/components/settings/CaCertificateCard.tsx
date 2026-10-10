import React from 'react';
import { Box, Button, Link, Typography } from '@mui/material';
import DownloadIcon from '@mui/icons-material/Download';
import SecurityIcon from '@mui/icons-material/Security';
import { useTranslation } from 'react-i18next';
import SectionCard from '../ui/SectionCard';

/** Download link for the server's CA certificate (self-signed deployments). */
const CaCertificateCard: React.FC = () => {
  const { t } = useTranslation('settings');
  return (
    <SectionCard title={t('security.caCertificate.title') as string} icon={<SecurityIcon color="primary" />}>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('security.caCertificate.description') as string}
      </Typography>
      <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
        <Button
          variant="contained"
          startIcon={<DownloadIcon />}
          onClick={() => window.open(`http://${window.location.hostname}:1337/ca.crt`, '_blank')}
        >
          {t('security.caCertificate.download') as string}
        </Button>
        <Button
          variant="text"
          component={Link}
          href="https://zerkereod.github.io/krakenhashes/admin-guide/system-setup/ssl-tls/"
          target="_blank"
          rel="noopener noreferrer"
        >
          {t('security.caCertificate.docsLink') as string}
        </Button>
      </Box>
    </SectionCard>
  );
};

export default CaCertificateCard;
