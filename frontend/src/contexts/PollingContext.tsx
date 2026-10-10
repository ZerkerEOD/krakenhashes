/**
 * Session-wide "auto refresh" switch shared by every `useLiveQuery` consumer
 * (Dashboard, Jobs, detail pages). Off = queries still load, but stop polling.
 */
import React, { createContext, useContext, useMemo, useState } from 'react';

interface PollingContextValue {
  enabled: boolean;
  setEnabled: (next: boolean) => void;
}

const PollingContext = createContext<PollingContextValue | undefined>(undefined);

export const PollingProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [enabled, setEnabled] = useState(true);
  const value = useMemo(() => ({ enabled, setEnabled }), [enabled]);
  return <PollingContext.Provider value={value}>{children}</PollingContext.Provider>;
};

export const usePolling = (): PollingContextValue => {
  const ctx = useContext(PollingContext);
  return ctx ?? { enabled: true, setEnabled: () => undefined };
};

export default PollingContext;
