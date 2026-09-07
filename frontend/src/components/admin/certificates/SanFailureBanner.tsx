import React from 'react';
import { Alert, AlertTitle, Button, Typography } from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../../../contexts/AuthContext';
import { getSanFailures } from '../../../services/serverCertificate';

/** Index of the Server Certificate tab in AdminSettings. */
const SERVER_CERTIFICATE_TAB = 11;

/**
 * Surfaces agents that cannot verify this server's certificate.
 *
 * Rendered on the Agent Management page because that is where an admin goes when
 * "the agent isn't showing up" — the symptom they actually experience. The
 * underlying cause, a certificate that does not name the agent's address, is not
 * something they would think to look for under TLS settings.
 *
 * Admin-only: a non-admin cannot act on this, so showing it would be noise.
 */
const SanFailureBanner: React.FC = () => {
  const { t } = useTranslation('admin');
  const navigate = useNavigate();
  const { userRole } = useAuth();
  const isAdmin = userRole === 'admin';

  const { data } = useQuery({
    queryKey: ['tls-san-failures'],
    queryFn: getSanFailures,
    refetchInterval: 30000,
    enabled: isAdmin,
    // A failure here is itself often a symptom of the problem being reported;
    // stay quiet rather than stacking a second error on the page.
    retry: false,
  });

  if (!isAdmin || !data?.active) return null;

  const first = data.addresses[0];
  const agentNames = (first?.agent_names ?? []).join(', ');

  const goToSettings = () => {
    // The admin settings tabs are not routed individually, so the persisted tab
    // index is the only deep-link mechanism available.
    localStorage.setItem('adminSettingsTab', String(SERVER_CERTIFICATE_TAB));
    navigate('/admin/settings');
  };

  return (
    <Alert
      severity="error"
      sx={{ mb: 2 }}
      action={
        <Button color="inherit" size="small" onClick={goToSettings}>
          {t('serverCertificate.banner.action') as string}
        </Button>
      }
    >
      <AlertTitle>
        {t('serverCertificate.banner.title', { count: data.agent_count }) as string}
      </AlertTitle>
      <Typography variant="body2">
        {
          t('serverCertificate.banner.body', {
            agents: agentNames || t('serverCertificate.banner.anAgent'),
            address: first?.address ?? '',
            count: data.distinct_addresses,
          }) as string
        }
      </Typography>
    </Alert>
  );
};

export default SanFailureBanner;
