import React, { useCallback, useMemo, useState } from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Checkbox,
  Chip,
  CircularProgress,
  Link,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import { useTranslation } from 'react-i18next';

import { getCloudCapacity } from '../../../services/cloud';
import {
  CloudCapacityReport,
  HardwareCapacity,
  PlacementCapacity,
  SignalKind,
} from '../../../types/cloud';

/**
 * One selected placement, in the shape the provider's settings store it.
 *
 * Deliberately generic: AWS writes {zone, zone_id, subnet_id, instance_types}
 * and RunPod will write {data_center_id, gpu_type_ids}, but the picker only
 * needs "which placement, and which hardware within it". The caller adapts.
 */
export interface PlacementSelection {
  /** Stable provider key — PlacementCapacity.id. */
  id: string;
  /** Display alias — PlacementCapacity.name. */
  name: string;
  /** What a launch needs to reach here — PlacementCapacity.ref. */
  ref: string;
  /** EMPTY MEANS ALL, never none. See the toggle logic below. */
  hardware: string[];
}

/**
 * How the two axes relate for this provider.
 *
 *  matrix — hardware is chosen PER placement, because that is how the provider
 *           actually models capacity. AWS spot capacity belongs to the
 *           (instance type, zone) pair, so "g4dn in 2a but not in 2b" is a
 *           statement the config can hold and the launch path can honour.
 *
 *  axes   — placements and hardware are chosen INDEPENDENTLY and combined.
 *           Vast.ai stores two flat lists, so ticking a card applies it
 *           everywhere. The grid still shows real per-cell availability, but
 *           the checkboxes have to behave the way the settings do or the screen
 *           promises a distinction that is silently discarded on save.
 */
export type SelectionMode = 'matrix' | 'axes';

interface Props {
  /** Saved provider config id. Null while a provider is being created. */
  providerConfigId: string | null;
  mode?: SelectionMode;
  selection: PlacementSelection[];
  /**
   * Hardware keys known from the form before any probe has run, so the grid is
   * usable immediately. The probe's list supersedes it.
   */
  fallbackHardware: string[];
  /** Shown when it will be superseded by a non-empty selection. Optional. */
  legacyRefNote?: string;
  onChange: (selection: PlacementSelection[]) => void;
}

const scoreColor = (score?: number): 'default' | 'success' | 'warning' => {
  if (!score) return 'default';
  if (score >= 7) return 'success';
  return 'warning';
};

const centsToUSD = (cents?: number): string =>
  cents && cents > 0 ? `$${(cents / 100).toFixed(2)}` : '—';

/**
 * ProviderCapacityPicker turns "paste an identifier you looked up in another
 * tab" into "tick the places and cards you are happy to rent".
 *
 * WHY THIS EXISTS. Capacity generally belongs to the (hardware, placement)
 * PAIR rather than to either alone. On EC2 spot this is literal: g4dn.xlarge
 * exhausted in us-east-2b says nothing about g4dn.xlarge in us-east-2c, they
 * are different physical inventories. A configuration with one placement and
 * one hardware type therefore has exactly ONE capacity pool, and when it is
 * empty the launch retry loop has nowhere to go — every candidate resolves to
 * the same exhausted inventory. Measured on a live deployment: fifteen
 * consecutive InsufficientInstanceCapacity refusals against a single pool, then
 * a success within two scheduler cycles of widening to three instance types
 * across two zones. The pool count under the grid is the number that matters.
 *
 * PROVIDER-AGNOSTIC BY CONSTRUCTION. The axis names, the columns and how much
 * each column is worth all come from the report, because the three providers
 * differ far more in signal quality than in structure — AWS has no stock count
 * at all and a placement score that has been measured contradicting reality,
 * while Vast.ai and RunPod return real inventory. Rendering those identically
 * is how an operator unticks a pool that would have worked.
 */
const ProviderCapacityPicker: React.FC<Props> = ({
  providerConfigId,
  mode = 'matrix',
  selection,
  fallbackHardware,
  legacyRefNote,
  onChange,
}) => {
  // 'admin' namespace, matching every sibling in this directory. Without it
  // react-i18next resolves against the default namespace, finds nothing, and
  // renders the raw key — so the whole grid reads as "cloud.providers.zones.*"
  // with no error anywhere to say why.
  const { t } = useTranslation('admin');
  const [report, setReport] = useState<CloudCapacityReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const probe = useCallback(async () => {
    if (!providerConfigId) return;
    setLoading(true);
    setError(null);
    try {
      setReport(await getCloudCapacity(providerConfigId));
    } catch (e: any) {
      setError(
        e?.response?.data?.error ?? e?.message ?? (t('cloud.providers.zones.probeFailed') as string)
      );
    } finally {
      setLoading(false);
    }
  }, [providerConfigId, t]);

  const selected = useMemo(() => {
    const map = new Map<string, PlacementSelection>();
    selection.forEach((s) => map.set(s.id, s));
    return map;
  }, [selection]);

  /** Which signals this provider can actually fill, for column rendering. */
  const has = useCallback(
    (kind: SignalKind) => (report?.signals ?? []).some((s) => s.kind === kind),
    [report]
  );

  const hardwareColumns = report?.hardware?.length ? report.hardware : fallbackHardware;

  const togglePlacement = (pc: PlacementCapacity, on: boolean) => {
    if (!on) {
      onChange(selection.filter((s) => s.id !== pc.id));
      return;
    }
    // Empty hardware list is the deliberate default: "any card I have approved".
    onChange([
      ...selection.filter((s) => s.id !== pc.id),
      { id: pc.id, name: pc.name, ref: pc.ref ?? '', hardware: [] },
    ]);
  };

  const toggleHardware = (pc: PlacementCapacity, hw: string, on: boolean) => {
    if (mode === 'axes') {
      /*
       * One shared hardware list. Applying the toggle to every selected
       * placement keeps the grid an honest picture of what will be saved —
       * letting the checkbox differ per row would show a per-placement choice
       * the settings cannot express and the save would quietly flatten.
       */
      onChange(
        selection.map((s) => {
          const current = s.hardware.length > 0 ? s.hardware : [...hardwareColumns];
          const next = on
            ? Array.from(new Set([...current, hw]))
            : current.filter((x) => x !== hw);
          return { ...s, hardware: next.length === hardwareColumns.length ? [] : next };
        })
      );
      return;
    }
    const existing = selected.get(pc.id);
    if (!existing) {
      // Ticking a cell in an unselected placement selects the placement with
      // just that card, which is what the click plainly means.
      if (!on) return;
      onChange([
        ...selection,
        { id: pc.id, name: pc.name, ref: pc.ref ?? '', hardware: [hw] },
      ]);
      return;
    }

    /*
     * An empty list means "all", so unticking one card has to materialise the
     * full list first and then remove from it. Otherwise the click either does
     * nothing visible or — worse — reads as "none".
     */
    const current = existing.hardware.length > 0 ? existing.hardware : [...hardwareColumns];
    const next = on
      ? Array.from(new Set([...current, hw]))
      : current.filter((x) => x !== hw);

    if (next.length === 0) {
      // No acceptable card left is the same statement as not using the place.
      onChange(selection.filter((s) => s.id !== pc.id));
      return;
    }
    onChange(
      selection.map((s) =>
        s.id === pc.id
          ? { ...s, hardware: next.length === hardwareColumns.length ? [] : next }
          : s
      )
    );
  };

  const selectAll = () => {
    if (!report) return;
    onChange(
      report.placements
        .filter((p) => p.usable)
        .map((p) => ({ id: p.id, name: p.name, ref: p.ref ?? '', hardware: [] }))
    );
  };

  const isHardwareSelected = (pc: PlacementCapacity, hw: string): boolean => {
    if (mode === 'axes') {
      // Shared across placements, so any entry answers for all of them.
      const any = selection[0];
      if (!any) return false;
      return any.hardware.length === 0 || any.hardware.includes(hw);
    }
    const sel = selected.get(pc.id);
    if (!sel) return false;
    return sel.hardware.length === 0 || sel.hardware.includes(hw);
  };

  /*
   * Pool count is computed from the LIVE form state, not report.pool_count:
   * the report reflects what is SAVED, and an operator ticking boxes needs to
   * watch this number move as they tick them.
   */
  const poolCount = useMemo(() => {
    if (!hardwareColumns.length) return 0;
    return selection.reduce((sum, s) => {
      const pc = report?.placements.find((p) => p.id === s.id);
      const hw = s.hardware.length > 0 ? s.hardware : hardwareColumns;
      if (!pc) return sum + hw.length;
      return sum + hw.filter((h) => pc.hardware.find((c) => c.id === h)?.offered).length;
    }, 0);
  }, [selection, hardwareColumns, report]);

  /** The per-cell body under each checkbox: whichever signals exist. */
  const renderCellSignals = (cell?: HardwareCapacity) => {
    if (!cell) return null;
    return (
      <>
        {has('available_count') && cell.available_count ? (
          <Tooltip title={t('cloud.providers.zones.availableCountHelp') as string}>
            <Typography variant="caption" color="success.main">
              {t('cloud.providers.zones.availableCount', { count: cell.available_count }) as string}
            </Typography>
          </Tooltip>
        ) : null}
        {has('live_price') && cell.live_cents ? (
          <Tooltip
            title={
              cell.configured_cents
                ? (t('cloud.providers.zones.spotVsConfigured', {
                    spot: centsToUSD(cell.live_cents),
                    configured: centsToUSD(cell.configured_cents),
                  }) as string)
                : (t('cloud.providers.zones.livePriceHelp') as string)
            }
          >
            <Typography
              variant="caption"
              color={
                cell.configured_cents && cell.live_cents > cell.configured_cents
                  ? 'error.main'
                  : 'text.secondary'
              }
            >
              {centsToUSD(cell.live_cents)}
            </Typography>
          </Tooltip>
        ) : null}
        {has('score') && cell.score ? (
          <Tooltip title={t('cloud.providers.zones.scoreHelp') as string}>
            <Chip
              size="small"
              variant="outlined"
              color={scoreColor(cell.score)}
              label={`${cell.score}/10`}
              sx={{ height: 18, fontSize: '0.65rem' }}
            />
          </Tooltip>
        ) : null}
      </>
    );
  };

  return (
    <Box sx={{ mt: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <Box>
          <Typography variant="subtitle2">
            {report
              ? `${report.placement_label} × ${report.hardware_label}`
              : (t('cloud.providers.zones.title') as string)}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {t('cloud.providers.zones.help') as string}
          </Typography>
        </Box>
        <Box sx={{ display: 'flex', gap: 1, flexShrink: 0 }}>
          {report && (
            <Button size="small" onClick={selectAll}>
              {t('cloud.providers.zones.selectAll') as string}
            </Button>
          )}
          <Tooltip
            title={
              providerConfigId
                ? (t('cloud.providers.zones.probeHelp') as string)
                : (t('cloud.providers.zones.saveFirst') as string)
            }
          >
            <span>
              <Button
                size="small"
                variant="outlined"
                disabled={!providerConfigId || loading}
                startIcon={loading ? <CircularProgress size={14} /> : <RefreshIcon />}
                onClick={probe}
              >
                {t('cloud.providers.zones.probe') as string}
              </Button>
            </span>
          </Tooltip>
        </Box>
      </Box>

      {!providerConfigId && (
        <Alert severity="info" sx={{ mb: 2 }}>
          {t('cloud.providers.zones.saveFirst') as string}
        </Alert>
      )}

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      {report?.warnings?.map((w, i) => (
        <Alert severity="warning" key={i} sx={{ mb: 1 }}>
          {w}
        </Alert>
      ))}

      {report && (
        <Paper variant="outlined" sx={{ overflowX: 'auto', mb: 1 }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell padding="checkbox" />
                <TableCell>{report.placement_label}</TableCell>
                {hardwareColumns.map((hw) => (
                  <TableCell key={hw} align="center">
                    {hw}
                  </TableCell>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {report.placements.map((pc) => {
                const placementSelected = selected.has(pc.id);
                return (
                  <TableRow key={pc.id} hover>
                    <TableCell padding="checkbox">
                      <Tooltip title={pc.usable ? '' : (t('cloud.providers.zones.unusable') as string)}>
                        <span>
                          <Checkbox
                            size="small"
                            checked={placementSelected}
                            disabled={!pc.usable}
                            onChange={(e) => togglePlacement(pc, e.target.checked)}
                          />
                        </span>
                      </Tooltip>
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2">{pc.name}</Typography>
                      {pc.id !== pc.name && (
                        <Typography variant="caption" color="text.secondary" display="block">
                          {pc.id}
                        </Typography>
                      )}
                      {pc.detail && (
                        <Typography variant="caption" color="text.secondary" display="block">
                          {pc.detail}
                        </Typography>
                      )}
                      {has('score') && pc.score ? (
                        <Tooltip title={t('cloud.providers.zones.combinedScoreHelp') as string}>
                          <Chip
                            size="small"
                            variant="outlined"
                            color={scoreColor(pc.score)}
                            label={t('cloud.providers.zones.combinedScore', { score: pc.score }) as string}
                            sx={{ height: 18, fontSize: '0.65rem', mt: 0.5 }}
                          />
                        </Tooltip>
                      ) : null}
                      {pc.notes?.map((n, i) => (
                        <Typography key={i} variant="caption" color="warning.main" display="block">
                          {n}
                        </Typography>
                      ))}
                    </TableCell>
                    {hardwareColumns.map((hw) => {
                      const cell = pc.hardware.find((c) => c.id === hw);
                      const offered = cell?.offered ?? false;
                      return (
                        <TableCell key={hw} align="center">
                          {!offered ? (
                            <Tooltip title={t('cloud.providers.zones.notOffered') as string}>
                              <Typography variant="caption" color="text.disabled">
                                —
                              </Typography>
                            </Tooltip>
                          ) : (
                            <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                              <Checkbox
                                size="small"
                                checked={isHardwareSelected(pc, hw)}
                                disabled={!pc.usable}
                                onChange={(e) => toggleHardware(pc, hw, e.target.checked)}
                              />
                              {renderCellSignals(cell)}
                            </Box>
                          )}
                        </TableCell>
                      );
                    })}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Paper>
      )}

      {/*
        * The signal legend. Shown because the columns above are NOT equally
        * trustworthy and nothing in a grid of numbers says so — an operator has
        * no way to know that one column is the provider stating a fact about its
        * catalogue and the next is an opinion measured contradicting reality.
        */}
      {mode === 'axes' && report && (
        <Alert severity="info" sx={{ mb: 1 }}>
          {t('cloud.providers.zones.axesModeNote') as string}
        </Alert>
      )}

      {report?.signals && report.signals.length > 0 && (
        <Box sx={{ mb: 1 }}>
          {report.signals.map((s) => (
            <Tooltip key={s.kind} title={s.explanation}>
              <Chip
                size="small"
                variant="outlined"
                sx={{ mr: 0.5, mb: 0.5 }}
                color={
                  s.trust === 'definitive' ? 'success' : s.trust === 'measured' ? 'info' : 'default'
                }
                label={`${s.label} · ${t(`cloud.providers.zones.trust.${s.trust}`) as string}`}
              />
            </Tooltip>
          ))}
        </Box>
      )}

      {(report || selection.length > 0) && (
        <Alert severity={poolCount <= 1 ? 'warning' : 'success'} sx={{ mb: 1 }}>
          <AlertTitle>
            {t('cloud.providers.zones.poolCount', { count: poolCount }) as string}
          </AlertTitle>
          {poolCount <= 1
            ? (t('cloud.providers.zones.poolCountWarning') as string)
            : (t('cloud.providers.zones.poolCountOk') as string)}
        </Alert>
      )}

      {legacyRefNote && selection.length > 0 && (
        <Alert severity="info" sx={{ mb: 1 }}>
          {t('cloud.providers.zones.legacyOverridden', { subnet: legacyRefNote }) as string}
        </Alert>
      )}

      {report === null && providerConfigId && !loading && (
        <Typography variant="body2" color="text.secondary">
          <Link component="button" type="button" onClick={probe} underline="hover">
            {t('cloud.providers.zones.probe') as string}
          </Link>{' '}
          {t('cloud.providers.zones.probeIdle') as string}
        </Typography>
      )}
    </Box>
  );
};

export default ProviderCapacityPicker;
