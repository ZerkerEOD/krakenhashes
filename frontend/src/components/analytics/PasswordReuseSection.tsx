/**
 * Password reuse section showing reuse count and list of affected users.
 * Displays one row per password with user lists and hashlist occurrence tracking.
 */
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Paper, Typography, Box, Chip, Button, Collapse, IconButton, Tooltip } from '@mui/material';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import ExpandLessIcon from '@mui/icons-material/ExpandLess';
import { ReuseStats, PasswordReuseInfo, UserOccurrence } from '../../types/analytics';
import CrackedPassword from '../common/CrackedPassword';
import { SimpleTable, SimpleColumn, useToast } from '../ui';
import { countPctColumns, CountPctRow } from './tableStyles';

interface PasswordReuseSectionProps {
  data: ReuseStats;
}

const DISPLAY_LIMIT = 5;

/**
 * "user (n), user (n), … and N more" with an expander for the remainder.
 * Shared with the hash-reuse section.
 */
export function ExpandableUserList({ users }: { users: Array<Pick<UserOccurrence, 'username' | 'hashlist_count'>> }) {
  const { t } = useTranslation('analytics');
  const [expanded, setExpanded] = useState(false);
  const formatUserText = (user: Pick<UserOccurrence, 'username' | 'hashlist_count'>) =>
    `${user.username} (${user.hashlist_count})`;

  const displayText = users.slice(0, DISPLAY_LIMIT).map(formatUserText).join(', ');
  const remainingUsers = users.slice(DISPLAY_LIMIT);

  if (remainingUsers.length === 0) {
    return <>{displayText}</>;
  }

  return (
    <Box>
      {displayText}
      <Button
        size="small"
        onClick={() => setExpanded((v) => !v)}
        endIcon={expanded ? <ExpandLessIcon /> : <ExpandMoreIcon />}
        sx={{ ml: 1 }}
      >
        {t('messages.andMoreUsers', { count: remainingUsers.length })}
      </Button>
      <Collapse in={expanded}>
        <Box sx={{ mt: 1, pl: 2 }}>
          {remainingUsers.map((user, idx) => (
            <Typography key={idx} variant="body2" sx={{ py: 0.5 }}>
              {formatUserText(user)}
            </Typography>
          ))}
        </Box>
      </Collapse>
    </Box>
  );
}

export default function PasswordReuseSection({ data }: PasswordReuseSectionProps) {
  const { t } = useTranslation('analytics');
  const toast = useToast();

  const hasData = data.total_reused > 0 && data.password_reuse_info && data.password_reuse_info.length > 0;

  if (!hasData) {
    return null;
  }

  const copyUsernames = async (users: UserOccurrence[]) => {
    const usernames = users.map((u) => u.username).join(', ');
    try {
      await navigator.clipboard.writeText(usernames);
      toast.success(t('messages.copiedSuccess'));
    } catch (err) {
      toast.error(err);
    }
  };

  const summaryRows: CountPctRow[] = [
    { key: 'reused', label: t('labels.passwordsReused'), count: data.total_reused, percentage: data.percentage_reused },
    {
      key: 'unique',
      label: t('labels.uniquePasswords'),
      count: data.total_unique,
      percentage: 100 - data.percentage_reused,
    },
  ];

  const columns: SimpleColumn<PasswordReuseInfo>[] = [
    {
      field: 'password',
      headerName: t('columns.password'),
      width: '15%',
      render: (p) => <Chip label={<CrackedPassword password={p.password} />} size="small" />,
    },
    {
      field: 'users',
      headerName: t('columns.usersHashlistCount'),
      width: '50%',
      render: (p) => <ExpandableUserList users={p.users} />,
    },
    { field: 'total_occurrences', headerName: t('columns.totalOccurrences'), width: '15%', align: 'right' },
    { field: 'user_count', headerName: t('columns.userCount'), width: '12%', align: 'right' },
    {
      field: 'actions',
      headerName: t('columns.actions'),
      width: '8%',
      align: 'center',
      render: (p) => (
        <Tooltip title={t('tooltips.copyUsernames')}>
          <IconButton size="small" onClick={() => copyUsernames(p.users)} aria-label={t('tooltips.copyUsernames')}>
            <ContentCopyIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      ),
    },
  ];

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.passwordReuse')}
      </Typography>

      {/* Summary */}
      <SimpleTable
        sx={{ mb: 3 }}
        rows={summaryRows}
        getRowKey={(r) => r.key}
        columns={countPctColumns(t('columns.metric'), t('columns.count'), t('columns.percentage'))}
      />

      {/* Password Reuse Table */}
      <Box>
        <Typography variant="h6" gutterBottom>
          {t('sections.reusedPasswords')}
        </Typography>
        <SimpleTable rows={data.password_reuse_info} maxRows={50} columns={columns} />
      </Box>
    </Paper>
  );
}
