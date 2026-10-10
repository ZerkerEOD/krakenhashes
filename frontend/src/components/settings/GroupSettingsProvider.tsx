import React from 'react';
import { SettingsCtx, SettingsLoading, useGroupSettings } from './fields';
import type { GroupSettingsOptions } from './fields';
import ErrorState from '../ui/ErrorState';

/**
 * Provides a SettingsCtx over a typed group endpoint (see useGroupSettings)
 * and renders its loading/error states. `render` receives the loaded object
 * for anything that needs the raw values (e.g. conditional UI).
 */
function GroupSettingsProvider<T extends Record<string, unknown>>({
  children,
  render,
  ...options
}: GroupSettingsOptions<T> & { children?: React.ReactNode; render?: (data: T) => React.ReactNode }) {
  const { data, error, reload, ...ctx } = useGroupSettings<T>(options);
  if (ctx.loading) return <SettingsLoading panels={1} />;
  if (error && !data) return <ErrorState error={error} onRetry={() => void reload()} compact />;
  return (
    <SettingsCtx.Provider value={ctx}>
      {render && data ? render(data) : null}
      {children}
    </SettingsCtx.Provider>
  );
}

export default GroupSettingsProvider;
