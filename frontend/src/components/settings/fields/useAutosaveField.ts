import { useCallback, useState } from 'react';
import { FieldSaveStatus, useSettingsCtx } from './context';

export interface AutosaveFieldOptions {
  settingKey: string;
  /** Client-side check on the string about to be stored; return a message to reject. */
  validate?: (next: string) => string | null;
  /** Transform the draft into the stored string (e.g. unit conversion, trimming). */
  toStored?: (draft: string) => string | null;
}

export interface AutosaveField {
  /** Server value for this key. */
  stored: string;
  /** In-progress edit, or null when the field shows the server value. */
  draft: string | null;
  setDraft: (next: string | null) => void;
  /** Save the draft (blur / Enter). No-op when nothing changed. */
  commit: () => void;
  /** Save a value immediately (switches, selects, sliders on release). */
  commitValue: (next: string) => void;
  status: FieldSaveStatus;
  /** Validation message from the last rejected commit. Cleared on the next edit. */
  validationError: string | null;
  disabled: boolean;
}

/**
 * Binds one settings key to draft/commit semantics against the SettingsCtx.
 *
 * Every primitive in this folder is built on it; custom widgets (colour
 * pickers, money inputs) use it directly.
 */
export const useAutosaveField = ({ settingKey, validate, toStored }: AutosaveFieldOptions): AutosaveField => {
  const { values, setValues, saveOne, loading, saveStates, enabled } = useSettingsCtx();
  const [draft, setDraftState] = useState<string | null>(null);
  const [validationError, setValidationError] = useState<string | null>(null);
  const stored = values[settingKey] ?? '';
  const status = saveStates[settingKey] ?? { state: 'idle' };

  const setDraft = useCallback((next: string | null) => {
    setValidationError(null);
    setDraftState(next);
  }, []);

  const persist = useCallback(
    (text: string) => {
      const next = toStored ? toStored(text) : text;
      if (next === null) {
        setDraftState(null);
        return;
      }
      if (validate) {
        const message = validate(next);
        if (message) {
          setValidationError(message);
          setDraftState(null);
          return;
        }
      }
      setDraftState(null);
      const previous = values[settingKey] ?? '';
      if (next === previous) return;
      setValues((v) => ({ ...v, [settingKey]: next }));
      void saveOne(settingKey, next, previous);
    },
    [saveOne, setValues, settingKey, toStored, validate, values]
  );

  const commit = useCallback(() => {
    if (draft === null) return;
    persist(draft);
  }, [draft, persist]);

  const commitValue = useCallback((next: string) => persist(next), [persist]);

  return {
    stored,
    draft,
    setDraft,
    commit,
    commitValue,
    status,
    validationError,
    disabled: loading || status.state === 'saving' || enabled === false,
  };
};

export default useAutosaveField;
