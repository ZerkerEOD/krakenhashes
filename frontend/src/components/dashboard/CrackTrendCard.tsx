import React, { useMemo } from 'react';
import { Box, List, ListItem, Typography } from '@mui/material';
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { format, parseISO } from 'date-fns';
import { useTranslation } from 'react-i18next';
import { EntityLink, ErrorState, SectionCard } from '../ui';
import { ChartTooltipBox, useChartColors } from '../ui/charts';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { useDateLocale } from '../../hooks/useDateLocale';
import { qk } from '../../services/queryKeys';
import { getCrackTrend, DashboardView } from '../../services/dashboard';

const DAYS = 14;

/** Cracks per day for the last two weeks, plus this week's most productive hashlists. */
const CrackTrendCard: React.FC<{ view: DashboardView; teamId?: string | null }> = ({ view, teamId }) => {
  const { t } = useTranslation('dashboard');
  const colors = useChartColors();
  const locale = useDateLocale();
  const q = useLiveQuery(
    { queryKey: qk.dashboard.crackTrend(view, teamId, DAYS), queryFn: () => getCrackTrend(view, teamId, DAYS) },
    { tier: 'slow' }
  );

  const data = useMemo(
    () =>
      (q.data?.days ?? []).map((d) => {
        const date = parseISO(d.date);
        return { ...d, label: format(date, 'd MMM', { locale }), full: format(date, 'PPP', { locale }) };
      }),
    [q.data, locale]
  );
  const total = data.reduce((n, d) => n + d.count, 0);
  const top = q.data?.top_hashlists ?? [];

  return (
    <SectionCard
      title={t('crackTrend.title', 'Cracks over time') as string}
      subtitle={t('crackTrend.subtitle', { count: total, days: DAYS }) as string}
      loading={q.isLoading}
      fill
    >
      {q.error && !q.data ? (
        <ErrorState error={q.error} onRetry={() => void q.refetch()} compact />
      ) : (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minHeight: 0 }}>
          <Box sx={{ height: 160 }}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: -16 }}>
                <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} vertical={false} />
                <XAxis dataKey="label" tick={{ fill: colors.axis, fontSize: 11 }} interval="preserveStartEnd" tickLine={false} />
                <YAxis allowDecimals={false} tick={{ fill: colors.axis, fontSize: 11 }} tickLine={false} axisLine={false} />
                <Tooltip
                  cursor={{ fill: colors.grid, opacity: 0.3 }}
                  content={({ active, payload }) =>
                    active && payload?.length ? (
                      <ChartTooltipBox>
                        <Typography variant="caption" color="text.secondary" component="div">
                          {payload[0].payload.full}
                        </Typography>
                        <Typography variant="body2">
                          {t('crackTrend.tooltip', { count: payload[0].payload.count }) as string}
                        </Typography>
                      </ChartTooltipBox>
                    ) : null
                  }
                />
                <Bar dataKey="count" fill={colors.primary} radius={[3, 3, 0, 0]} maxBarSize={28} />
              </BarChart>
            </ResponsiveContainer>
          </Box>
          <Box>
            <Typography variant="overline" color="text.secondary">
              {t('crackTrend.topHashlists', 'Top hashlists this week') as string}
            </Typography>
            {top.length === 0 ? (
              <Typography variant="body2" color="text.secondary">
                {t('crackTrend.noCracksWeek', 'No cracks in the last 7 days') as string}
              </Typography>
            ) : (
              <List dense disablePadding>
                {top.map((h) => (
                  <ListItem key={h.id} disableGutters sx={{ py: 0.25, gap: 1 }}>
                    <Box sx={{ minWidth: 0, flexGrow: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      <EntityLink type="hashlist" id={h.id} label={h.name} />
                    </Box>
                    <EntityLink type="pot_hashlist" id={h.id} label={h.count.toLocaleString()} sx={{ fontWeight: 500 }} />
                  </ListItem>
                ))}
              </List>
            )}
          </Box>
        </Box>
      )}
    </SectionCard>
  );
};

export default CrackTrendCard;
