import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Box, IconButton, LinearProgress, Paper, Tooltip } from '@mui/material';
import {
  DataGrid,
  GridActionsCellItem,
  GridColDef,
  GridPaginationModel,
  GridRowId,
  GridRowParams,
  GridSortModel,
  GridValidRowModel,
} from '@mui/x-data-grid';
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import KeyboardArrowRightIcon from '@mui/icons-material/KeyboardArrowRight';
import MoreVertIcon from '@mui/icons-material/MoreVert';
import { useTranslation } from 'react-i18next';
import EmptyState, { EmptyStateProps } from '../EmptyState';
import ErrorState from '../ErrorState';
import { TableSkeleton } from '../PageSkeleton';
import DataTableToolbar from './DataTableToolbar';
import BulkActionBar from './BulkActionBar';
import DetailDrawer from './DetailDrawer';
import gridLocaleText from './localeText';
import type { DataTableProps } from './types';

const MAX_PAGE_SIZE = 100; // DataGrid community limit
const DETAIL_PREFIX = '__kh_detail__';
const EXPAND_FIELD = '__kh_expand';
const ACTIONS_FIELD = '__kh_actions';

interface Persisted {
  pageSize?: number;
  sort?: GridSortModel;
}

/** Saved page size / sort for a `tableKey`. Server-paged owners use it to seed their own page size. */
export const readPersisted = (key?: string): Persisted => {
  if (!key) return {};
  try {
    const raw = window.localStorage.getItem(`kh.table.${key}`);
    return raw ? (JSON.parse(raw) as Persisted) : {};
  } catch {
    return {};
  }
};

const writePersisted = (key: string | undefined, patch: Persisted) => {
  if (!key) return;
  try {
    const next = { ...readPersisted(key), ...patch };
    window.localStorage.setItem(`kh.table.${key}`, JSON.stringify(next));
  } catch {
    /* ignore */
  }
};

const isEmptyStateProps = (v: unknown): v is EmptyStateProps =>
  typeof v === 'object' && v !== null && 'title' in (v as any) && !React.isValidElement(v);

/**
 * The application's list primitive: a DataGrid (community) with a toolbar,
 * search, selection + bulk bar, row actions, empty/error/skeleton states, and
 * expandable rows (inline synthetic rows or a side drawer).
 */
function DataTable<R extends GridValidRowModel>(props: DataTableProps<R>) {
  const {
    rows,
    columns,
    getRowId,
    loading,
    fetching,
    error,
    onRetry,
    pagination = { mode: 'client' },
    sorting = { mode: 'client' },
    toolbar,
    selection,
    bulkActions,
    rowActions,
    rowActionsInlineLimit = 2,
    onRowClick,
    rowLinkTo,
    rowClassName,
    detail,
    emptyState,
    skeletonRows = 8,
    height = 'auto',
    tableKey,
    hideFooter,
    gridProps,
    sx,
    flat,
  } = props;

  const { t, i18n } = useTranslation('common');
  const navigate = useNavigate();
  const persisted = useMemo(() => readPersisted(tableKey), [tableKey]);

  const resolveId = useCallback((row: R): GridRowId => (getRowId ? getRowId(row) : (row as any).id), [getRowId]);

  // ---- detail / expansion -------------------------------------------------
  const serverOrNoSort = sorting === false || sorting.mode === 'server';
  const serverOrNoPage = pagination === false || pagination.mode === 'server';
  const inlineAllowed = serverOrNoSort && serverOrNoPage;
  const requestedMode = detail?.mode ?? (inlineAllowed ? 'inline' : 'drawer');
  const detailMode: 'inline' | 'drawer' | undefined = detail
    ? requestedMode === 'inline' && !inlineAllowed
      ? 'drawer'
      : requestedMode
    : undefined;
  useEffect(() => {
    if (detail && requestedMode === 'inline' && !inlineAllowed && import.meta.env.DEV) {
      // eslint-disable-next-line no-console
      console.error('[DataTable] inline detail requires server/none sorting and pagination; falling back to drawer');
    }
  }, [detail, requestedMode, inlineAllowed]);

  const [internalExpanded, setInternalExpanded] = useState<GridRowId[]>([]);
  const expanded = detail?.expandedIds ?? internalExpanded;
  const setExpanded = useCallback(
    (ids: GridRowId[]) => {
      if (detail?.onExpandedChange) detail.onExpandedChange(ids);
      else setInternalExpanded(ids);
    },
    [detail]
  );
  const [drawerRow, setDrawerRow] = useState<R | null>(null);

  const toggleExpand = useCallback(
    (row: R) => {
      const id = resolveId(row);
      if (detailMode === 'drawer') {
        setDrawerRow(row);
        return;
      }
      setExpanded(expanded.includes(id) ? expanded.filter((x) => x !== id) : [...expanded, id]);
    },
    [detailMode, expanded, resolveId, setExpanded]
  );

  // ---- rows (inject synthetic detail rows) --------------------------------
  const displayRows = useMemo(() => {
    if (detailMode !== 'inline' || expanded.length === 0) return rows;
    const out: any[] = [];
    rows.forEach((row) => {
      out.push(row);
      const id = resolveId(row);
      if (expanded.includes(id)) out.push({ id: `${DETAIL_PREFIX}${id}`, __detailFor: id, __parent: row });
    });
    return out as R[];
  }, [rows, expanded, detailMode, resolveId]);

  const isDetailRow = (row: any) => Boolean(row && row.__detailFor !== undefined);
  const gridGetRowId = useCallback((row: any) => (isDetailRow(row) ? row.id : resolveId(row)), [resolveId]);

  // ---- columns (expand + actions) -----------------------------------------
  const gridColumns = useMemo<GridColDef<R>[]>(() => {
    const cols: GridColDef<R>[] = [];
    // Only show the expand column when at least one row on the page can expand.
    const anyExpandable = Boolean(detailMode) && (!detail?.canExpand || rows.some((r) => detail.canExpand!(r)));
    if (detailMode && anyExpandable) {
      cols.push({
        field: EXPAND_FIELD,
        headerName: '',
        width: 44,
        sortable: false,
        filterable: false,
        disableColumnMenu: true,
        resizable: false,
        align: 'center',
        renderCell: (params) => {
          const row = params.row as any;
          if (isDetailRow(row)) return null;
          if (detail?.canExpand && !detail.canExpand(row)) return null;
          const open = detailMode === 'inline' && expanded.includes(params.id);
          return (
            <IconButton
              size="small"
              aria-label={open ? (t('table.collapseRow') as string) : (t('table.expandRow') as string)}
              aria-expanded={open}
              onClick={(e) => {
                e.stopPropagation();
                toggleExpand(row);
              }}
            >
              {open ? <KeyboardArrowDownIcon fontSize="small" /> : <KeyboardArrowRightIcon fontSize="small" />}
            </IconButton>
          );
        },
      });
    }

    const userCols = columns.map((c, idx) => {
      if (detailMode !== 'inline') return c;
      // First user column renders the detail panel across all columns for synthetic rows.
      if (idx !== 0) return c;
      const total = columns.length + (rowActions ? 1 : 0);
      return {
        ...c,
        colSpan: (_value: any, row: any) => (isDetailRow(row) ? total : (typeof c.colSpan === 'function' ? undefined : c.colSpan) ?? 1),
        renderCell: (params: any) =>
          isDetailRow(params.row) ? (
            <Box sx={{ width: '100%', p: 2, bgcolor: 'surface.sunken' }} onClick={(e) => e.stopPropagation()}>
              {detail!.render(params.row.__parent)}
            </Box>
          ) : c.renderCell ? (
            c.renderCell(params)
          ) : (
            params.formattedValue ?? params.value
          ),
      } as GridColDef<R>;
    });
    cols.push(...userCols);

    if (rowActions) {
      cols.push({
        field: ACTIONS_FIELD,
        type: 'actions',
        headerName: '',
        width: 56 + Math.max(0, rowActionsInlineLimit - 1) * 36,
        align: 'right',
        sortable: false,
        resizable: false,
        getActions: (params: GridRowParams<R>) => {
          if (isDetailRow(params.row)) return [];
          const actions = rowActions(params.row).filter((a) => !a.hidden);
          // Convention: destructive actions always live in the ⋮ menu, never inline.
          const inline = actions.filter((a) => a.placement !== 'menu' && !a.danger).slice(0, rowActionsInlineLimit);
          const inlineKeys = new Set(inline.map((a) => a.key));
          const menu = actions.filter((a) => !inlineKeys.has(a.key));
          const items: React.ReactElement[] = inline.map((a) => (
            <Tooltip key={a.key} title={a.tooltip ?? a.label}>
              <span>
                <GridActionsCellItem
                  icon={a.icon ?? <MoreVertIcon fontSize="small" />}
                  label={a.label}
                  disabled={a.disabled}
                  color={a.danger ? 'error' : 'default'}
                  onClick={(e: React.MouseEvent) => {
                    e.stopPropagation();
                    a.onClick(params.row);
                  }}
                />
              </span>
            </Tooltip>
          ));
          menu.forEach((a) =>
            items.push(
              <GridActionsCellItem
                key={a.key}
                icon={a.icon}
                label={a.label}
                showInMenu
                disabled={a.disabled}
                sx={a.danger ? { color: 'error.main' } : undefined}
                onClick={(e: React.MouseEvent) => {
                  e.stopPropagation();
                  a.onClick(params.row);
                }}
              />
            )
          );
          return items;
        },
      } as GridColDef<R>);
    }
    return cols;
  }, [columns, detail, detailMode, expanded, rowActions, rowActionsInlineLimit, rows, t, toggleExpand]);

  // ---- fetching indicator -------------------------------------------------
  // Only show the progress bar for refetches the user caused (page, sort,
  // search, filter); background polls refresh rows silently.
  const [userPending, setUserPending] = useState(false);
  const sawFetchRef = useRef(false);
  const markUserAction = useCallback(() => {
    sawFetchRef.current = false;
    setUserPending(true);
  }, []);
  useEffect(() => {
    if (!userPending) return;
    if (fetching) {
      sawFetchRef.current = true;
      return;
    }
    if (sawFetchRef.current) {
      setUserPending(false);
      return;
    }
    // The action didn't trigger a fetch (client-side table): stop waiting.
    const timer = setTimeout(() => setUserPending(false), 1500);
    return () => clearTimeout(timer);
  }, [fetching, userPending]);

  // ---- pagination ---------------------------------------------------------
  const [clientPagination, setClientPagination] = useState<GridPaginationModel>({
    page: 0,
    pageSize: Math.min(
      MAX_PAGE_SIZE,
      persisted.pageSize ?? (pagination && pagination.mode === 'client' ? pagination.initialPageSize ?? 25 : MAX_PAGE_SIZE)
    ),
  });
  const pageSizeOptions = (pagination ? pagination.pageSizeOptions : undefined) ?? [10, 25, 50, 100];
  const paginationModel: GridPaginationModel =
    pagination && pagination.mode === 'server'
      ? { page: pagination.page, pageSize: Math.min(pagination.pageSize, MAX_PAGE_SIZE) }
      : pagination === false
      ? { page: 0, pageSize: MAX_PAGE_SIZE }
      : clientPagination;
  const onPaginationModelChange = (model: GridPaginationModel) => {
    markUserAction();
    writePersisted(tableKey, { pageSize: model.pageSize });
    if (pagination && pagination.mode === 'server') pagination.onChange(model);
    else setClientPagination(model);
  };
  // `pagination: false` still shows a footer if the grid is forced to page (>100 rows).
  const footerHidden = hideFooter || (pagination === false && rows.length <= MAX_PAGE_SIZE);

  // ---- sorting ------------------------------------------------------------
  const [clientSort, setClientSort] = useState<GridSortModel>(
    persisted.sort ?? (sorting && sorting.mode === 'client' ? sorting.initial ?? [] : [])
  );
  const sortModel = sorting && sorting.mode === 'server' ? sorting.model : clientSort;
  const onSortModelChange = (model: GridSortModel) => {
    markUserAction();
    writePersisted(tableKey, { sort: model });
    if (sorting && sorting.mode === 'server') sorting.onChange(model);
    else setClientSort(model);
  };

  // ---- selection ----------------------------------------------------------
  const selectedCount = selection?.model.length ?? 0;
  const clearSelection = () => selection?.onChange([]);

  // ---- states -------------------------------------------------------------
  const showSkeleton = Boolean(loading) && rows.length === 0;
  const showError = Boolean(error) && rows.length === 0 && !loading;
  const showEmpty = !loading && !error && rows.length === 0;

  const renderEmpty = () => {
    if (emptyState === undefined) return <EmptyState size="sm" title={t('labels.noData') as string} />;
    if (isEmptyStateProps(emptyState)) return <EmptyState size="sm" {...emptyState} />;
    return <>{emptyState}</>;
  };

  const autoHeight = height === 'auto';

  const content = (
    <>
      {selectedCount > 0 && bulkActions ? (
        <BulkActionBar count={selectedCount} onClear={clearSelection}>
          {bulkActions(selection!.model, clearSelection)}
        </BulkActionBar>
      ) : (
        toolbar && (
          <Box onChangeCapture={markUserAction} onClickCapture={markUserAction}>
            <DataTableToolbar {...toolbar} />
          </Box>
        )
      )}
      <Box sx={{ position: 'relative', height: autoHeight ? undefined : height, display: 'flex', flexDirection: 'column' }}>
        {fetching && userPending && !showSkeleton && (
          <LinearProgress sx={{ position: 'absolute', top: 0, left: 0, right: 0, height: 2, zIndex: 2 }} />
        )}
        {showSkeleton ? (
          <TableSkeleton rows={skeletonRows} columns={Math.min(columns.length, 6)} />
        ) : showError ? (
          <ErrorState error={error} onRetry={onRetry} />
        ) : showEmpty ? (
          renderEmpty()
        ) : (
          <DataGrid<R>
            rows={displayRows}
            columns={gridColumns}
            getRowId={gridGetRowId}
            autoHeight={autoHeight}
            loading={false}
            localeText={gridLocaleText(i18n.language)}
            // pagination
            paginationMode={pagination && pagination.mode === 'server' ? 'server' : 'client'}
            paginationModel={paginationModel}
            onPaginationModelChange={onPaginationModelChange}
            pageSizeOptions={pageSizeOptions.filter((n) => n <= MAX_PAGE_SIZE)}
            rowCount={pagination && pagination.mode === 'server' ? pagination.rowCount : undefined}
            hideFooter={footerHidden}
            // sorting
            sortingMode={sorting && sorting.mode === 'server' ? 'server' : 'client'}
            sortModel={sortModel}
            onSortModelChange={onSortModelChange}
            disableColumnSorting={sorting === false}
            // selection
            checkboxSelection={Boolean(selection)}
            disableRowSelectionOnClick
            rowSelectionModel={selection?.model ?? []}
            onRowSelectionModelChange={(ids) => selection?.onChange(ids as GridRowId[])}
            isRowSelectable={(params) =>
              !isDetailRow(params.row) && (selection?.isRowSelectable ? selection.isRowSelectable(params.row) : true)
            }
            keepNonExistentRowsSelected={pagination !== false && pagination.mode === 'server'}
            // rows
            // Rows grow to fit multi-line cells (name + subtitle, chips); 40px minimum via the theme.
            getRowHeight={() => 'auto'}
            getEstimatedRowHeight={() => 44}
            getRowClassName={(params) =>
              [
                String(params.id).startsWith(DETAIL_PREFIX) ? 'kh-detail-row' : '',
                onRowClick || rowLinkTo ? 'kh-row-clickable' : '',
                !String(params.id).startsWith(DETAIL_PREFIX) && rowClassName ? rowClassName(params.row as R) ?? '' : '',
              ]
                .filter(Boolean)
                .join(' ')
            }
            onRowClick={(params, event) => {
              if (isDetailRow(params.row)) return;
              const target = event.target as HTMLElement;
              if (target.closest('a, button, input, [role="button"], .MuiChip-clickable')) return;
              if (rowLinkTo) {
                const to = rowLinkTo(params.row);
                if (to) {
                  navigate(to);
                  return;
                }
              }
              onRowClick?.(params.row);
            }}
            disableVirtualization={autoHeight && displayRows.length <= 50}
            sx={{
              border: 0,
              borderRadius: 0,
              '& .MuiDataGrid-virtualScroller': { minHeight: autoHeight ? undefined : 120 },
            }}
            {...gridProps}
          />
        )}
      </Box>
      {detailMode === 'drawer' && detail && (
        <DetailDrawer
          open={Boolean(drawerRow)}
          onClose={() => setDrawerRow(null)}
          title={drawerRow && detail.drawerTitle ? detail.drawerTitle(drawerRow) : undefined}
          width={detail.drawerWidth}
        >
          {drawerRow && detail.render(drawerRow)}
        </DetailDrawer>
      )}
    </>
  );

  if (flat) return <Box sx={sx}>{content}</Box>;
  return (
    <Paper variant="outlined" sx={{ overflow: 'hidden', ...(sx as any) }} aria-label={props['aria-label']}>
      {content}
    </Paper>
  );
}

export default DataTable;
