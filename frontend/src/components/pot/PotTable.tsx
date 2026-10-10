import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Box,
  Button,
  Chip,
  CircularProgress,
  IconButton,
  InputAdornment,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import {
  Search as SearchIcon,
  ContentCopy as CopyIcon,
  Download as DownloadIcon,
  FilterList as FilterListIcon,
  Clear as ClearIcon,
} from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../services/api';
import { CrackedHash, PotResponse } from '../../services/pot';
import CrackedPassword from '../common/CrackedPassword';
import { toPotfilePlain } from '../../utils/hexPlain';
import { DataTable, useToast } from '../ui';

interface PotTableProps {
  title: string;
  fetchData: (limit: number, offset: number, search?: string) => Promise<PotResponse>;
  filterParam?: string;
  filterValue?: string;
  contextType: 'master' | 'hashlist' | 'client' | 'job';
  contextName: string;
  contextId?: string;
}

type DownloadFormat = 'hash-pass' | 'user-pass' | 'user' | 'pass' | 'domain-user' | 'domain-user-pass' | 'potfile';

/** A cell value that copies itself on click. `translate="no"` keeps browser translators off sensitive data. */
const CopyCell: React.FC<{ title: string; onCopy: () => void; mono?: boolean; children: React.ReactNode }> = ({ title, onCopy, mono, children }) => (
  <Tooltip title={title}>
    <Box
      component="span"
      role="button"
      tabIndex={0}
      translate="no"
      className="notranslate"
      onClick={(e) => {
        e.stopPropagation();
        onCopy();
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          e.stopPropagation();
          onCopy();
        }
      }}
      sx={{
        cursor: 'pointer',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
        ...(mono ? { fontFamily: (th: any) => th.typography.monoFamily, fontSize: '0.875rem' } : {}),
        '&:hover': { textDecoration: 'underline' },
      }}
    >
      {children}
    </Box>
  </Tooltip>
);

export default function PotTable({ title, fetchData, filterParam, filterValue, contextType, contextName, contextId }: PotTableProps) {
  const { t } = useTranslation('pot');
  const toast = useToast();
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(100);

  // Server-side search: what the user types vs. what was submitted.
  const [searchInput, setSearchInput] = useState('');
  const [activeSearch, setActiveSearch] = useState('');
  // Client-side filter over the current page.
  const [filterTerm, setFilterTerm] = useState('');
  const [downloadingFormat, setDownloadingFormat] = useState<string | null>(null);

  // The pages recreate `fetchData` freely; keep the latest in a ref so it doesn't churn the query key.
  const fetchRef = useRef(fetchData);
  useEffect(() => {
    fetchRef.current = fetchData;
  }, [fetchData]);

  const query = useQuery({
    queryKey: ['pot', contextType, contextId ?? contextName, filterValue ?? '', page, pageSize, activeSearch],
    queryFn: () => fetchRef.current(pageSize, page * pageSize, activeSearch || undefined),
    placeholderData: keepPreviousData,
  });

  useEffect(() => {
    if (query.isError) toast.error(t('errors.loadFailed') as string);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.isError]);

  const data = useMemo(() => query.data?.hashes ?? [], [query.data]);
  const totalCount = query.data?.total_count ?? 0;
  const isSearching = query.isFetching && activeSearch !== '';
  const hasUsernameData = data.some((h) => h.username && h.username.trim() !== '');

  const handleSearch = useCallback(() => {
    const trimmed = searchInput.trim();
    if (trimmed !== activeSearch) {
      setActiveSearch(trimmed);
      setPage(0);
      setFilterTerm('');
    }
  }, [searchInput, activeSearch]);

  const handleClearSearch = useCallback(() => {
    setSearchInput('');
    setActiveSearch('');
    setPage(0);
  }, []);

  const copyToClipboard = useCallback(
    (text: string) => {
      navigator.clipboard.writeText(text);
      toast.success(t('notifications.copiedToClipboard') as string);
    },
    [t, toast]
  );

  const downloadFormat = async (format: DownloadFormat) => {
    try {
      setDownloadingFormat(format);
      let url = '';
      if (contextType === 'master') url = `/api/pot/download/${format}`;
      else if (contextType === 'hashlist' && contextId) url = `/api/pot/hashlist/${contextId}/download/${format}`;
      else if (contextType === 'client' && contextId) url = `/api/pot/client/${contextId}/download/${format}`;
      else if (contextType === 'job' && contextId) url = `/api/pot/job/${contextId}/download/${format}`;

      const response = await api.get(url, { responseType: 'blob' });
      const blob = new Blob([response.data], { type: 'text/plain' });
      const downloadUrl = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = downloadUrl;

      const contentDisposition = response.headers['content-disposition'];
      let filename = `${contextName}-${format}.lst`;
      if (contentDisposition) {
        const m = contentDisposition.match(/filename[^;=\n]*=((['"])(.*?)\2|[^;\n]*)/i);
        if (m && m[3]) filename = m[3];
        else {
          const fallback = contentDisposition.match(/filename=([^;\n]*)/i);
          if (fallback && fallback[1]) filename = fallback[1].trim();
        }
      }

      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      window.URL.revokeObjectURL(downloadUrl);
      toast.success(t('export.downloaded', { filename }) as string);
    } catch (err) {
      console.error('Error downloading format:', err);
      toast.error(t('export.downloadFailed') as string);
    } finally {
      setDownloadingFormat(null);
    }
  };

  const filteredData = useMemo(() => {
    if (!filterTerm) return data;
    const f = filterTerm.toLowerCase();
    return data.filter(
      (h) =>
        h.original_hash.toLowerCase().includes(f) ||
        h.password.toLowerCase().includes(f) ||
        (h.username && h.username.toLowerCase().includes(f)) ||
        (h.domain && h.domain.toLowerCase().includes(f))
    );
  }, [data, filterTerm]);

  const exportData = () => {
    // Exports the rows currently shown (current page after the local filter).
    const exportText = filteredData.map((h) => `${h.original_hash}:${toPotfilePlain(h.password)}`).join('\n');
    const blob = new Blob([exportText], { type: 'text/plain' });
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `cracked_hashes_${new Date().toISOString().split('T')[0]}.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    window.URL.revokeObjectURL(url);
    toast.success(t('notifications.exported') as string);
  };

  const columns = useMemo<GridColDef<CrackedHash>[]>(
    () => [
      {
        field: 'original_hash',
        headerName: t('columns.originalHash') as string,
        flex: 3,
        minWidth: 240,
        renderCell: (p) => (
          <Box
            component="span"
            translate="no"
            className="notranslate"
            sx={{ fontFamily: (th: any) => th.typography.monoFamily, fontSize: '0.875rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            title={p.row.original_hash}
          >
            {p.row.original_hash}
          </Box>
        ),
      },
      {
        field: 'domain',
        headerName: t('columns.domain') as string,
        flex: 1,
        minWidth: 110,
        renderCell: (p) =>
          p.row.domain ? (
            <CopyCell title={t('tooltips.copyDomain') as string} onCopy={() => copyToClipboard(p.row.domain!)}>
              {p.row.domain}
            </CopyCell>
          ) : (
            '-'
          ),
      },
      {
        field: 'username',
        headerName: t('columns.username') as string,
        flex: 1,
        minWidth: 110,
        renderCell: (p) =>
          p.row.username ? (
            <CopyCell title={t('tooltips.copyUsername') as string} onCopy={() => copyToClipboard(p.row.username!)}>
              {p.row.username}
            </CopyCell>
          ) : (
            '-'
          ),
      },
      {
        field: 'password',
        headerName: t('columns.password') as string,
        flex: 1,
        minWidth: 120,
        renderCell: (p) => (
          <CopyCell title={t('tooltips.copyPassword') as string} onCopy={() => copyToClipboard(p.row.password)} mono>
            <CrackedPassword password={p.row.password} />
          </CopyCell>
        ),
      },
      {
        field: 'hash_type_id',
        headerName: t('columns.hashType') as string,
        width: 100,
      },
    ],
    [t, copyToClipboard]
  );

  const exportButtons: { format: DownloadFormat; label: string; needsUser?: boolean; color?: 'secondary' }[] = [
    { format: 'hash-pass', label: t('export.hashPass') as string },
    { format: 'user-pass', label: t('export.userPass') as string, needsUser: true },
    { format: 'user', label: t('export.username') as string, needsUser: true },
    { format: 'pass', label: t('export.password') as string },
    { format: 'domain-user', label: t('export.domainUser') as string, needsUser: true },
    { format: 'domain-user-pass', label: t('export.domainUserPass') as string, needsUser: true },
    { format: 'potfile', label: t('export.potfile') as string, color: 'secondary' },
  ];

  return (
    <Box sx={{ width: '100%', mb: 2 }}>
      <Box sx={{ display: 'flex', gap: 1, mb: 2, flexWrap: 'wrap' }}>
        {exportButtons.map((b) => (
          <Button
            key={b.format}
            size="small"
            variant="outlined"
            color={b.color}
            startIcon={downloadingFormat === b.format ? <CircularProgress size={14} color="inherit" /> : <DownloadIcon />}
            onClick={() => void downloadFormat(b.format)}
            disabled={downloadingFormat !== null || (b.needsUser && !hasUsernameData)}
          >
            {b.label}
          </Button>
        ))}
      </Box>

      <DataTable<CrackedHash>
        rows={filteredData}
        columns={columns}
        loading={query.isLoading}
        fetching={query.isFetching && !query.isLoading}
        error={query.error ? (t('errors.loadFailed') as string) : undefined}
        onRetry={() => void query.refetch()}
        pagination={{
          mode: 'server',
          page,
          pageSize,
          rowCount: totalCount,
          pageSizeOptions: [25, 50, 100],
          onChange: (m) => {
            if (m.pageSize !== pageSize) setPage(0);
            else setPage(m.page);
            setPageSize(m.pageSize);
          },
        }}
        sorting={false}
        toolbar={{
          title,
          subtitle:
            filterParam && filterValue ? (t('filter.filteredBy', { param: filterParam, value: filterValue }) as string) : undefined,
          filters: (
            <>
              <TextField
                size="small"
                placeholder={t('search.placeholder') as string}
                value={searchInput}
                onChange={(e) => setSearchInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleSearch();
                }}
                disabled={isSearching}
                sx={{ minWidth: 250 }}
                InputProps={{
                  startAdornment: (
                    <InputAdornment position="start">
                      <SearchIcon color={activeSearch ? 'primary' : 'inherit'} />
                    </InputAdornment>
                  ),
                  endAdornment: (
                    <InputAdornment position="end">
                      {activeSearch && (
                        <IconButton size="small" onClick={handleClearSearch} sx={{ mr: 0.5 }} aria-label="clear search">
                          <ClearIcon fontSize="small" />
                        </IconButton>
                      )}
                      <Button
                        size="small"
                        variant="contained"
                        onClick={handleSearch}
                        disabled={isSearching || searchInput === activeSearch}
                        sx={{ minWidth: 'auto', px: 1.5 }}
                      >
                        {isSearching ? <CircularProgress size={16} color="inherit" /> : (t('search.button') as string)}
                      </Button>
                    </InputAdornment>
                  ),
                }}
              />
              <TextField
                size="small"
                placeholder={t('search.filterPlaceholder') as string}
                value={filterTerm}
                onChange={(e) => setFilterTerm(e.target.value)}
                sx={{ minWidth: 180 }}
                InputProps={{
                  startAdornment: (
                    <InputAdornment position="start">
                      <FilterListIcon />
                    </InputAdornment>
                  ),
                }}
              />
              {activeSearch && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Chip label={`Search: "${activeSearch}"`} onDelete={handleClearSearch} color="primary" variant="outlined" size="small" />
                  <Typography variant="body2" color="text.secondary">
                    {t('search.resultsFound', { count: totalCount }) as string}
                  </Typography>
                </Box>
              )}
            </>
          ),
          actions: (
            <Tooltip title={t('export.exportVisible') as string}>
              <span>
                <IconButton onClick={exportData} disabled={filteredData.length === 0} aria-label={t('export.exportVisible') as string}>
                  <DownloadIcon />
                </IconButton>
              </span>
            </Tooltip>
          ),
        }}
        rowActions={() => [
          {
            key: 'copy',
            label: t('tooltips.copyHash') as string,
            icon: <CopyIcon fontSize="small" />,
            onClick: (r) => copyToClipboard(`${r.original_hash}:${toPotfilePlain(r.password)}`),
          },
        ]}
        aria-label="cracked hashes table"
      />
    </Box>
  );
}
