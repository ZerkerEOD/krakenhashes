import { useCallback, useEffect, useRef, useState } from 'react';
import { FieldSaveStatus, SAVED_DECAY_MS } from './context';

/**
 * Per-key save state with the "saved" tick decaying back to idle. Shared by
 * both context providers so the adornment behaves identically everywhere.
 */
export const useSaveStates = () => {
  const [saveStates, setSaveStates] = useState<Record<string, FieldSaveStatus>>({});
  const timers = useRef<Record<string, number>>({});

  useEffect(() => {
    const current = timers.current;
    return () => {
      Object.values(current).forEach((id) => window.clearTimeout(id));
    };
  }, []);

  const setState = useCallback((key: string, status: FieldSaveStatus) => {
    setSaveStates((s) => ({ ...s, [key]: status }));
    if (timers.current[key]) {
      window.clearTimeout(timers.current[key]);
      delete timers.current[key];
    }
    if (status.state === 'saved') {
      timers.current[key] = window.setTimeout(() => {
        setSaveStates((s) => (s[key]?.state === 'saved' ? { ...s, [key]: { state: 'idle' } } : s));
        delete timers.current[key];
      }, SAVED_DECAY_MS);
    }
  }, []);

  const savingKey = Object.entries(saveStates).find(([, v]) => v.state === 'saving')?.[0] ?? null;

  return { saveStates, setState, savingKey };
};

export default useSaveStates;
