import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemSettings, updateSystemSetting } from '../../../services/systemSettings';
import { getErrorMessage } from '../../../utils/errors';
import { useToast } from '../../ui/toast';
import { SettingsCtxValue, SettingsMap } from './context';
import { useSaveStates } from './useSaveStates';

/**
 * Loads every system setting and exposes the per-key save used by the fields.
 *
 * Each save writes ONLY its key through `PUT /admin/settings/{key}`. A failed
 * save reverts that one field (the rest of the page is still server-accurate
 * because nothing else was sent) and surfaces the server's message both on
 * the field and as a toast.
 */
export const useSystemSettingsForm = (): SettingsCtxValue & {
  error: string | null;
  clearError: () => void;
  reload: () => Promise<void>;
} => {
  const { t } = useTranslation('admin');
  const toast = useToast();
  const [values, setValues] = useState<SettingsMap>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const { saveStates, setState, savingKey } = useSaveStates();

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
    } catch (err) {
      const message = getErrorMessage(err, t('jobExecution.errors.loadFailed') as string);
      setError(message);
      toast.error(message);
    } finally {
      setLoading(false);
    }
  }, [t, toast]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const saveOne = useCallback(
    async (key: string, value: string, previous: string) => {
      setState(key, { state: 'saving' });
      try {
        await updateSystemSetting(key, value);
        setState(key, { state: 'saved' });
      } catch (err) {
        const message = getErrorMessage(err, t('jobExecution.messages.saveFailed') as string);
        // Revert just this field — nothing else was sent.
        setValues((v) => ({ ...v, [key]: previous }));
        setState(key, { state: 'error', error: message });
        toast.error(message);
      }
    },
    [setState, t, toast]
  );

  const clearError = useCallback(() => setError(null), []);

  return { values, setValues, saveOne, loading, saveStates, savingKey, error, clearError, reload };
};

export default useSystemSettingsForm;
