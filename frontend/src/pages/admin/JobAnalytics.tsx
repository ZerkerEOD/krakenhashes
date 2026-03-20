import React, { useState, useEffect, useCallback } from 'react';
import { Box, Typography, Tabs, Tab, Alert } from '@mui/material';
import JobAnalyticsFilters from '../../components/admin/job-analytics/JobAnalyticsFilters';
import JobAnalyticsSummaryCards from '../../components/admin/job-analytics/JobAnalyticsSummaryCards';
import JobHashRateChart from '../../components/admin/job-analytics/JobHashRateChart';
import JobExecutionTable from '../../components/admin/job-analytics/JobExecutionTable';
import BenchmarkTrendChart from '../../components/admin/job-analytics/BenchmarkTrendChart';
import BenchmarkHistoryTable from '../../components/admin/job-analytics/BenchmarkHistoryTable';
import {
  JobAnalyticsFilterOptions,
  JobAnalyticsSummary,
  JobAnalyticsEntry,
  TimelinePoint,
  JobAnalyticsFilterParams,
} from '../../types/jobAnalytics';
import { jobAnalyticsService } from '../../services/jobAnalytics';

const emptyFilter: JobAnalyticsFilterParams = {};

const JobAnalyticsPage: React.FC = () => {
  const [tab, setTab] = useState(0);
  const [filter, setFilter] = useState<JobAnalyticsFilterParams>(emptyFilter);
  const [appliedFilter, setAppliedFilter] = useState<JobAnalyticsFilterParams>(emptyFilter);

  // Filter options
  const [filterOptions, setFilterOptions] = useState<JobAnalyticsFilterOptions>();
  const [filterOptionsLoading, setFilterOptionsLoading] = useState(true);

  // Summary
  const [summary, setSummary] = useState<JobAnalyticsSummary>();
  const [summaryLoading, setSummaryLoading] = useState(true);

  // Timeline
  const [timeline, setTimeline] = useState<TimelinePoint[]>();
  const [timelineLoading, setTimelineLoading] = useState(true);
  const [resolution, setResolution] = useState('daily');

  // Jobs table
  const [jobs, setJobs] = useState<JobAnalyticsEntry[]>();
  const [jobsTotal, setJobsTotal] = useState(0);
  const [jobsPage, setJobsPage] = useState(1);
  const [jobsPageSize, setJobsPageSize] = useState(25);
  const [sortBy, setSortBy] = useState('started_at');
  const [sortOrder, setSortOrder] = useState('desc');
  const [jobsLoading, setJobsLoading] = useState(true);

  const [error, setError] = useState<string | null>(null);

  // Load filter options on mount
  useEffect(() => {
    jobAnalyticsService.getFilters()
      .then(opts => { setFilterOptions(opts); setFilterOptionsLoading(false); })
      .catch(err => { setError('Failed to load filter options'); setFilterOptionsLoading(false); });
  }, []);

  // Load summary
  const loadSummary = useCallback((f: JobAnalyticsFilterParams) => {
    setSummaryLoading(true);
    jobAnalyticsService.getSummary(f)
      .then(s => { setSummary(s); setSummaryLoading(false); })
      .catch(() => { setSummaryLoading(false); });
  }, []);

  // Load timeline
  const loadTimeline = useCallback((f: JobAnalyticsFilterParams, res: string) => {
    setTimelineLoading(true);
    jobAnalyticsService.getTimeline(f, res)
      .then(data => { setTimeline(data.points); setTimelineLoading(false); })
      .catch(() => { setTimelineLoading(false); });
  }, []);

  // Load jobs
  const loadJobs = useCallback((f: JobAnalyticsFilterParams, page: number, pageSize: number, sortBy: string, sortOrder: string) => {
    setJobsLoading(true);
    jobAnalyticsService.getJobs(f, page, pageSize, sortBy, sortOrder)
      .then(data => {
        setJobs(data.jobs || data.items || []);
        setJobsTotal(data.pagination?.total || 0);
        setJobsLoading(false);
      })
      .catch(() => { setJobsLoading(false); });
  }, []);

  // Initial load
  useEffect(() => {
    loadSummary(appliedFilter);
    loadTimeline(appliedFilter, resolution);
    loadJobs(appliedFilter, jobsPage, jobsPageSize, sortBy, sortOrder);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleApply = () => {
    setAppliedFilter(filter);
    setJobsPage(1);
    loadSummary(filter);
    loadTimeline(filter, resolution);
    loadJobs(filter, 1, jobsPageSize, sortBy, sortOrder);
  };

  const handleReset = () => {
    setFilter(emptyFilter);
    setAppliedFilter(emptyFilter);
    setJobsPage(1);
    loadSummary(emptyFilter);
    loadTimeline(emptyFilter, resolution);
    loadJobs(emptyFilter, 1, jobsPageSize, sortBy, sortOrder);
  };

  const handleResolutionChange = (newRes: string) => {
    setResolution(newRes);
    loadTimeline(appliedFilter, newRes);
  };

  const handlePageChange = (newPage: number) => {
    setJobsPage(newPage);
    loadJobs(appliedFilter, newPage, jobsPageSize, sortBy, sortOrder);
  };

  const handlePageSizeChange = (newSize: number) => {
    setJobsPageSize(newSize);
    setJobsPage(1);
    loadJobs(appliedFilter, 1, newSize, sortBy, sortOrder);
  };

  const handleSortChange = (newSortBy: string, newSortOrder: string) => {
    setSortBy(newSortBy);
    setSortOrder(newSortOrder);
    loadJobs(appliedFilter, jobsPage, jobsPageSize, newSortBy, newSortOrder);
  };

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h4" component="h1" gutterBottom>
        Job Performance Analytics
      </Typography>
      <Typography variant="body1" color="text.secondary" sx={{ mb: 3 }}>
        Analyze job execution performance, hash rates, and benchmark history across your agents.
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>{error}</Alert>}

      <JobAnalyticsFilters
        filterOptions={filterOptions}
        filter={filter}
        onFilterChange={setFilter}
        onApply={handleApply}
        onReset={handleReset}
        loading={filterOptionsLoading}
      />

      <JobAnalyticsSummaryCards summary={summary} loading={summaryLoading} />

      <Tabs
        value={tab}
        onChange={(_, v) => setTab(v)}
        sx={{ mb: 2, borderBottom: 1, borderColor: 'divider' }}
      >
        <Tab label="Job Performance" />
        <Tab label="Benchmark History" />
      </Tabs>

      {tab === 0 && (
        <>
          <JobHashRateChart
            data={timeline}
            loading={timelineLoading}
            resolution={resolution}
            onResolutionChange={handleResolutionChange}
          />
          <JobExecutionTable
            jobs={jobs}
            total={jobsTotal}
            page={jobsPage}
            pageSize={jobsPageSize}
            sortBy={sortBy}
            sortOrder={sortOrder}
            loading={jobsLoading}
            onPageChange={handlePageChange}
            onPageSizeChange={handlePageSizeChange}
            onSortChange={handleSortChange}
          />
        </>
      )}

      {tab === 1 && (
        <>
          <BenchmarkTrendChart filterOptions={filterOptions} />
          <BenchmarkHistoryTable filterOptions={filterOptions} />
        </>
      )}
    </Box>
  );
};

export default JobAnalyticsPage;
