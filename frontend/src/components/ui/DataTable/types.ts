import React from 'react';
import type {
  DataGridProps,
  GridColDef,
  GridPaginationModel,
  GridRowId,
  GridSortModel,
  GridValidRowModel,
} from '@mui/x-data-grid';
import type { SxProps, Theme } from '@mui/material/styles';
import type { EmptyStateProps } from '../EmptyState';

export interface DataTableAction<R> {
  key: string;
  label: string;
  icon?: React.ReactElement;
  onClick: (row: R) => void;
  disabled?: boolean;
  hidden?: boolean;
  /** Red text in the menu / red icon inline. */
  danger?: boolean;
  /** Force inline (icon button) or menu placement; default: first `rowActionsInlineLimit` are inline. */
  placement?: 'inline' | 'menu';
  tooltip?: string;
}

export interface DataTableSearch {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  /** Debounce before `onChange` fires; default 400ms. */
  debounceMs?: number;
  autoFocus?: boolean;
}

export interface DataTableToolbarProps {
  title?: React.ReactNode;
  subtitle?: React.ReactNode;
  search?: DataTableSearch;
  /** Filters, chips, selects: rendered after the search box. */
  filters?: React.ReactNode;
  /** Right-aligned actions (Add, Refresh...). */
  actions?: React.ReactNode;
}

export type DataTablePagination =
  | { mode: 'client'; pageSizeOptions?: number[]; initialPageSize?: number }
  | {
      mode: 'server';
      page: number;
      pageSize: number;
      rowCount: number;
      onChange: (model: GridPaginationModel) => void;
      pageSizeOptions?: number[];
    }
  | false;

export type DataTableSorting =
  | { mode: 'client'; initial?: GridSortModel }
  | { mode: 'server'; model: GridSortModel; onChange: (model: GridSortModel) => void }
  | false;

export interface DataTableSelection {
  model: GridRowId[];
  onChange: (ids: GridRowId[]) => void;
  isRowSelectable?: (row: any) => boolean;
}

export interface DataTableDetail<R> {
  render: (row: R) => React.ReactNode;
  /**
   * `inline` injects a full-width row under the parent (needs server or no
   * sorting/pagination so synthetic rows are not re-ordered). `drawer` opens
   * a side panel. Default `inline` when allowed, else `drawer`.
   */
  mode?: 'inline' | 'drawer';
  expandedIds?: GridRowId[];
  onExpandedChange?: (ids: GridRowId[]) => void;
  drawerTitle?: (row: R) => React.ReactNode;
  drawerWidth?: number;
  /** Hide the expand toggle for rows that have nothing to show. */
  canExpand?: (row: R) => boolean;
}

export interface DataTableProps<R extends GridValidRowModel> {
  rows: R[];
  columns: GridColDef<R>[];
  getRowId?: (row: R) => GridRowId;
  /** First load (no rows yet): skeleton overlay. */
  loading?: boolean;
  /** A refetch is in flight; a thin progress bar shows only when the user caused it (page/sort/search/filter). */
  fetching?: boolean;
  error?: unknown;
  onRetry?: () => void;
  pagination?: DataTablePagination;
  sorting?: DataTableSorting;
  toolbar?: DataTableToolbarProps;
  selection?: DataTableSelection;
  /** Replaces the toolbar while rows are selected. */
  bulkActions?: (selectedIds: GridRowId[], clear: () => void) => React.ReactNode;
  rowActions?: (row: R) => DataTableAction<R>[];
  /** How many actions render as icon buttons before overflowing into a kebab menu. Default 2. */
  rowActionsInlineLimit?: number;
  onRowClick?: (row: R) => void;
  /** Navigate on row click. */
  rowLinkTo?: (row: R) => string | undefined;
  /** Extra class names for a row (merged with the table's own). */
  rowClassName?: (row: R) => string | undefined;
  detail?: DataTableDetail<R>;
  emptyState?: React.ReactNode | EmptyStateProps;
  skeletonRows?: number;
  /** `auto` grows with content; a number/string fixes the grid height (scrolling body). */
  height?: number | string | 'auto';
  /** Persist page size / sort under `kh.table.<key>`. */
  tableKey?: string;
  /** Hide the whole footer (pagination) even in client mode. */
  hideFooter?: boolean;
  /** Escape hatch for DataGrid props. */
  gridProps?: Partial<Omit<DataGridProps<R>, 'rows' | 'columns'>>;
  'aria-label'?: string;
  sx?: SxProps<Theme>;
  /** Render without the outer Paper (when already inside a card). */
  flat?: boolean;
}
