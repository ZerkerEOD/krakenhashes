import React, { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Pagination,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import { useTranslation, Trans } from 'react-i18next';
import { api } from '../../services/api';
import { SimpleTable, SimpleColumn } from '../ui';

export interface ValidationInvalidEntry {
  id?: number;
  line_number: number;
  content: string;
  reason: string;
}

export interface ValidationPreviewProps {
  open: boolean;
  hashlistId: number | null;
  hashlistName: string;
  currentHashTypeId: number;
  totalInputLines: number;
  validCount: number;
  invalidCount: number;
  truncated: boolean;
  initialSample: ValidationInvalidEntry[];
  onProceed: () => void;
  onCancel: () => void;
}

interface HashTypeOption {
  id: number;
  name: string;
}

interface RevalidatedSnapshot {
  hashTypeId: number;
  hashTypeName: string;
  totalInputLines: number;
  validCount: number;
  invalidCount: number;
  truncated: boolean;
  sample: ValidationInvalidEntry[];
}

const PAGE_SIZE = 50;

// ValidationPreviewDialog shows the user the list of malformed lines the
// upload validator flagged (GitHub issue #38). It handles three outcomes:
//
//   - validCount > 0  → "Proceed with N valid hashes" + Cancel
//   - validCount == 0 → user picks a different hash type and clicks
//                       "Re-validate" — usually they picked the wrong type.
//                       The file stays on disk; the backend swaps hash_type_id,
//                       drops stale invalid_hashes rows, and re-runs validation.
//   - any state       → Cancel & fix deletes the upload entirely.
const ValidationPreviewDialog: React.FC<ValidationPreviewProps> = ({
  open,
  hashlistId,
  hashlistName,
  currentHashTypeId,
  totalInputLines: propTotalInputLines,
  validCount: propValidCount,
  invalidCount: propInvalidCount,
  truncated: propTruncated,
  initialSample,
  onProceed,
  onCancel,
}) => {
  // Counters etc. start from the props (the initial upload outcome) but get
  // replaced if the user re-validates with a different hash type.
  const { t } = useTranslation('hashlists');
  const [snapshot, setSnapshot] = useState<RevalidatedSnapshot | null>(null);
  const totalInputLines = snapshot?.totalInputLines ?? propTotalInputLines;
  const validCount = snapshot?.validCount ?? propValidCount;
  const invalidCount = snapshot?.invalidCount ?? propInvalidCount;
  const truncated = snapshot?.truncated ?? propTruncated;

  const [page, setPage] = useState(1);
  const [items, setItems] = useState<ValidationInvalidEntry[]>(initialSample);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState<'proceed' | 'cancel' | 'revalidate' | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selectedHashType, setSelectedHashType] = useState<HashTypeOption | null>(null);

  const { data: hashTypes = [] } = useQuery<HashTypeOption[]>({
    queryKey: ['hashTypes'],
    queryFn: async () => {
      const r = await api.get<HashTypeOption[] | { data: HashTypeOption[] }>('/api/hashtypes');
      const arr = Array.isArray(r.data) ? r.data : r.data?.data;
      return Array.isArray(arr) ? arr : [];
    },
    enabled: open,
    staleTime: 5 * 60 * 1000,
  });

  const invalidColumns: SimpleColumn<ValidationInvalidEntry>[] = useMemo(() => [
    { field: 'line_number', headerName: t('validationPreview.columns.line') as string, width: 80 },
    {
      field: 'content',
      headerName: t('validationPreview.columns.hashTruncated') as string,
      render: (row) => (
        <Tooltip title={row.content} arrow placement="top-start">
          <Box
            component="code"
            sx={{
              display: 'block',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              maxWidth: 480,
              fontFamily: (th) => th.typography.monoFamily,
            }}
          >
            {row.content}
          </Box>
        </Tooltip>
      ),
    },
    { field: 'reason', headerName: t('validationPreview.columns.reason') as string, width: 320 },
  ], [t]);

  // Reset state when reopened with a fresh hashlist.
  useEffect(() => {
    if (open) {
      setPage(1);
      setItems(initialSample);
      setError(null);
      setSubmitting(null);
      setSnapshot(null);
      setSelectedHashType(null);
    }
  }, [open, hashlistId, initialSample]);

  // Pre-fill the hash-type picker with the current type once the list loads
  // so the dropdown opens with a clear "you're currently set to X" baseline.
  useEffect(() => {
    if (!selectedHashType && hashTypes.length > 0) {
      const current = hashTypes.find((h) => h.id === currentHashTypeId);
      if (current) setSelectedHashType(current);
    }
  }, [hashTypes, currentHashTypeId, selectedHashType]);

  const currentHashTypeName =
    snapshot?.hashTypeName ??
    hashTypes.find((h) => h.id === currentHashTypeId)?.name ??
    (t('validationPreview.modeFallback', { id: currentHashTypeId }) as string);

  const totalPages = Math.max(1, Math.ceil(invalidCount / PAGE_SIZE));

  const fetchPage = async (nextPage: number) => {
    if (!hashlistId) return;
    setLoading(true);
    setError(null);
    try {
      const resp = await api.get(`/api/hashlists/${hashlistId}/invalid-hashes`, {
        params: { page: nextPage, page_size: PAGE_SIZE },
      });
      setItems(resp.data?.items ?? []);
      setPage(nextPage);
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || (t('validationPreview.errors.loadInvalidHashesFailed') as string));
    } finally {
      setLoading(false);
    }
  };

  const handleConfirm = async (action: 'proceed' | 'cancel') => {
    if (!hashlistId) return;
    setSubmitting(action);
    setError(null);
    try {
      await api.post(`/api/hashlists/${hashlistId}/confirm`, { action });
      if (action === 'proceed') onProceed();
      else onCancel();
    } catch (e: any) {
      setError(
        e?.response?.data?.error ||
          e?.message ||
          (action === 'proceed'
            ? (t('validationPreview.errors.proceedFailed') as string)
            : (t('validationPreview.errors.cancelFailed') as string))
      );
    } finally {
      setSubmitting(null);
    }
  };

  const handleRevalidate = async () => {
    if (!hashlistId || !selectedHashType) return;
    setSubmitting('revalidate');
    setError(null);
    try {
      const resp = await api.put(`/api/hashlists/${hashlistId}/revalidate`, {
        hash_type_id: selectedHashType.id,
      });
      const data = resp.data ?? {};
      // No-validator mode swept the hashlist into processing; close the
      // dialog and let the parent navigate to the detail view.
      if (data.validation_status === 'no_validator') {
        onProceed();
        return;
      }
      setSnapshot({
        hashTypeId: selectedHashType.id,
        hashTypeName: selectedHashType.name,
        totalInputLines: data.total_input_lines ?? 0,
        validCount: data.valid_count ?? 0,
        invalidCount: data.invalid_count ?? 0,
        truncated: !!data.truncated,
        sample: data.sample_invalid ?? [],
      });
      setItems(data.sample_invalid ?? []);
      setPage(1);
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || (t('validationPreview.errors.revalidationFailed') as string));
    } finally {
      setSubmitting(null);
    }
  };

  const allInvalid = validCount === 0 && invalidCount > 0;

  return (
    <Dialog open={open} maxWidth="lg" fullWidth onClose={() => undefined}>
      <DialogTitle>{t('validationPreview.title') as string}</DialogTitle>
      <DialogContent>
        <Alert severity={allInvalid ? 'error' : 'warning'} sx={{ mb: 2 }}>
          {allInvalid ? (
            <>
              <Typography variant="body1">
                <Trans
                  t={t}
                  i18nKey="validationPreview.allInvalid.body1"
                  values={{ totalLines: totalInputLines.toLocaleString(), hashlistName, hashTypeName: currentHashTypeName }}
                  components={{ strong: <strong />, em: <em /> }}
                />
              </Typography>
              <Typography variant="body2" sx={{ mt: 1 }}>
                <Trans t={t} i18nKey="validationPreview.allInvalid.body2" components={{ strong: <strong /> }} />
              </Typography>
            </>
          ) : (
            <Typography variant="body1">
              <Trans
                t={t}
                i18nKey="validationPreview.notAllInvalid.body"
                values={{
                  invalidCount: invalidCount.toLocaleString(),
                  totalLines: totalInputLines.toLocaleString(),
                  hashlistName,
                  validCount: validCount.toLocaleString(),
                }}
                components={{ strong: <strong />, em: <em /> }}
              />
            </Typography>
          )}
          {truncated && (
            <Typography variant="body2" sx={{ mt: 1 }}>
              {t('validationPreview.truncatedNotice') as string}
            </Typography>
          )}
        </Alert>

        {!allInvalid && (
          <DialogContentText sx={{ mb: 2 }}>
            <Trans
              t={t}
              i18nKey="validationPreview.chooseActionText"
              values={{ validCount: validCount.toLocaleString() }}
              components={{ strong: <strong /> }}
            />
          </DialogContentText>
        )}

        {allInvalid && (
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ mb: 2 }}>
            <Autocomplete
              fullWidth
              options={hashTypes}
              getOptionLabel={(o) => t('validationPreview.hashTypeOptionLabel', { id: o.id, name: o.name }) as string}
              isOptionEqualToValue={(a, b) => a.id === b.id}
              value={selectedHashType}
              onChange={(_, v) => setSelectedHashType(v)}
              disabled={submitting !== null}
              renderInput={(params) => (
                <TextField {...params} label={t('validationPreview.hashTypeLabel') as string} size="small" />
              )}
            />
            <Button
              variant="contained"
              color="primary"
              onClick={handleRevalidate}
              disabled={
                submitting !== null ||
                !selectedHashType ||
                selectedHashType.id === (snapshot?.hashTypeId ?? currentHashTypeId)
              }
              sx={{ minWidth: 180 }}
            >
              {submitting === 'revalidate' ? (t('validationPreview.revalidating') as string) : (t('validationPreview.revalidateButton') as string)}
            </Button>
          </Stack>
        )}

        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
            <CircularProgress size={20} />
          </Box>
        ) : (
          <SimpleTable
            rows={items}
            columns={invalidColumns}
            getRowKey={(row) => `${row.line_number}-${row.id ?? ''}`}
            stickyHeader
            maxHeight={480}
            emptyState={{ title: t('validationPreview.noInvalidLines') as string }}
          />
        )}

        {invalidCount > PAGE_SIZE && (
          <Box sx={{ display: 'flex', justifyContent: 'center', mt: 2 }}>
            <Pagination
              count={totalPages}
              page={page}
              onChange={(_, value) => fetchPage(value)}
              disabled={loading || submitting !== null}
              size="small"
            />
          </Box>
        )}
      </DialogContent>
      <DialogActions>
        <Button
          color="error"
          onClick={() => handleConfirm('cancel')}
          disabled={submitting !== null}
        >
          {submitting === 'cancel' ? (t('validationPreview.cancelling') as string) : (t('validationPreview.cancelAndFix') as string)}
        </Button>
        {!allInvalid && (
          <Button
            color="primary"
            variant="contained"
            onClick={() => handleConfirm('proceed')}
            disabled={submitting !== null}
          >
            {submitting === 'proceed'
              ? (t('validationPreview.starting') as string)
              : (t('validationPreview.proceedButton', { validCount: validCount.toLocaleString() }) as string)}
          </Button>
        )}
      </DialogActions>
    </Dialog>
  );
};

export default ValidationPreviewDialog;
