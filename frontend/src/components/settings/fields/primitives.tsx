/**
 * Settings field primitives. Each binds to ONE key through SettingsCtx and
 * saves only itself (see context.ts). They are declared at module scope so
 * their identity is stable across renders and the caret is never lost.
 */
import React, { useState } from 'react';
import {
  Box,
  Checkbox,
  FormControlLabel,
  FormGroup,
  FormHelperText,
  Grid,
  InputAdornment,
  MenuItem,
  Slider,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import SectionCard from '../../ui/SectionCard';
import { isHexColor } from '../../../styles/palette';
import FieldSaveAdornment from './FieldSaveAdornment';
import { useAutosaveField } from './useAutosaveField';
import { SettingsCtx, useSettingsCtx } from './context';

const blurOnEnter = (e: React.KeyboardEvent) => {
  if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
};

// ---------------------------------------------------------------------------

export interface NumberSettingProps {
  settingKey: string;
  label: string;
  helper?: string;
  min?: number;
  max?: number;
  step?: number;
  unit?: string;
  /** Displayed unit differs from the stored unit (e.g. stored seconds, shown minutes). */
  toDisplay?: (stored: number) => number;
  toStored?: (shown: number) => number;
  /** Allow decimals (default integers only). */
  decimal?: boolean;
  fullWidth?: boolean;
  disabled?: boolean;
  size?: 'small' | 'medium';
}

/** Number field; saves on blur or Enter. Range is checked here, not just by the browser. */
export const NumberSetting: React.FC<NumberSettingProps> = ({
  settingKey,
  label,
  helper,
  min,
  max,
  step,
  unit,
  toDisplay,
  toStored,
  decimal,
  fullWidth = true,
  disabled: disabledProp,
  size,
}) => {
  const { t } = useTranslation('admin');
  const field = useAutosaveField({
    settingKey,
    toStored: (text) => {
      const parsed = decimal ? parseFloat(text) : parseInt(text, 10);
      if (isNaN(parsed)) return null;
      return String(toStored ? toStored(parsed) : parsed);
    },
    validate: (next) => {
      const storedNum = decimal ? parseFloat(next) : parseInt(next, 10);
      const shown = toDisplay ? toDisplay(storedNum) : storedNum;
      if ((min !== undefined && shown < min) || (max !== undefined && shown > max)) {
        return t('jobExecution.errors.outOfRange', { label, min, max }) as string;
      }
      return null;
    },
  });
  const storedNum = decimal ? parseFloat(field.stored) : parseInt(field.stored, 10);
  const shown = isNaN(storedNum) ? '' : String(toDisplay ? toDisplay(storedNum) : storedNum);
  const hasError = field.validationError !== null || field.status.state === 'error';

  return (
    <TextField
      fullWidth={fullWidth}
      size={size}
      type="number"
      label={label}
      value={field.draft ?? shown}
      onChange={(e) => field.setDraft(e.target.value)}
      onBlur={field.commit}
      onKeyDown={blurOnEnter}
      disabled={field.disabled || disabledProp}
      error={hasError}
      helperText={field.validationError ?? field.status.error ?? helper}
      InputProps={{
        inputProps: { min, max, step },
        endAdornment: (
          <>
            {unit && <InputAdornment position="end">{unit}</InputAdornment>}
            <FieldSaveAdornment status={field.status} />
          </>
        ),
      }}
    />
  );
};

// ---------------------------------------------------------------------------

export interface TextSettingProps {
  settingKey: string;
  label: string;
  helper?: string;
  placeholder?: string;
  type?: string;
  multiline?: boolean;
  rows?: number;
  /** Trim before saving (default true). */
  trim?: boolean;
  /** Reject the value with a message. */
  validate?: (next: string) => string | null;
  /** Write-only secret: the stored value is never shown; an empty draft is not saved. */
  secret?: boolean;
  fullWidth?: boolean;
  disabled?: boolean;
  maxLength?: number;
  size?: 'small' | 'medium';
  /** Force the label into its shrunk position (native time/date inputs). */
  shrinkLabel?: boolean;
}

/** Text field; saves on blur or Enter (Enter is ignored for multiline). */
export const TextSetting: React.FC<TextSettingProps> = ({
  settingKey,
  label,
  helper,
  placeholder,
  type,
  multiline,
  rows,
  trim = true,
  validate,
  secret,
  fullWidth = true,
  disabled: disabledProp,
  maxLength,
  size,
  shrinkLabel,
}) => {
  const field = useAutosaveField({
    settingKey,
    toStored: (text) => {
      const v = trim ? text.trim() : text;
      if (secret && v === '') return null;
      return v;
    },
    validate,
  });
  const hasError = field.validationError !== null || field.status.state === 'error';
  return (
    <TextField
      fullWidth={fullWidth}
      size={size}
      type={secret ? 'password' : type}
      label={label}
      placeholder={placeholder}
      multiline={multiline}
      rows={rows}
      value={field.draft ?? (secret ? '' : field.stored)}
      onChange={(e) => field.setDraft(e.target.value)}
      onBlur={field.commit}
      onKeyDown={multiline ? undefined : blurOnEnter}
      disabled={field.disabled || disabledProp}
      error={hasError}
      helperText={field.validationError ?? field.status.error ?? helper}
      inputProps={{ maxLength, autoComplete: secret ? 'new-password' : undefined }}
      InputProps={{ endAdornment: <FieldSaveAdornment status={field.status} /> }}
      InputLabelProps={shrinkLabel ? { shrink: true } : undefined}
    />
  );
};

// ---------------------------------------------------------------------------

export interface SwitchSettingProps {
  settingKey: string;
  label: string;
  helper?: string;
  disabled?: boolean;
  /** Stored representation of on/off (default "true"/"false"). */
  onValue?: string;
  offValue?: string;
}

/** Toggle; saves immediately. */
export const SwitchSetting: React.FC<SwitchSettingProps> = ({
  settingKey,
  label,
  helper,
  disabled: disabledProp,
  onValue = 'true',
  offValue = 'false',
}) => {
  const field = useAutosaveField({ settingKey });
  const checked = field.stored === onValue;
  return (
    <Box>
      <FormControlLabel
        control={
          <Switch
            checked={checked}
            onChange={(e) => field.commitValue(e.target.checked ? onValue : offValue)}
            disabled={field.disabled || disabledProp}
          />
        }
        label={
          <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center' }}>
            {label}
            <FieldSaveAdornment status={field.status} inline />
          </Box>
        }
      />
      {(helper || field.status.error) && (
        <FormHelperText error={field.status.state === 'error'} sx={{ ml: 1.75, mt: -0.5 }}>
          {field.status.error ?? helper}
        </FormHelperText>
      )}
    </Box>
  );
};

// ---------------------------------------------------------------------------

export interface SelectSettingProps {
  settingKey: string;
  label: string;
  helper?: string;
  options: { value: string; label: string }[];
  /** Label for an explicit empty option. */
  noneLabel?: string;
  fullWidth?: boolean;
  disabled?: boolean;
}

/** Select; saves immediately. */
export const SelectSetting: React.FC<SelectSettingProps> = ({
  settingKey,
  label,
  helper,
  options,
  noneLabel,
  fullWidth = true,
  disabled: disabledProp,
}) => {
  const field = useAutosaveField({ settingKey });
  const known = options.some((o) => o.value === field.stored);
  return (
    <TextField
      select
      fullWidth={fullWidth}
      label={label}
      value={known ? field.stored : ''}
      onChange={(e) => field.commitValue(e.target.value)}
      disabled={field.disabled || disabledProp}
      error={field.status.state === 'error'}
      helperText={field.status.error ?? helper}
      InputProps={{ endAdornment: <FieldSaveAdornment status={field.status} /> }}
      SelectProps={{ sx: { '& .MuiSelect-icon': { right: 36 } } }}
    >
      {noneLabel !== undefined && (
        <MenuItem value="">
          <em>{noneLabel}</em>
        </MenuItem>
      )}
      {options.map((o) => (
        <MenuItem key={o.value} value={o.value}>
          {o.label}
        </MenuItem>
      ))}
    </TextField>
  );
};

// ---------------------------------------------------------------------------

export interface SliderSettingProps {
  settingKey: string;
  label: string;
  helper?: string;
  min: number;
  max: number;
  step?: number;
  marks?: boolean | { value: number; label: string }[];
  unit?: string;
  disabled?: boolean;
}

/** Slider with a companion number input; saves when the slider is released or the input blurs. */
export const SliderSetting: React.FC<SliderSettingProps> = ({
  settingKey,
  label,
  helper,
  min,
  max,
  step = 1,
  marks = true,
  unit,
  disabled: disabledProp,
}) => {
  const { t } = useTranslation('admin');
  const field = useAutosaveField({
    settingKey,
    toStored: (text) => {
      const n = parseInt(text, 10);
      return isNaN(n) ? null : String(n);
    },
    validate: (next) => {
      const n = parseInt(next, 10);
      return n < min || n > max ? (t('jobExecution.errors.outOfRange', { label, min, max }) as string) : null;
    },
  });
  const storedNum = parseInt(field.stored, 10);
  const [sliding, setSliding] = useState<number | null>(null);
  const value = sliding ?? (field.draft !== null ? parseInt(field.draft, 10) : storedNum);
  const disabled = field.disabled || disabledProp;

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography variant="body2" sx={{ flexGrow: 1 }}>
          {label}
        </Typography>
        <FieldSaveAdornment status={field.status} inline />
      </Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 3, mt: 1 }}>
        <Slider
          value={isNaN(value) ? min : value}
          min={min}
          max={max}
          step={step}
          marks={marks}
          valueLabelDisplay="auto"
          disabled={disabled}
          onChange={(_e, v) => setSliding(v as number)}
          onChangeCommitted={(_e, v) => {
            setSliding(null);
            field.commitValue(String(v as number));
          }}
          sx={{ flexGrow: 1, ml: 1 }}
        />
        <TextField
          type="number"
          size="small"
          value={field.draft ?? (isNaN(storedNum) ? '' : String(storedNum))}
          onChange={(e) => field.setDraft(e.target.value)}
          onBlur={field.commit}
          onKeyDown={blurOnEnter}
          disabled={disabled}
          error={field.validationError !== null || field.status.state === 'error'}
          inputProps={{ min, max, step, style: { width: 64 } }}
          InputProps={{ endAdornment: unit ? <InputAdornment position="end">{unit}</InputAdornment> : undefined }}
        />
      </Box>
      {(field.validationError || field.status.error || helper) && (
        <FormHelperText error={field.validationError !== null || field.status.state === 'error'}>
          {field.validationError ?? field.status.error ?? helper}
        </FormHelperText>
      )}
    </Box>
  );
};

// ---------------------------------------------------------------------------

export interface CheckboxGroupSettingProps {
  /** Key whose stored value is a JSON array of strings. */
  settingKey: string;
  label?: string;
  helper?: string;
  options: { value: string; label: string; disabled?: boolean }[];
  /** Require at least one selection. */
  minSelected?: number;
  row?: boolean;
  disabled?: boolean;
}

/** A set of checkboxes stored as a JSON array; each toggle saves immediately. */
export const CheckboxGroupSetting: React.FC<CheckboxGroupSettingProps> = ({
  settingKey,
  label,
  helper,
  options,
  minSelected = 0,
  row,
  disabled: disabledProp,
}) => {
  const { t } = useTranslation('admin');
  const field = useAutosaveField({
    settingKey,
    validate: (next) => {
      try {
        const arr = JSON.parse(next) as string[];
        return arr.length < minSelected ? (t('fieldState.minSelected', { count: minSelected }) as string) : null;
      } catch {
        return null;
      }
    },
  });
  let selected: string[] = [];
  try {
    selected = field.stored ? (JSON.parse(field.stored) as string[]) : [];
  } catch {
    selected = [];
  }
  const toggle = (value: string, on: boolean) => {
    const next = on ? Array.from(new Set([...selected, value])) : selected.filter((v) => v !== value);
    field.commitValue(JSON.stringify(next));
  };
  return (
    <Box>
      {label && (
        <Typography variant="subtitle2" sx={{ display: 'flex', alignItems: 'center', mb: 0.5 }}>
          {label}
          <FieldSaveAdornment status={field.status} inline />
        </Typography>
      )}
      <FormGroup row={row}>
        {options.map((o) => (
          <FormControlLabel
            key={o.value}
            control={
              <Checkbox
                checked={selected.includes(o.value)}
                onChange={(e) => toggle(o.value, e.target.checked)}
                disabled={field.disabled || disabledProp || o.disabled}
              />
            }
            label={o.label}
          />
        ))}
      </FormGroup>
      {(field.validationError || field.status.error || helper) && (
        <FormHelperText error={field.validationError !== null || field.status.state === 'error'}>
          {field.validationError ?? field.status.error ?? helper}
        </FormHelperText>
      )}
    </Box>
  );
};

// ---------------------------------------------------------------------------

export interface ListTextSettingProps {
  /** Key whose stored value is a JSON array of strings; edited one per line. */
  settingKey: string;
  label: string;
  helper?: string;
  placeholder?: string;
  rows?: number;
  validate?: (items: string[]) => string | null;
  disabled?: boolean;
}

/** Multiline editor for a list of strings (one per line), stored as JSON. */
export const ListTextSetting: React.FC<ListTextSettingProps> = ({
  settingKey,
  label,
  helper,
  placeholder,
  rows = 3,
  validate,
  disabled: disabledProp,
}) => {
  const field = useAutosaveField({
    settingKey,
    toStored: (text) =>
      JSON.stringify(
        text
          .split(/\r?\n/)
          .map((s) => s.trim())
          .filter(Boolean)
      ),
    validate: validate
      ? (next) => {
          try {
            return validate(JSON.parse(next) as string[]);
          } catch {
            return null;
          }
        }
      : undefined,
  });
  let shown = '';
  try {
    shown = field.stored ? (JSON.parse(field.stored) as string[]).join('\n') : '';
  } catch {
    shown = field.stored;
  }
  const hasError = field.validationError !== null || field.status.state === 'error';
  return (
    <TextField
      fullWidth
      multiline
      rows={rows}
      label={label}
      placeholder={placeholder}
      value={field.draft ?? shown}
      onChange={(e) => field.setDraft(e.target.value)}
      onBlur={field.commit}
      disabled={field.disabled || disabledProp}
      error={hasError}
      helperText={field.validationError ?? field.status.error ?? helper}
      InputProps={{ endAdornment: <FieldSaveAdornment status={field.status} /> }}
    />
  );
};

// ---------------------------------------------------------------------------

export interface ColorSettingProps {
  settingKey: string;
  label: string;
  helper?: string;
  /** Shown in the swatch when the value is empty. */
  fallback: string;
  disabled?: boolean;
}

/** Hex colour with a native swatch; saves on blur, or when the swatch picker closes. */
export const ColorSetting: React.FC<ColorSettingProps> = ({ settingKey, label, helper, fallback, disabled: disabledProp }) => {
  const { t } = useTranslation('admin');
  const field = useAutosaveField({
    settingKey,
    toStored: (text) => text.trim().toLowerCase(),
    validate: (next) => (next !== '' && !isHexColor(next) ? (t('branding.errors.invalidHex') as string) : null),
  });
  const shown = field.draft ?? field.stored;
  const invalid = shown !== '' && !isHexColor(shown);
  return (
    <TextField
      fullWidth
      label={label}
      value={shown}
      placeholder={fallback}
      onChange={(e) => field.setDraft(e.target.value)}
      onBlur={field.commit}
      onKeyDown={blurOnEnter}
      disabled={field.disabled || disabledProp}
      error={invalid || field.validationError !== null || field.status.state === 'error'}
      helperText={field.validationError ?? field.status.error ?? helper}
      InputProps={{
        startAdornment: (
          <InputAdornment position="start">
            <Box
              component="input"
              type="color"
              aria-label={label}
              value={isHexColor(shown) ? shown : fallback}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => field.setDraft(e.target.value)}
              onBlur={field.commit}
              disabled={field.disabled || disabledProp}
              sx={{ width: 28, height: 28, p: 0, border: 1, borderColor: 'divider', borderRadius: 1, bgcolor: 'transparent', cursor: 'pointer' }}
            />
          </InputAdornment>
        ),
        endAdornment: <FieldSaveAdornment status={field.status} />,
      }}
    />
  );
};

// ---------------------------------------------------------------------------

export interface MoneySettingProps {
  /** Key stored in cents. */
  settingKey: string;
  label: string;
  helper?: string;
  min?: number;
  currency?: string;
  disabled?: boolean;
}

/** Dollars in the UI, cents in storage. */
export const MoneySetting: React.FC<MoneySettingProps> = ({ settingKey, label, helper, min = 0, currency = '$', disabled }) => (
  <NumberSetting
    settingKey={settingKey}
    label={label}
    helper={helper}
    min={min}
    step={0.01}
    decimal
    unit={currency}
    toDisplay={(cents) => Math.round(cents) / 100}
    toStored={(dollars) => Math.round(dollars * 100)}
    disabled={disabled}
  />
);

// ---------------------------------------------------------------------------

export interface PanelProps {
  title: string;
  caption?: string;
  children: React.ReactNode;
  /** Right-aligned header content. */
  actions?: React.ReactNode;
  /** Grid spacing between fields. */
  spacing?: number;
}

/** Titled section wrapping a grid of fields (a `<Grid item xs={12}>`). */
export const Panel: React.FC<PanelProps> = ({ title, caption, children, actions, spacing = 2 }) => (
  <Grid item xs={12}>
    <SectionCard title={title} subtitle={caption} actions={actions}>
      <Grid container spacing={spacing}>
        {children}
      </Grid>
    </SectionCard>
  </Grid>
);

/** Fields disabled while a dependent toggle is off. */
export const DependentFields: React.FC<{ enabled: boolean; children: React.ReactNode }> = ({ enabled, children }) => {
  const ctx = useSettingsCtx();
  return <SettingsCtx.Provider value={{ ...ctx, enabled }}>{children}</SettingsCtx.Provider>;
};
