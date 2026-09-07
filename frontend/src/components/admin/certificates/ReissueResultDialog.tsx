import React from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { ReissueReport } from '../../../types/serverCertificate';

interface ReissueResultDialogProps {
  report: ReissueReport | null;
  onClose: () => void;
}

const ReissueResultDialog: React.FC<ReissueResultDialogProps> = ({ report, onClose }) => {
  const { t } = useTranslation('admin');
  if (!report) return null;

  const nginx = report.propagation.nginx_reload;
  const rotated = report.propagation.agents_action_required === 'refetch-ca';

  return (
    <Dialog open onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>
        {
          t(
            report.reissued
              ? 'serverCertificate.reissue.doneTitle'
              : 'serverCertificate.reissue.noChangeTitle'
          ) as string
        }
      </DialogTitle>
      <DialogContent>
        {!report.reissued && (
          <Typography variant="body2" color="text.secondary">
            {t('serverCertificate.reissue.noChangeBody') as string}
          </Typography>
        )}

        {report.reissued && (
          <Stack spacing={2}>
            {(report.added_sans?.length || report.removed_sans?.length) && (
              <Box>
                <Typography variant="caption" color="text.secondary" display="block" gutterBottom>
                  {t('serverCertificate.reissue.changes') as string}
                </Typography>
                <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', gap: 1 }}>
                  {report.added_sans?.map((san) => (
                    <Chip key={`add-${san}`} size="small" color="success" label={`+ ${san}`} />
                  ))}
                  {report.removed_sans?.map((san) => (
                    <Chip key={`rm-${san}`} size="small" color="warning" label={`− ${san}`} />
                  ))}
                </Stack>
              </Box>
            )}

            {report.certificate && (
              <Typography variant="body2" color="text.secondary">
                {
                  t('serverCertificate.reissue.validUntil', {
                    date: new Date(report.certificate.not_after).toLocaleString(),
                  }) as string
                }
              </Typography>
            )}

            {/* The API is already serving the new certificate; nginx is a
                separate consumer of the same file and may not be. Report each
                independently, because "reissued" with a silently stale :443 is
                a genuinely confusing state to debug. */}
            <Alert severity="success">
              {t('serverCertificate.reissue.backendReloaded') as string}
            </Alert>

            {nginx.attempted && nginx.succeeded && (
              <Alert severity="success">
                {t('serverCertificate.reissue.nginxReloaded') as string}
              </Alert>
            )}

            {nginx.attempted && !nginx.succeeded && (
              <Alert severity="warning">
                <AlertTitle>{t('serverCertificate.reissue.nginxFailed') as string}</AlertTitle>
                <Typography variant="body2" sx={{ mb: 1 }}>
                  {t('serverCertificate.reissue.nginxFailedBody') as string}
                </Typography>
                {/* The underlying error only, with no suggested command: the
                    command would have to name a container or service, and those
                    vary per deployment. Restarting always works. */}
                {nginx.detail && (
                  <Typography
                    variant="caption"
                    component="div"
                    sx={{ fontFamily: 'monospace', wordBreak: 'break-word', opacity: 0.8 }}
                  >
                    {nginx.detail}
                  </Typography>
                )}
              </Alert>
            )}

            {report.warnings?.map((warning) => (
              <Alert key={warning} severity="warning">
                {warning}
              </Alert>
            ))}

            {report.backup_dir && (
              <Typography variant="caption" color="text.secondary">
                {t('serverCertificate.reissue.backup', { path: report.backup_dir }) as string}
              </Typography>
            )}

            <Alert severity="info">
              {
                t(
                  rotated
                    ? 'serverCertificate.reissue.agentsRefetch'
                    : 'serverCertificate.reissue.agentsReconnect'
                ) as string
              }
            </Alert>
          </Stack>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} variant="contained">
          {t('serverCertificate.reissue.close') as string}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default ReissueResultDialog;
