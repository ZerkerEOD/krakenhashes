/**
 * Shared table styling constants for analytics sections
 * Ensures consistent column widths and alignments across all analytics tables
 */
import type React from 'react';
import type { SimpleColumn } from '../ui';

// Standard 3-column table (Label | Count | Percentage)
export const threeColumnTableStyles = {
  labelCell: {
    width: '60%',
  },
  countCell: {
    width: '20%',
    textAlign: 'right' as const,
  },
  percentageCell: {
    width: '20%',
    textAlign: 'right' as const,
  },
};

// Standard 2-column table (Metric | Value)
export const twoColumnTableStyles = {
  labelCell: {
    width: '70%',
  },
  valueCell: {
    width: '30%',
    textAlign: 'right' as const,
  },
};

// Password Reuse table (5 columns)
export const passwordReuseTableStyles = {
  passwordCell: {
    width: '15%',
  },
  usersCell: {
    width: '50%',
  },
  occurrencesCell: {
    width: '15%',
    textAlign: 'right' as const,
  },
  userCountCell: {
    width: '12%',
    textAlign: 'right' as const,
  },
  actionsCell: {
    width: '8%',
    textAlign: 'center' as const,
  },
};

// Top Passwords table (3 columns with different layout)
export const topPasswordsTableStyles = {
  passwordCell: {
    width: '50%',
  },
  countCell: {
    width: '25%',
    textAlign: 'right' as const,
  },
  percentageCell: {
    width: '25%',
    textAlign: 'right' as const,
  },
};

// Mask Analysis table (4 columns)
export const maskAnalysisTableStyles = {
  maskCell: {
    width: '35%',
  },
  exampleCell: {
    width: '30%',
  },
  countCell: {
    width: '17.5%',
    textAlign: 'right' as const,
  },
  percentageCell: {
    width: '17.5%',
    textAlign: 'right' as const,
  },
};

/**
 * Column set for the standard "Label | Count | Percentage" report table
 * rendered with the UI kit's SimpleTable.
 */
export interface CountPctRow {
  key: string;
  label: React.ReactNode;
  count: number;
  percentage: number;
}

export const countPctColumns = (
  labelHeader: React.ReactNode,
  countHeader: React.ReactNode,
  percentageHeader: React.ReactNode,
  opts: { mono?: boolean } = {},
): SimpleColumn<CountPctRow>[] => [
  { field: 'label', headerName: labelHeader, width: '60%', mono: opts.mono, render: (r) => r.label },
  { field: 'count', headerName: countHeader, width: '20%', align: 'right', render: (r) => r.count.toLocaleString() },
  {
    field: 'percentage',
    headerName: percentageHeader,
    width: '20%',
    align: 'right',
    render: (r) => `${r.percentage.toFixed(2)}%`,
  },
];

/** Build CountPctRows from [key, label, stats] triples, dropping zero counts. */
export const nonZeroRows = (
  entries: Array<[string, React.ReactNode, { count: number; percentage: number } | undefined | null]>,
): CountPctRow[] =>
  entries
    .filter(([, , s]) => !!s && s.count > 0)
    .map(([key, label, s]) => ({ key, label, count: s!.count, percentage: s!.percentage }));
