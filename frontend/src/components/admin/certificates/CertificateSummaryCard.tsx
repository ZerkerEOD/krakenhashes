import React from 'react';
import { Alert, Box, Chip, Divider, Grid, Paper, Stack, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { CertificateStatus } from '../../../types/serverCertificate';

interface CertificateSummaryCardProps {
  status: CertificateStatus;
}

const formatDate = (value?: string): string => {
  if (!value) return '—';
  return new Date(value).toLocaleString();
};

/** Expiry chip colour: warn a month out, escalate inside a week. */
const expiryColor = (days: number): 'default' | 'warning' | 'error' => {
  if (days < 7) return 'error';
  if (days < 30) return 'warning';
  return 'default';
};

const Field: React.FC<{ label: string; value: React.ReactNode }> = ({ label, value }) => (
  <Grid item xs={12} sm={6}>
    <Typography variant="caption" color="text.secondary" display="block">
      {label}
    </Typography>
    <Typography variant="body2" sx={{ wordBreak: 'break-all' }}>
      {value}
    </Typography>
  </Grid>
);

const CertificateSummaryCard: React.FC<CertificateSummaryCardProps> = ({ status }) => {
  const { t } = useTranslation('admin');
  const cert = status.certificate;

  // Read back from the LIVE certificate, not from configuration. The entire
  // point of this panel is to show what the server actually presents, which is
  // exactly what the old environment-variable flow could not tell anyone.
  const dnsNames = cert?.dns_names ?? [];
  const ipAddresses = cert?.ip_addresses ?? [];

  const pendingIp = (status.effective_sans_preview?.ip_addresses ?? []).filter(
    (v) => !ipAddresses.includes(v)
  );
  const pendingDns = (status.effective_sans_preview?.dns_names ?? []).filter(
    (v) => !dnsNames.includes(v)
  );
  const pendingCount = pendingIp.length + pendingDns.length;

  return (
    <Paper sx={{ p: 3 }}>
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}>
        <Typography variant="subtitle1">
          {t('serverCertificate.summary.title') as string}
        </Typography>
        <Chip size="small" label={status.tls_mode} />
        {cert && (
          <Chip
            size="small"
            color={expiryColor(cert.days_remaining)}
            label={
              t('serverCertificate.summary.daysRemaining', { count: cert.days_remaining }) as string
            }
          />
        )}
      </Stack>

      {!cert && (
        <Alert severity="warning">
          {t('serverCertificate.summary.unavailable') as string}
        </Alert>
      )}

      {cert && (
        <>
          <Grid container spacing={2}>
            <Field label={t('serverCertificate.summary.subject') as string} value={cert.subject} />
            <Field label={t('serverCertificate.summary.issuer') as string} value={cert.issuer} />
            <Field
              label={t('serverCertificate.summary.validity') as string}
              value={`${formatDate(cert.not_before)} → ${formatDate(cert.not_after)}`}
            />
            <Field label={t('serverCertificate.summary.serial') as string} value={cert.serial} />
          </Grid>

          <Divider sx={{ my: 2 }} />

          <Typography variant="caption" color="text.secondary" display="block" gutterBottom>
            {t('serverCertificate.summary.covers') as string}
          </Typography>
          <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', gap: 1 }}>
            {ipAddresses.map((ip) => (
              <Chip key={`ip-${ip}`} size="small" label={`IP: ${ip}`} />
            ))}
            {dnsNames.map((name) => (
              <Chip key={`dns-${name}`} size="small" label={`DNS: ${name}`} />
            ))}
            {ipAddresses.length === 0 && dnsNames.length === 0 && (
              <Typography variant="body2" color="text.secondary">
                {t('serverCertificate.summary.coversNothing') as string}
              </Typography>
            )}
          </Stack>

          {status.ca && (
            <>
              <Divider sx={{ my: 2 }} />
              <Grid container spacing={2}>
                <Field
                  label={t('serverCertificate.summary.caSubject') as string}
                  value={status.ca.subject}
                />
                <Field
                  label={t('serverCertificate.summary.caExpires') as string}
                  value={formatDate(status.ca.not_after)}
                />
                <Grid item xs={12}>
                  <Typography variant="caption" color="text.secondary" display="block">
                    {t('serverCertificate.summary.caFingerprint') as string}
                  </Typography>
                  <Typography variant="caption" sx={{ wordBreak: 'break-all', fontFamily: 'monospace' }}>
                    {status.ca.fingerprint_sha256}
                  </Typography>
                </Grid>
              </Grid>
            </>
          )}
        </>
      )}

      {pendingCount > 0 && (
        <Alert severity="info" sx={{ mt: 2 }}>
          {t('serverCertificate.summary.pending', { count: pendingCount }) as string}
        </Alert>
      )}

      {status.settings?.deprecated_env_present && (
        <Alert severity="warning" sx={{ mt: 2 }}>
          {t('serverCertificate.summary.deprecatedEnv') as string}
        </Alert>
      )}

      {!status.managed && status.unmanaged_reason && (
        <Alert severity="info" sx={{ mt: 2 }}>
          {status.unmanaged_reason}
        </Alert>
      )}
    </Paper>
  );
};

export default CertificateSummaryCard;
