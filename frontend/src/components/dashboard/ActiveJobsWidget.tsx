import React, { useEffect, useState } from 'react';
import { Button } from '@mui/material';
import { keepPreviousData, useQueryClient } from '@tanstack/react-query';
import { Link as RouterLink } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { SectionCard, readTablePrefs } from '../ui';
import JobsDataTable from '../jobs/JobsDataTable';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { qk } from '../../services/queryKeys';
import { api } from '../../services/api';
import { ROUTES } from '../../constants/routes';
import type { JobSummary, PaginationInfo } from '../../types/jobs';
import { viewParams, DashboardView } from '../../services/dashboard';

const TABLE_KEY = 'dashboard.jobs';
const PAGE_SIZES = [5, 10];

/**
 * Jobs in the selected view: active work first (scheduler order), then the most
 * recently finished, five per page by default. Mine = jobs I created (/api/user/jobs).
 */
const ActiveJobsWidget: React.FC<{ view: DashboardView; teamId?: string | null }> = ({ view, teamId }) => {
  const { t } = useTranslation('dashboard');
  const queryClient = useQueryClient();
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(() => {
    const saved = readTablePrefs(TABLE_KEY).pageSize;
    return saved && PAGE_SIZES.includes(saved) ? saved : PAGE_SIZES[0];
  });
  // A different view or team is a different list: start from its first page.
  useEffect(() => setPage(0), [view, teamId]);
  const params: Record<string, string> = { ...viewParams(view, teamId), page: String(page + 1), page_size: String(pageSize) };
  const url = view === 'mine' ? '/api/user/jobs' : '/api/jobs';
  const q = useLiveQuery<{ jobs: JobSummary[]; total: number }>(
    {
      queryKey: qk.dashboard.jobs(view, params),
      // /api/jobs nests the count under `pagination`; /api/user/jobs returns it top-level.
      queryFn: async () => {
        const data = (await api.get<{ jobs?: JobSummary[]; pagination?: PaginationInfo; total?: number }>(url, { params })).data;
        return { jobs: data.jobs ?? [], total: data.pagination?.total ?? data.total ?? 0 };
      },
      placeholderData: keepPreviousData,
    },
    { tier: 'fast' }
  );
  const total = q.data?.total ?? 0;

  return (
    <SectionCard
      title={t('activeJobs.recentTitle', 'Jobs') as string}
      subtitle={t('activeJobs.recentSubtitle', 'Active first, then recently finished') as string}
      actions={
        <Button size="small" component={RouterLink} to={ROUTES.jobs}>
          {t('activeJobs.viewAll', 'All jobs') as string}
        </Button>
      }
      flush
    >
      <JobsDataTable
        jobs={q.data?.jobs ?? []}
        loading={q.isLoading}
        fetching={q.isFetching && !q.isLoading}
        error={q.error}
        onRetry={() => void q.refetch()}
        onChanged={() => void queryClient.invalidateQueries({ queryKey: qk.dashboard.all })}
        compact
        flat
        tableKey={TABLE_KEY}
        pagination={{
          mode: 'server',
          page,
          pageSize,
          rowCount: total,
          pageSizeOptions: PAGE_SIZES,
          onChange: (m) => {
            if (m.pageSize !== pageSize) {
              setPageSize(m.pageSize);
              setPage(0);
            } else setPage(m.page);
          },
        }}
        emptyState={{ title: t('activeJobs.recentEmpty', 'No jobs yet') as string, description: t('activeJobs.emptyHint', 'Start a job from a hashlist.') as string, action: { label: t('activeJobs.goHashlists', 'Go to hashlists') as string, to: ROUTES.hashlists } }}
      />
    </SectionCard>
  );
};

export default ActiveJobsWidget;
