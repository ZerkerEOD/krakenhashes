/**
 * Password reuse section showing reuse count and list of affected users.
 * Displays one row per password with user lists and hashlist occurrence tracking.
 */
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Paper,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TablePagination,
  Box,
  Chip,
  Button,
  Collapse,
  IconButton,
  Snackbar,
  Alert,
} from '@mui/material';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import ExpandLessIcon from '@mui/icons-material/ExpandLess';
import { ReuseStats, PasswordReuseInfo, UserOccurrence } from '../../types/analytics';
import { threeColumnTableStyles, passwordReuseTableStyles } from './tableStyles';

interface PasswordReuseSectionProps {
  data: ReuseStats;
}

export default function PasswordReuseSection({ data }: PasswordReuseSectionProps) {
  const { t } = useTranslation('analytics');
  const [page, setPage] = useState(0);
  const [rowsPerPage] = useState(50);
  const [expandedRows, setExpandedRows] = useState<Set<number>>(new Set());
  const [copySuccess, setCopySuccess] = useState(false);

  const hasData = data.total_reused > 0 && data.password_reuse_info && data.password_reuse_info.length > 0;

  if (!hasData) {
    return null;
  }

  const handleChangePage = (_event: unknown, newPage: number) => {
    setPage(newPage);
  };

  const toggleExpanded = (rowIndex: number) => {
    const newExpanded = new Set(expandedRows);
    if (newExpanded.has(rowIndex)) {
      newExpanded.delete(rowIndex);
    } else {
      newExpanded.add(rowIndex);
    }
    setExpandedRows(newExpanded);
  };

  const formatUsers = (users: UserOccurrence[], rowIndex: number) => {
    const displayLimit = 5;
    const displayUsers = users.slice(0, displayLimit);
    const remainingUsers = users.slice(displayLimit);
    const remainingCount = users.length - displayLimit;

    const formatUserText = (user: UserOccurrence) => `${user.username} (${user.hashlist_count})`;

    const displayText = displayUsers.map(formatUserText).join(', ');

    if (remainingCount > 0) {
      return (
        <Box>
          {displayText}
          <Button
            size="small"
            onClick={() => toggleExpanded(rowIndex)}
            endIcon={expandedRows.has(rowIndex) ? <ExpandLessIcon /> : <ExpandMoreIcon />}
            sx={{ ml: 1 }}
          >
            {t('messages.andMoreUsers', { count: remainingCount })}
          </Button>
          <Collapse in={expandedRows.has(rowIndex)}>
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

    return displayText;
  };

  const copyUsernames = (users: UserOccurrence[]) => {
    const usernames = users.map(u => u.username).join(', ');
    navigator.clipboard.writeText(usernames);
    setCopySuccess(true);
  };

  const handleCloseSnackbar = () => {
    setCopySuccess(false);
  };

  // Pagination
  const paginatedData = data.password_reuse_info.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  );

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Typography variant="h5" gutterBottom>
        {t('sections.passwordReuse')}
      </Typography>

      {/* Summary */}
      <Box sx={{ mb: 3 }}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('columns.metric')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{t('columns.count')}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{t('columns.percentage')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('labels.passwordsReused')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{data.total_reused.toLocaleString()}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{data.percentage_reused.toFixed(2)}%</TableCell>
            </TableRow>
            <TableRow>
              <TableCell sx={threeColumnTableStyles.labelCell}>{t('labels.uniquePasswords')}</TableCell>
              <TableCell sx={threeColumnTableStyles.countCell}>{data.total_unique.toLocaleString()}</TableCell>
              <TableCell sx={threeColumnTableStyles.percentageCell}>{(100 - data.percentage_reused).toFixed(2)}%</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </Box>

      {/* Password Reuse Table */}
      <Box>
        <Typography variant="h6" gutterBottom>
          {t('sections.reusedPasswords')}
        </Typography>
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell sx={passwordReuseTableStyles.passwordCell}>{t('columns.password')}</TableCell>
                <TableCell sx={passwordReuseTableStyles.usersCell}>{t('columns.usersHashlistCount')}</TableCell>
                <TableCell sx={passwordReuseTableStyles.occurrencesCell}>{t('columns.totalOccurrences')}</TableCell>
                <TableCell sx={passwordReuseTableStyles.userCountCell}>{t('columns.userCount')}</TableCell>
                <TableCell sx={passwordReuseTableStyles.actionsCell}>{t('columns.actions')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {paginatedData.map((passwordInfo: PasswordReuseInfo, index) => {
                const globalIndex = page * rowsPerPage + index;
                return (
                  <TableRow key={index}>
                    <TableCell sx={passwordReuseTableStyles.passwordCell}>
                      <Chip label={passwordInfo.password} size="small" />
                    </TableCell>
                    <TableCell sx={passwordReuseTableStyles.usersCell}>{formatUsers(passwordInfo.users, globalIndex)}</TableCell>
                    <TableCell sx={passwordReuseTableStyles.occurrencesCell}>{passwordInfo.total_occurrences}</TableCell>
                    <TableCell sx={passwordReuseTableStyles.userCountCell}>{passwordInfo.user_count}</TableCell>
                    <TableCell sx={passwordReuseTableStyles.actionsCell}>
                      <IconButton
                        size="small"
                        onClick={() => copyUsernames(passwordInfo.users)}
                        title={t('tooltips.copyUsernames')}
                      >
                        <ContentCopyIcon fontSize="small" />
                      </IconButton>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </TableContainer>
        <TablePagination
          rowsPerPageOptions={[50]}
          component="div"
          count={data.password_reuse_info.length}
          rowsPerPage={rowsPerPage}
          page={page}
          onPageChange={handleChangePage}
          labelRowsPerPage={t('pagination.rowsPerPage', { ns: 'common' }) as string}
        />
      </Box>

      {/* Success Snackbar */}
      <Snackbar
        open={copySuccess}
        autoHideDuration={3000}
        onClose={handleCloseSnackbar}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert onClose={handleCloseSnackbar} severity="success" sx={{ width: '100%' }}>
          {t('messages.copiedSuccess')}
        </Alert>
      </Snackbar>
    </Paper>
  );
}
