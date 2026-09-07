import React, { useState } from 'react';
import { InputAdornment, TextField } from '@mui/material';

/**
 * A number input whose displayed unit differs from its stored unit — minutes
 * over seconds, for instance.
 *
 * WHY THIS EXISTS.
 *
 * The natural spelling is `value={toDisplay(stored)}` with an onChange that
 * converts back. It misbehaves in two ways, both because the rendered value is
 * re-derived from the parsed number on every keystroke:
 *
 *   - Clearing the field parses to NaN, falls back to 0, and the box
 *     immediately refills with "0". The field cannot be emptied, and the next
 *     character typed lands after that zero.
 *   - Any conversion that is not perfectly round-tripping (dollars over cents
 *     being the worst case) rewrites the text under the caret mid-entry.
 *
 * Holding the raw typed string while the field is focused and converting only
 * on commit fixes both. The draft is the source of truth while editing; the
 * stored number is the source of truth at rest.
 */
export interface ScaledNumberFieldProps {
  label: string;
  helperText?: string;
  /** Stored value, in the unit the API uses. */
  value: number | null | undefined;
  /** Called with the stored-unit value. null when the field is cleared. */
  onChange: (value: number | null) => void;
  /** Stored unit -> displayed unit. Identity when omitted. */
  toDisplay?: (stored: number) => number;
  /** Displayed unit -> stored unit. Identity when omitted. */
  toStored?: (shown: number) => number;
  unit?: string;
  min?: number;
  disabled?: boolean;
  placeholder?: string;
  fullWidth?: boolean;
}

const ScaledNumberField: React.FC<ScaledNumberFieldProps> = ({
  label,
  helperText,
  value,
  onChange,
  toDisplay,
  toStored,
  unit,
  min = 0,
  disabled,
  placeholder,
  fullWidth = true,
}) => {
  const [draft, setDraft] = useState<string | undefined>(undefined);

  const displayed = (): string => {
    if (draft !== undefined) return draft;
    if (value === null || value === undefined) return '';
    return String(toDisplay ? toDisplay(value) : value);
  };

  const commit = (raw: string) => {
    setDraft(undefined);
    const trimmed = raw.trim();
    if (trimmed === '') {
      onChange(null);
      return;
    }
    const parsed = Number(trimmed);
    if (isNaN(parsed)) return;
    onChange(toStored ? toStored(parsed) : parsed);
  };

  return (
    <TextField
      fullWidth={fullWidth}
      type="number"
      label={label}
      value={displayed()}
      placeholder={placeholder}
      disabled={disabled}
      onChange={(e) => {
        setDraft(e.target.value);
        // Kept live so a surrounding form's dirty state tracks typing; only the
        // rendered text is decoupled.
        const parsed = Number(e.target.value);
        if (e.target.value.trim() === '') onChange(null);
        else if (!isNaN(parsed)) onChange(toStored ? toStored(parsed) : parsed);
      }}
      onBlur={(e) => commit(e.target.value)}
      helperText={helperText}
      InputProps={{
        endAdornment: unit ? <InputAdornment position="end">{unit}</InputAdornment> : undefined,
      }}
      inputProps={{ min }}
      InputLabelProps={placeholder ? { shrink: true } : undefined}
    />
  );
};

export default ScaledNumberField;
