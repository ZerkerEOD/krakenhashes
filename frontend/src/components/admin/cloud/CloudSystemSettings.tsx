import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Grid,
  InputAdornment,
  Paper,
  TextField,
  Typography,
} from '@mui/material';
import { Save as SaveIcon, Undo as UndoIcon } from '@mui/icons-material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { getSystemSettings, updateSystemSetting } from '../../../services/systemSettings';

/**
 * System-wide cloud settings.
 *
 * These keys are seeded by the cloud-provisioning migrations and, until this
 * panel existed, had no UI at all — they could only be changed with SQL or a
 * hand-written call to the settings API. That mattered most for
 * `cloud_global_monthly_cap_cents`, which defaults to 0 and where 0 means
 * "provisioning is off", not "unlimited": the feature could be fully configured
 * and simply never do anything, with nothing on screen saying why.
 *
 * Deliberately separate from the Policy tab. Policy is the budget *ladder* —
 * the percentages at which a client's spend triggers notify/drain/stop. This is
 * the set of absolute system ceilings and the timing of the machinery that
 * enforces them. Mixing a hard dollar cap in with tuning percentages is how the
 * cap ends up read as advisory.
 *
 * Edits are explicit: type, then Save. An earlier revision saved each field on
 * blur, following JobExecutionSettings. That is wrong for this panel — these
 * are spend ceilings, blur-to-save gives no confirmation that a number was
 * accepted, and switching tabs discards the edit with no warning that anything
 * was lost. Writes still go one key at a time through PUT /admin/settings/{key}
 * so saving here cannot stamp or clobber an unrelated key.
 */

/** How one setting maps between its stored string and what the operator sees. */
interface FieldSpec {
  key: string;
  /** Free text rather than a number, e.g. a container image reference. */
  text?: boolean;
  toDisplay?: (stored: number) => number;
  toStored?: (shown: number) => number;
}

const FIELDS = {
  monthlyCapCents: {
    key: 'cloud_global_monthly_cap_cents',
    // Stored in cents, shown in dollars: "1000" read as ten dollars is a 100x
    // error in the one field that bounds total spend.
    toDisplay: (cents: number) => Math.round(cents) / 100,
    toStored: (dollars: number) => Math.round(dollars * 100),
  },
  concurrentCap: { key: 'cloud_global_concurrent_instance_cap' },
  agentImage: { key: 'cloud_agent_image', text: true },
  chunkSeconds: { key: 'cloud_chunk_duration_seconds' },
  teardownSlack: { key: 'cloud_teardown_slack_seconds' },
  idleDrainMinutes: { key: 'cloud_idle_drain_minutes' },
  reaperSeconds: { key: 'cloud_reaper_interval_seconds' },
  orphanGraceMinutes: { key: 'cloud_orphan_grace_minutes' },
} satisfies Record<string, FieldSpec>;

const ALL_FIELDS: FieldSpec[] = Object.values(FIELDS);

type SettingsMap = Record<string, string>;

/** The display string for a spec, given the raw stored value. */
const toDisplayString = (spec: FieldSpec, stored: string | undefined): string => {
  const raw = stored ?? '';
  if (spec.text) return raw;
  const parsed = parseFloat(raw);
  const value = isNaN(parsed) ? 0 : parsed;
  return String(spec.toDisplay ? spec.toDisplay(value) : value);
};

/** The stored string for a spec, given what the operator typed. Null if unusable. */
const toStoredString = (spec: FieldSpec, shown: string): string | null => {
  if (spec.text) return shown.trim();
  const trimmed = shown.trim();
  if (trimmed === '') return null;
  const parsed = Number(trimmed);
  if (isNaN(parsed)) return null;
  return String(spec.toStored ? spec.toStored(parsed) : parsed);
};

/*
 * DECLARED AT MODULE SCOPE, NOT INSIDE THE COMPONENT.
 *
 * A component defined in the render body gets a new function identity on every
 * render. React compares element types by identity, so it does not see an
 * update to an existing input — it sees a different component type in that
 * position, unmounts the old subtree and mounts a new one. The DOM node is
 * replaced and focus lands on the body, so the caret is lost after every single
 * character typed. Hoisting keeps the identity stable, the node mounted, and
 * the caret where the operator left it.
 */
interface SettingFieldProps {
  label: string;
  helper: string;
  value: string;
  onChange: (raw: string) => void;
  disabled: boolean;
  dirty?: boolean;
  unit?: string;
  type?: 'number' | 'text';
  min?: number;
  step?: number;
  placeholder?: string;
}

const SettingField: React.FC<SettingFieldProps> = ({
  label,
  helper,
  value,
  onChange,
  disabled,
  dirty,
  unit,
  type = 'number',
  min = 0,
  step,
  placeholder,
}) => (
  <TextField
    fullWidth
    type={type}
    label={label}
    value={value}
    placeholder={placeholder}
    onChange={(e) => onChange(e.target.value)}
    disabled={disabled}
    helperText={helper}
    focused={dirty || undefined}
    color={dirty ? 'warning' : undefined}
    InputLabelProps={placeholder ? { shrink: true } : undefined}
    InputProps={{
      inputProps: type === 'number' ? { min, step } : undefined,
      endAdornment: unit ? <InputAdornment position="end">{unit}</InputAdornment> : undefined,
    }}
  />
);

const CloudSystemSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const { enqueueSnackbar } = useSnackbar();

  /** Last known server state, as raw stored strings. */
  const [values, setValues] = useState<SettingsMap>({});
  /** In-progress edits, as display strings. Absent means "not edited". */
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchSettings = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getSystemSettings();
      const map: SettingsMap = {};
      data.forEach((s) => {
        map[s.key] = s.value ?? '';
      });
      setValues(map);
      setDrafts({});
    } catch (err: any) {
      setError(err?.response?.data?.error || (t('cloud.system.loadFailed') as string));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchSettings();
  }, [fetchSettings]);

  /** Specs whose draft differs from what the server holds. */
  const dirtyFields = useMemo(
    () =>
      ALL_FIELDS.filter((spec) => {
        const draft = drafts[spec.key];
        if (draft === undefined) return false;
        const stored = toStoredString(spec, draft);
        return stored !== null && stored !== (values[spec.key] ?? '');
      }),
    [drafts, values]
  );

  const isDirty = (key: string) => dirtyFields.some((f) => f.key === key);

  const handleSave = async () => {
    if (!dirtyFields.length) return;
    setSaving(true);
    setError(null);

    const saved: SettingsMap = {};
    const failures: string[] = [];

    // Sequential and per key: a failure part-way through must leave the keys
    // that already succeeded reflected in the UI rather than rolled back in the
    // display only, which would misreport what the server holds.
    for (const spec of dirtyFields) {
      const next = toStoredString(spec, drafts[spec.key]);
      if (next === null) continue;
      try {
        await updateSystemSetting(spec.key, next);
        saved[spec.key] = next;
      } catch (err: any) {
        failures.push(`${spec.key}: ${err?.response?.data?.error || err?.message || 'failed'}`);
      }
    }

    setValues((v) => ({ ...v, ...saved }));
    setDrafts((d) => {
      const next = { ...d };
      Object.keys(saved).forEach((k) => delete next[k]);
      return next;
    });
    setSaving(false);

    if (failures.length) {
      setError(failures.join('; '));
      enqueueSnackbar(t('cloud.system.saveFailed') as string, { variant: 'error' });
    } else {
      enqueueSnackbar(t('cloud.system.saved') as string, { variant: 'success' });
    }
  };

  const bind = (spec: FieldSpec) => ({
    value: drafts[spec.key] !== undefined ? drafts[spec.key] : toDisplayString(spec, values[spec.key]),
    onChange: (raw: string) => setDrafts((d) => ({ ...d, [spec.key]: raw })),
    disabled: loading || saving,
    dirty: isDirty(spec.key),
    ...(spec.text ? { type: 'text' as const } : {}),
  });

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  const capCents = parseInt(values[FIELDS.monthlyCapCents.key] ?? '', 10);

  return (
    <Box>
      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      {/*
        * The single most common "nothing happens" cause. The cap defaults to 0
        * and 0 disables provisioning outright, so a fully configured provider,
        * a funded client and a cloud-enabled job still produce silence.
        */}
      {(isNaN(capCents) || capCents <= 0) && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          {t('cloud.system.capDisabledWarning') as string}
        </Alert>
      )}

      <Box
        sx={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          mb: 2,
          gap: 2,
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          {dirtyFields.length > 0 && (
            <Chip
              size="small"
              color="warning"
              label={t('cloud.system.unsaved', { count: dirtyFields.length }) as string}
            />
          )}
        </Box>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            startIcon={<UndoIcon />}
            disabled={!dirtyFields.length || saving}
            onClick={() => setDrafts({})}
          >
            {t('cloud.system.discard') as string}
          </Button>
          <Button
            variant="contained"
            startIcon={saving ? <CircularProgress size={16} /> : <SaveIcon />}
            disabled={!dirtyFields.length || saving}
            onClick={handleSave}
          >
            {t('cloud.system.save') as string}
          </Button>
        </Box>
      </Box>

      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" gutterBottom>
          {t('cloud.system.ceilings') as string}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('cloud.system.ceilingsHelp') as string}
        </Typography>
        <Grid container spacing={3}>
          <Grid item xs={12} md={6}>
            <SettingField
              label={t('cloud.system.fields.monthlyCap') as string}
              helper={t('cloud.system.fields.monthlyCapHelp') as string}
              unit="$"
              step={1}
              {...bind(FIELDS.monthlyCapCents)}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SettingField
              label={t('cloud.system.fields.concurrentCap') as string}
              helper={t('cloud.system.fields.concurrentCapHelp') as string}
              {...bind(FIELDS.concurrentCap)}
            />
          </Grid>
        </Grid>
      </Paper>

      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" gutterBottom>
          {t('cloud.system.agent') as string}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('cloud.system.agentHelp') as string}
        </Typography>
        <SettingField
          label={t('cloud.system.fields.agentImage') as string}
          helper={t('cloud.system.fields.agentImageHelp') as string}
          placeholder="zerkereod/krakenhashes-agent-cloud:latest"
          {...bind(FIELDS.agentImage)}
        />
      </Paper>

      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" gutterBottom>
          {t('cloud.system.work') as string}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('cloud.system.workHelp') as string}
        </Typography>
        <Grid container spacing={3}>
          <Grid item xs={12} md={6}>
            <SettingField
              label={t('cloud.system.fields.chunkDuration') as string}
              helper={t('cloud.system.fields.chunkDurationHelp') as string}
              unit={t('cloud.system.units.seconds') as string}
              {...bind(FIELDS.chunkSeconds)}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SettingField
              label={t('cloud.system.fields.teardownSlack') as string}
              helper={t('cloud.system.fields.teardownSlackHelp') as string}
              unit={t('cloud.system.units.seconds') as string}
              {...bind(FIELDS.teardownSlack)}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SettingField
              label={t('cloud.system.fields.idleDrain') as string}
              helper={t('cloud.system.fields.idleDrainHelp') as string}
              unit={t('cloud.system.units.minutes') as string}
              {...bind(FIELDS.idleDrainMinutes)}
            />
          </Grid>
        </Grid>
      </Paper>

      <Paper sx={{ p: 3 }}>
        <Typography variant="h6" gutterBottom>
          {t('cloud.system.reconciliation') as string}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('cloud.system.reconciliationHelp') as string}
        </Typography>
        <Grid container spacing={3}>
          <Grid item xs={12} md={6}>
            <SettingField
              label={t('cloud.system.fields.reaperInterval') as string}
              helper={t('cloud.system.fields.reaperIntervalHelp') as string}
              unit={t('cloud.system.units.seconds') as string}
              min={1}
              {...bind(FIELDS.reaperSeconds)}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SettingField
              label={t('cloud.system.fields.orphanGrace') as string}
              helper={t('cloud.system.fields.orphanGraceHelp') as string}
              unit={t('cloud.system.units.minutes') as string}
              {...bind(FIELDS.orphanGraceMinutes)}
            />
          </Grid>
        </Grid>
      </Paper>
    </Box>
  );
};

export default CloudSystemSettings;
