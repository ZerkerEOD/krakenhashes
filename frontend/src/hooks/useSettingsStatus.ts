import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { qk } from '../services/queryKeys';
import { Attention, StatusKey, deriveAttention, getSettingsStatus, worst } from '../services/settingsStatus';
import { useAuth } from '../contexts/AuthContext';

/**
 * One shared query for the settings hub, the settings rail badges and the
 * sidebar badge. Admin-only; returns an empty result for other roles.
 */
export const useSettingsStatus = () => {
  const { userRole } = useAuth();
  const query = useQuery({
    queryKey: qk.admin.settingsStatus(),
    queryFn: getSettingsStatus,
    enabled: userRole === 'admin',
    staleTime: 30_000,
    refetchInterval: 60_000,
  });

  const attention = useMemo(() => (query.data ? deriveAttention(query.data) : undefined), [query.data]);

  const attentionFor = (keys: StatusKey[] | undefined): Attention | undefined => {
    if (!attention || !keys || keys.length === 0) return undefined;
    const items = keys.map((k) => attention[k]);
    const severity = worst(items);
    if (severity === 'ok') return undefined;
    return items.find((a) => a.severity === severity);
  };

  const attentionCount = attention ? Object.values(attention).filter((a) => a.severity !== 'ok').length : 0;

  return { ...query, status: query.data, attention, attentionFor, attentionCount };
};

export default useSettingsStatus;
