import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Grid,
  Paper,
  Typography,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import {
  dismissDiscoveredAddress,
  getCertificateStatus,
  getDiscoveredAddresses,
  rotateCertificateAuthority,
  updateSans,
} from '../../../services/serverCertificate';
import {
  CertificateStatus,
  DiscoveredAddress,
  ReissueReport,
  SanRejection,
} from '../../../types/serverCertificate';
import CertificateSummaryCard from './CertificateSummaryCard';
import DiscoveredAddressesPanel from './DiscoveredAddressesPanel';
import ReissueResultDialog from './ReissueResultDialog';
import RotateCaDangerZone from './RotateCaDangerZone';
import SanListEditor from './SanListEditor';

/** How often the discovered list refreshes while this tab is open. */
const DISCOVERY_POLL_MS = 20000;

const sameSet = (a: string[], b: string[]): boolean =>
  a.length === b.length && a.every((v) => b.includes(v));

/**
 * Admin panel for the addresses the server certificate covers.
 *
 * A single scrolling page rather than nested tabs: the whole task is one
 * decision — which addresses the certificate should name — and hiding the
 * discovered list behind a tab would break the one-click "add what my agent is
 * failing on" flow that is the point of the feature.
 *
 * Saving is one explicit action, deliberately NOT the per-field-on-blur pattern
 * used elsewhere in admin settings. A half-typed address must never be persisted
 * and then baked into a certificate.
 */
const ServerCertificateSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const { enqueueSnackbar } = useSnackbar();

  const [status, setStatus] = useState<CertificateStatus | null>(null);
  const [discovered, setDiscovered] = useState<DiscoveredAddress[]>([]);
  const [ipSans, setIpSans] = useState<string[]>([]);
  const [dnsSans, setDnsSans] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [report, setReport] = useState<ReissueReport | null>(null);
  const [confirmRemoval, setConfirmRemoval] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const editable = status?.managed === true;

  const loadStatus = useCallback(async () => {
    const next = await getCertificateStatus();
    setStatus(next);
    setIpSans(next.settings?.additional_ip_addresses ?? []);
    setDnsSans(next.settings?.additional_dns_names ?? []);
    return next;
  }, []);

  const loadDiscovered = useCallback(async () => {
    try {
      setDiscovered(await getDiscoveredAddresses());
    } catch {
      // Non-fatal: the editor is still usable without suggestions.
    }
  }, []);

  useEffect(() => {
    (async () => {
      try {
        await loadStatus();
        await loadDiscovered();
      } catch (e: any) {
        setError(e?.response?.data?.error ?? e?.message ?? 'Failed to load certificate status');
      } finally {
        setLoading(false);
      }
    })();
  }, [loadStatus, loadDiscovered]);

  // Poll while mounted so an admin watching this page sees an address appear as
  // soon as a failing agent reports it, without reloading.
  useEffect(() => {
    const id = window.setInterval(loadDiscovered, DISCOVERY_POLL_MS);
    return () => window.clearInterval(id);
  }, [loadDiscovered]);

  const dirty = useMemo(() => {
    const savedIp = status?.settings?.additional_ip_addresses ?? [];
    const savedDns = status?.settings?.additional_dns_names ?? [];
    return !sameSet(ipSans, savedIp) || !sameSet(dnsSans, savedDns);
  }, [ipSans, dnsSans, status]);

  /** Addresses currently in the live certificate that this edit would drop. */
  const removedFromCert = useMemo(() => {
    const cert = status?.certificate;
    if (!cert) return [];
    const keeping = new Set([
      ...ipSans,
      ...dnsSans,
      ...(status?.locked?.ip_addresses ?? []),
      ...(status?.locked?.dns_names ?? []),
    ]);
    return [...(cert.ip_addresses ?? []), ...(cert.dns_names ?? [])].filter(
      (v) => !keeping.has(v)
    );
  }, [status, ipSans, dnsSans]);

  const applyChanges = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const result = await updateSans(ipSans, dnsSans, true);
      if ('propagation' in result) {
        setReport(result as ReissueReport);
      }
      await loadStatus();
      await loadDiscovered();
    } catch (e: any) {
      const rejected: SanRejection[] | undefined = e?.response?.data?.rejected;
      if (rejected?.length) {
        // Nothing was persisted: the server writes all-or-nothing, so the saved
        // configuration is unchanged and the working copy still holds the edit.
        setError(rejected.map((r) => `${r.value}: ${r.reason}`).join('\n'));
      } else {
        setError(e?.response?.data?.error ?? e?.message ?? 'Failed to apply changes');
      }
    } finally {
      setBusy(false);
      setConfirmRemoval(null);
    }
  }, [ipSans, dnsSans, loadStatus, loadDiscovered]);

  const handleApply = () => {
    if (removedFromCert.length > 0) {
      setConfirmRemoval(removedFromCert);
      return;
    }
    void applyChanges();
  };

  const handleRotate = async (confirm: string) => {
    setBusy(true);
    setError(null);
    try {
      setReport(await rotateCertificateAuthority(confirm));
      await loadStatus();
    } catch (e: any) {
      setError(e?.response?.data?.error ?? e?.message ?? 'Failed to rotate the certificate authority');
    } finally {
      setBusy(false);
    }
  };

  const handleAddDiscovered = (item: DiscoveredAddress) => {
    if (item.kind === 'ip') {
      if (!ipSans.includes(item.address)) setIpSans([...ipSans, item.address]);
    } else if (!dnsSans.includes(item.address)) {
      setDnsSans([...dnsSans, item.address]);
    }
    enqueueSnackbar(t('serverCertificate.discovered.added', { address: item.address }) as string, {
      variant: 'info',
    });
  };

  const handleDismiss = async (id: number) => {
    try {
      await dismissDiscoveredAddress(id);
      setDiscovered((prev) => prev.filter((d) => d.id !== id));
    } catch (e: any) {
      enqueueSnackbar(e?.message ?? 'Failed to dismiss address', { variant: 'error' });
    }
  };

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Box
        sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}
      >
        <Box>
          <Typography variant="h5" component="h2" gutterBottom>
            {t('serverCertificate.title') as string}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {t('serverCertificate.description') as string}
          </Typography>
        </Box>
        <Button
          variant="contained"
          startIcon={<RefreshIcon />}
          disabled={!editable || busy}
          onClick={handleApply}
        >
          {t('serverCertificate.apply') as string}
        </Button>
      </Box>

      {/* Permanent and non-dismissible. This tool stores cracked credentials;
          the constraint is not a preference the admin can turn off. */}
      <Alert severity="warning" sx={{ mb: 3 }}>
        <AlertTitle>{t('serverCertificate.warning.title') as string}</AlertTitle>
        {t('serverCertificate.warning.body') as string}
      </Alert>

      {error && (
        <Alert severity="error" sx={{ mb: 3, whiteSpace: 'pre-line' }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      <Grid container spacing={3}>
        <Grid item xs={12}>
          {status && <CertificateSummaryCard status={status} />}
        </Grid>

        <Grid item xs={12}>
          <Paper sx={{ p: 3 }}>
            <Typography variant="subtitle1" gutterBottom>
              {t('serverCertificate.sans.title') as string}
            </Typography>
            <Grid container spacing={4} sx={{ mt: 0 }}>
              <Grid item xs={12} md={6}>
                <SanListEditor
                  kind="ip"
                  values={ipSans}
                  onChange={setIpSans}
                  disabled={!editable || busy}
                  locked={status?.locked?.ip_addresses ?? []}
                />
              </Grid>
              <Grid item xs={12} md={6}>
                <SanListEditor
                  kind="dns"
                  values={dnsSans}
                  onChange={setDnsSans}
                  disabled={!editable || busy}
                  locked={status?.locked?.dns_names ?? []}
                />
              </Grid>
            </Grid>
            {dirty && (
              <Alert severity="info" sx={{ mt: 2 }}>
                {t('serverCertificate.sans.unsaved') as string}
              </Alert>
            )}
          </Paper>
        </Grid>

        <Grid item xs={12}>
          <DiscoveredAddressesPanel
            items={discovered}
            onAdd={handleAddDiscovered}
            onDismiss={handleDismiss}
            disabled={!editable || busy}
          />
        </Grid>

        {editable && (
          <Grid item xs={12}>
            <RotateCaDangerZone onRotate={handleRotate} busy={busy} disabled={!editable} />
          </Grid>
        )}
      </Grid>

      <ReissueResultDialog report={report} onClose={() => setReport(null)} />

      <Dialog open={confirmRemoval !== null} onClose={() => setConfirmRemoval(null)}>
        <DialogTitle>{t('serverCertificate.confirmRemoval.title') as string}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {
              t('serverCertificate.confirmRemoval.body', {
                addresses: (confirmRemoval ?? []).join(', '),
              }) as string
            }
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmRemoval(null)}>
            {t('serverCertificate.confirmRemoval.cancel') as string}
          </Button>
          <Button color="warning" variant="contained" onClick={() => void applyChanges()}>
            {t('serverCertificate.confirmRemoval.confirm') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default ServerCertificateSettings;
