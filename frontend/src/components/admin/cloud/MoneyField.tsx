import React, { useState } from 'react';
import { Box, TextField } from '@mui/material';

/**
 * A dollars input over a cents value.
 *
 * WHY THIS EXISTS RATHER THAN A TextField PER CALL SITE.
 *
 * The obvious spelling is `value={centsToDollars(cents)}` with an onChange that
 * parses back to cents. It is unusable. Every keystroke round-trips
 * string -> cents -> toFixed(2) -> string, so typing "1" is immediately
 * rewritten to "1.00" with the caret left at the end of the formatted text. The
 * operator can then only edit the last decimal place, and there is no way to
 * type "10" at all. Both cloud money fields shipped with exactly that bug.
 *
 * The fix is to keep the RAW typed string while the field is being edited and
 * only format when it is committed. The draft is the source of truth during
 * editing; the cents value is the source of truth at rest.
 *
 * onChange still fires while typing so the surrounding form stays live and a
 * Save button can enable — it is only the *rendered* value that stops being
 * derived from the parsed number.
 */
export interface MoneyFieldProps {
  label: string;
  helperText?: string;
  /** Current value in cents. null means unset. */
  cents: number | null;
  /** Called with the parsed value. null when the field is cleared. */
  onChange: (cents: number | null) => void;
  disabled?: boolean;
  placeholder?: string;
  fullWidth?: boolean;
  /** Rendered when the field is empty and the value is inherited. */
  inheritedHint?: string;
}

const format = (cents: number | null): string =>
  cents === null || cents === undefined ? '' : (cents / 100).toFixed(2);

/**
 * Parse dollars to cents.
 *
 * Rounds rather than truncates: at cent precision `Math.trunc(10.07 * 100)` is
 * 1006, because 10.07 has no exact binary representation. A cap silently one
 * cent under what was typed is a small error that is very hard to believe.
 */
const parse = (raw: string): number | null => {
  const trimmed = raw.trim();
  if (trimmed === '') return null;
  const value = Number(trimmed);
  if (isNaN(value) || value < 0) return null;
  return Math.round(value * 100);
};

const MoneyField: React.FC<MoneyFieldProps> = ({
  label,
  helperText,
  cents,
  onChange,
  disabled,
  placeholder,
  fullWidth = true,
  inheritedHint,
}) => {
  // undefined means "not being edited"; the formatted value is shown instead.
  const [draft, setDraft] = useState<string | undefined>(undefined);

  return (
    <TextField
      fullWidth={fullWidth}
      label={label}
      value={draft !== undefined ? draft : format(cents)}
      placeholder={placeholder}
      disabled={disabled}
      onChange={(e) => {
        setDraft(e.target.value);
        onChange(parse(e.target.value));
      }}
      onBlur={() => setDraft(undefined)}
      helperText={
        helperText ??
        (inheritedHint && cents === null ? inheritedHint : undefined)
      }
      InputProps={{ startAdornment: <Box sx={{ mr: 1 }}>$</Box> }}
      inputProps={{ inputMode: 'decimal' }}
      InputLabelProps={placeholder ? { shrink: true } : undefined}
    />
  );
};

export default MoneyField;
