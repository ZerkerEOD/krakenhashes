import React, { useState, useCallback, useEffect } from 'react';
import {
  Box,
  Divider,
  FormControlLabel,
  Grid,
  InputAdornment,
  MenuItem,
  Paper,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { getSystemSettings, updateSystemSetting } from '../../services/systemSettings';

/**
 * Shared admin settings fields, each bound to ONE system setting key.
 *
 * Two properties every page using these gets, which hand-rolled settings forms
 * on this codebase repeatedly did not:
 *
 *  1. A field saves ONLY ITSELF. The older pattern was
 *     `onBlur={() => saveSettings(settingsRef.current)}` against a bulk
 *     endpoint, so blurring one input rewrote every key on the page. That could
 *     clobber a value an operator had just changed elsewhere, and it stamped an
 *     identical updated_at across unrelated settings, destroying the audit
 *     trail — which is how a bad value in one field came to look like a
 *     deliberate reconfiguration of eighteen.
 *
 *  2. The text being typed is held in local `draft` state and is NOT written
 *     into the shared values map until it is saved. An earlier NumberSetting
 *     updated the map on every keystroke and then guarded blur with
 *     `if (next === values[key]) return` — always true by then, so the field
 *     silently never saved and the page had no save button to fall back on.
 *     `values` is server state; `draft` is the edit in progress.
 *
 * Components are declared at MODULE scope deliberately. Defined inside a render
 * body they get a new function identity per render, so React unmounts and
 * remounts the subtree on every keystroke and the caret is lost after each
 * character. Shared state arrives through context rather than props so call
 * sites stay terse.
 */

export type SettingsMap = Record<string, string>;

export interface SettingsCtxValue {
  values: SettingsMap;
  setValues: React.Dispatch<React.SetStateAction<SettingsMap>>;
  saveOne: (key: string, value: string, previous: string) => void;
  loading: boolean;
  savingKey: string | null;
}

export const SettingsCtx = React.createContext<SettingsCtxValue | null>(null);

export const useSettingsCtx = (): SettingsCtxValue => {
  const ctx = React.useContext(SettingsCtx);
  if (!ctx) throw new Error('setting field rendered outside a SettingsCtx provider');
  return ctx;
};

export const numberValueOf = (values: SettingsMap, key: string, fallback = 0): number => {
  const parsed = parseInt(values[key] ?? '', 10);
  return isNaN(parsed) ? fallback : parsed;
};

/**
 * Loads every system setting and exposes the per-key save used by the fields.
 *
 * Returns the context value plus the load error, so a page can render its own
 * loading and error chrome while the save semantics stay identical everywhere.
 */
export const useSystemSettingsForm = (): SettingsCtxValue & {
  error: string | null;
  clearError: () => void;
  reload: () => void;
} => {
  const { t } = useTranslation('admin');
  const { enqueueSnackbar } = useSnackbar();
  const [values, setValues] = useState<SettingsMap>({});
  const [loading, setLoading] = useState(true);
  const [savingKey, setSavingKey] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getSystemSettings();
      const map: SettingsMap = {};
      data.forEach((s) => {
        map[s.key] = s.value ?? '';
      });
      setValues(map);
    } catch (err: any) {
      console.error('Failed to fetch settings:', err);
      setError(err.response?.data?.error || (t('jobExecution.errors.loadFailed') as string));
      enqueueSnackbar(t('jobExecution.messages.loadFailed') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  }, [t, enqueueSnackbar]);

  useEffect(() => {
    reload();
  }, [reload]);

  /**
   * Persist a single setting. Only the key being edited is written, so a save
   * here can never clobber an unrelated setting.
   */
  const saveOne = useCallback(
    async (key: string, value: string, previous: string) => {
      setSavingKey(key);
      setError(null);
      try {
        await updateSystemSetting(key, value);
        enqueueSnackbar(t('jobExecution.messages.updateSuccess') as string, { variant: 'success' });
      } catch (err: any) {
        console.error(`Failed to update ${key}:`, err);
        // Revert just this field — the rest of the page is still server-accurate
        // because nothing else was sent.
        setValues((v) => ({ ...v, [key]: previous }));
        const message = err.response?.data?.error || (t('jobExecution.messages.saveFailed') as string);
        setError(`${key}: ${message}`);
        enqueueSnackbar(message, { variant: 'error' });
      } finally {
        setSavingKey(null);
      }
    },
    [t, enqueueSnackbar]
  );

  const clearError = useCallback(() => setError(null), []);

  return { values, setValues, saveOne, loading, savingKey, error, clearError, reload };
};

/** Number field bound to one setting key; saves on blur (and on Enter). */
export const NumberSetting: React.FC<{
  settingKey: string;
  label: string;
  helper: string;
  min?: number;
  max?: number;
  unit?: string;
  /** Displayed unit differs from the stored unit (e.g. stored seconds, shown minutes). */
  toDisplay?: (stored: number) => number;
  toStored?: (shown: number) => number;
}> = ({ settingKey, label, helper, min, max, unit, toDisplay, toStored }) => {
  const { values, setValues, saveOne, loading, savingKey } = useSettingsCtx();
  const { enqueueSnackbar } = useSnackbar();
  const { t } = useTranslation('admin');
  const [draft, setDraft] = useState<string | null>(null);
  const stored = numberValueOf(values, settingKey);
  const shown = toDisplay ? toDisplay(stored) : stored;

  const commit = () => {
    if (draft === null) return; // never edited — nothing to save
    const text = draft;
    // Drop the draft first: every rejection path below then falls back to
    // rendering the server value, so the field can never be left displaying
    // something that was not persisted.
    setDraft(null);

    const parsed = parseInt(text, 10);
    if (isNaN(parsed)) return;

    // min/max are expressed in DISPLAY units — they are handed to inputProps,
    // which renders against `shown` — so range-check before converting.
    // inputProps min/max is only an HTML hint; browsers happily accept a typed
    // value outside it, so this is the check that actually holds.
    if ((min !== undefined && parsed < min) || (max !== undefined && parsed > max)) {
      enqueueSnackbar(
        t('jobExecution.errors.outOfRange', { label, min, max }) as string,
        { variant: 'warning' }
      );
      return;
    }

    const next = String(toStored ? toStored(parsed) : parsed);
    const previous = values[settingKey] ?? '';
    if (next === previous) return;

    setValues((v) => ({ ...v, [settingKey]: next }));
    saveOne(settingKey, next, previous);
  };

  return (
    <TextField
      fullWidth
      type="number"
      label={label}
      value={draft ?? String(shown)}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
      }}
      disabled={loading || savingKey === settingKey}
      helperText={helper}
      InputProps={{
        inputProps: { min, max },
        endAdornment: unit ? <InputAdornment position="end">{unit}</InputAdornment> : undefined,
      }}
    />
  );
};

/** Toggle bound to one setting key; saves immediately. */
export const SwitchSetting: React.FC<{ settingKey: string; label: string; helper?: string }> = ({
  settingKey,
  label,
  helper,
}) => {
  const { values, setValues, saveOne, loading, savingKey } = useSettingsCtx();
  return (
    <>
      <FormControlLabel
        control={
          <Switch
            checked={values[settingKey] === 'true'}
            onChange={(e) => {
              const previous = values[settingKey] ?? '';
              const next = String(e.target.checked);
              setValues((v) => ({ ...v, [settingKey]: next }));
              saveOne(settingKey, next, previous);
            }}
            disabled={loading || savingKey === settingKey}
          />
        }
        label={label}
      />
      {helper && (
        <Typography variant="caption" color="textSecondary" display="block">
          {helper}
        </Typography>
      )}
    </>
  );
};

/** Select bound to one setting key; saves immediately. */
export const SelectSetting: React.FC<{
  settingKey: string;
  label: string;
  helper: string;
  options: { value: string; label: string }[];
}> = ({ settingKey, label, helper, options }) => {
  const { values, setValues, saveOne, loading, savingKey } = useSettingsCtx();
  return (
    <TextField
      select
      fullWidth
      label={label}
      value={values[settingKey] ?? ''}
      onChange={(e) => {
        const previous = values[settingKey] ?? '';
        setValues((v) => ({ ...v, [settingKey]: e.target.value }));
        saveOne(settingKey, e.target.value, previous);
      }}
      disabled={loading || savingKey === settingKey}
      helperText={helper}
    >
      {options.map((o) => (
        <MenuItem key={o.value} value={o.value}>
          {o.label}
        </MenuItem>
      ))}
    </TextField>
  );
};

/** Titled, divided section wrapping a grid of fields. */
export const Panel: React.FC<{ title: string; children: React.ReactNode; caption?: string }> = ({
  title,
  caption,
  children,
}) => (
  <Grid item xs={12}>
    <Paper sx={{ p: 3 }}>
      <Typography variant="subtitle1" gutterBottom fontWeight="bold">
        {title}
      </Typography>
      {caption && (
        <Typography variant="body2" color="textSecondary" sx={{ mb: 1 }}>
          {caption}
        </Typography>
      )}
      <Divider sx={{ mb: 2 }} />
      <Grid container spacing={2}>
        {children}
      </Grid>
    </Paper>
  </Grid>
);

/** Text field bound to one setting key; saves on blur (and on Enter). */
export const TextSetting: React.FC<{
  settingKey: string;
  label: string;
  helper: string;
  placeholder?: string;
  type?: string;
}> = ({ settingKey, label, helper, placeholder, type }) => {
  const { values, setValues, saveOne, loading, savingKey } = useSettingsCtx();
  const [draft, setDraft] = useState<string | null>(null);
  const stored = values[settingKey] ?? '';

  const commit = () => {
    if (draft === null) return;
    const next = draft;
    setDraft(null);
    if (next === stored) return;
    setValues((v) => ({ ...v, [settingKey]: next }));
    saveOne(settingKey, next, stored);
  };

  return (
    <TextField
      fullWidth
      type={type}
      label={label}
      placeholder={placeholder}
      value={draft ?? stored}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
      }}
      disabled={loading || savingKey === settingKey}
      helperText={helper}
    />
  );
};

/** Centred spinner placeholder, so pages share one loading treatment. */
export const SettingsLoading: React.FC<{ minHeight?: number | string }> = ({ minHeight = 200 }) => (
  <Box display="flex" justifyContent="center" alignItems="center" minHeight={minHeight}>
    <Typography variant="body2" color="textSecondary">
      Loading…
    </Typography>
  </Box>
);
