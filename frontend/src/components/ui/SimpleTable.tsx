import React, { useState } from 'react';
import { Box, Button, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import EmptyState, { EmptyStateProps } from './EmptyState';

export interface SimpleColumn<R> {
  field: string;
  headerName: React.ReactNode;
  align?: 'left' | 'right' | 'center';
  width?: number | string;
  render?: (row: R, index: number) => React.ReactNode;
  /** Monospace cell (hashes, masks). */
  mono?: boolean;
  /** Keep on one line and truncate with an ellipsis. */
  noWrap?: boolean;
}

export interface SimpleTableProps<R> {
  rows: R[];
  columns: SimpleColumn<R>[];
  getRowKey?: (row: R, index: number) => string | number;
  /** Show only this many rows with a "show all" toggle. */
  maxRows?: number;
  emptyState?: React.ReactNode | EmptyStateProps;
  dense?: boolean;
  stickyHeader?: boolean;
  caption?: React.ReactNode;
  maxHeight?: number | string;
  sx?: any;
  /** Key/value tables: no header row. */
  hideHeader?: boolean;
  /** Per-row styling (totals, group headings). */
  getRowSx?: (row: R, index: number) => any;
  /** Render this row as a single full-width cell (group heading). */
  isGroupRow?: (row: R) => boolean;
}

const isEmptyStateProps = (v: unknown): v is EmptyStateProps =>
  typeof v === 'object' && v !== null && 'title' in (v as any) && !React.isValidElement(v);

/**
 * Static report table (analytics sections, diagnostics key/value lists).
 * Plain MUI Table so it prints and exports cleanly; use `DataTable` for
 * anything interactive.
 */
function SimpleTable<R>({
  rows,
  columns,
  getRowKey,
  maxRows,
  emptyState,
  dense = true,
  stickyHeader,
  caption,
  maxHeight,
  sx,
  hideHeader,
  getRowSx,
  isGroupRow,
}: SimpleTableProps<R>) {
  const { t } = useTranslation('common');
  const [showAll, setShowAll] = useState(false);
  const limited = maxRows !== undefined && !showAll && rows.length > maxRows;
  const visible = limited ? rows.slice(0, maxRows) : rows;

  if (rows.length === 0) {
    if (emptyState === undefined) return <EmptyState size="sm" title={t('labels.noData') as string} />;
    if (isEmptyStateProps(emptyState)) return <EmptyState size="sm" {...emptyState} />;
    return <>{emptyState}</>;
  }

  return (
    <Box sx={sx}>
      <TableContainer sx={{ maxHeight }}>
        <Table size={dense ? 'small' : 'medium'} stickyHeader={stickyHeader}>
          {caption && <caption style={{ captionSide: 'top', padding: '8px 12px' }}>{caption}</caption>}
          {!hideHeader && (
          <TableHead>
            <TableRow>
              {columns.map((c) => (
                <TableCell key={c.field} align={c.align} sx={{ width: c.width }}>
                  {c.headerName}
                </TableCell>
              ))}
            </TableRow>
          </TableHead>
          )}
          <TableBody>
            {visible.map((row, i) => (
              <TableRow key={getRowKey ? getRowKey(row, i) : i} hover={!isGroupRow?.(row)} sx={getRowSx?.(row, i)}>
                {isGroupRow?.(row) ? (
                  <TableCell colSpan={columns.length} sx={{ fontWeight: 600, bgcolor: 'action.hover' }}>
                    {columns[0]?.render ? columns[0].render(row, i) : String((row as any)[columns[0]?.field] ?? '')}
                  </TableCell>
                ) : columns.map((c) => (
                  <TableCell
                    key={c.field}
                    align={c.align}
                    sx={{
                      fontFamily: c.mono ? (th: any) => th.typography.monoFamily : undefined,
                      whiteSpace: c.noWrap ? 'nowrap' : undefined,
                      maxWidth: c.noWrap ? c.width ?? 320 : undefined,
                      overflow: c.noWrap ? 'hidden' : undefined,
                      textOverflow: c.noWrap ? 'ellipsis' : undefined,
                    }}
                  >
                    {c.render ? c.render(row, i) : String((row as any)[c.field] ?? '')}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {maxRows !== undefined && rows.length > maxRows && (
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', px: 1.5, py: 1 }}>
          <Typography variant="caption" color="text.secondary">
            {t('table.showingOf', { shown: visible.length, total: rows.length }) as string}
          </Typography>
          <Button size="small" onClick={() => setShowAll((v) => !v)}>
            {(showAll ? t('table.showFewer') : t('table.showAll')) as string}
          </Button>
        </Box>
      )}
    </Box>
  );
}

export default SimpleTable;
