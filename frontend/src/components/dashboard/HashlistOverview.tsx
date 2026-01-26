import React, { useState } from 'react';
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Typography,
  LinearProgress,
  Chip,
  TablePagination,
  CircularProgress,
  Alert,
} from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../services/api';
import { useNavigate } from 'react-router-dom';

interface Hashlist {
  id: string;
  name: string;
  status: 'uploading' | 'processing' | 'ready' | 'error';
  total_hashes: number;
  cracked_hashes: number;
  clientName?: string;
  client_id?: string;
  exclude_from_potfile?: boolean;
}

interface UserHashlistsResponse {
  data: Hashlist[];
  total_count: number;
  limit: number;
  offset: number;
}

export default function HashlistOverview() {
  const { t } = useTranslation('dashboard');
  const navigate = useNavigate();
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(10);

  const { data: response, isLoading, error } = useQuery<UserHashlistsResponse>({
    queryKey: ['userHashlists', page, rowsPerPage],
    queryFn: async () => {
      const params = new URLSearchParams({
        limit: rowsPerPage.toString(),
        offset: (page * rowsPerPage).toString(),
      });
      const res = await api.get<UserHashlistsResponse>(`/api/user/hashlists?${params}`);
      return res.data;
    },
    refetchInterval: 5000, // Auto-refresh every 5 seconds like the jobs table
  });

  const handleChangePage = (event: unknown, newPage: number) => {
    setPage(newPage);
  };

  const handleChangeRowsPerPage = (event: React.ChangeEvent<HTMLInputElement>) => {
    setRowsPerPage(parseInt(event.target.value, 10));
    setPage(0);
  };

  const crackPercentage = (hashlist: Hashlist) => {
    return hashlist.total_hashes > 0 
      ? Math.round((hashlist.cracked_hashes / hashlist.total_hashes) * 100)
      : 0;
  };

  if (isLoading) {
    return (
      <Paper sx={{ p: 2, display: 'flex', flexDirection: 'column', height: '100%' }}>
        <Typography variant="h6" gutterBottom>
          {t('hashlistOverview.title')}
        </Typography>
        <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', flexGrow: 1 }}>
          <CircularProgress />
        </Box>
      </Paper>
    );
  }

  if (error) {
    return (
      <Paper sx={{ p: 2, display: 'flex', flexDirection: 'column' }}>
        <Typography variant="h6" gutterBottom>
          {t('hashlistOverview.title')}
        </Typography>
        <Alert severity="error">
          {t('hashlistOverview.loadError')}
        </Alert>
      </Paper>
    );
  }

  const hashlists = response?.data || [];
  const totalCount = response?.total_count || 0;

  return (
    <Paper sx={{ p: 2, display: 'flex', flexDirection: 'column' }}>
      <Typography variant="h6" gutterBottom>
        {t('hashlistOverview.title')}
      </Typography>
      
      {hashlists.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          {t('hashlistOverview.noHashlists')}
        </Typography>
      ) : (
        <>
          <TableContainer sx={{ flexGrow: 1 }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t('hashlistOverview.columns.name')}</TableCell>
                  <TableCell>{t('hashlistOverview.columns.client')}</TableCell>
                  <TableCell>{t('hashlistOverview.columns.status')}</TableCell>
                  <TableCell align="right">{t('hashlistOverview.columns.total')}</TableCell>
                  <TableCell align="right">{t('hashlistOverview.columns.cracked')}</TableCell>
                  <TableCell>{t('hashlistOverview.columns.progress')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {hashlists.map((hashlist) => (
                  <TableRow key={hashlist.id}>
                    <TableCell>
                      <Typography
                        component="span"
                        sx={{
                          cursor: 'pointer',
                          color: 'primary.main',
                          '&:hover': {
                            textDecoration: 'underline'
                          }
                        }}
                        onClick={() => navigate(`/hashlists/${hashlist.id}`)}
                      >
                        {hashlist.name}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      {hashlist.client_id && hashlist.clientName ? (
                        <Typography
                          component="span"
                          sx={{
                            cursor: 'pointer',
                            color: 'primary.main',
                            '&:hover': {
                              textDecoration: 'underline'
                            }
                          }}
                          onClick={() => navigate(`/pot/client/${hashlist.client_id}`)}
                        >
                          {hashlist.clientName}
                        </Typography>
                      ) : (
                        hashlist.clientName || '-'
                      )}
                    </TableCell>
                    <TableCell>
                      <Chip
                        label={t(`hashlistOverview.status.${hashlist.status}`)}
                        size="small"
                        color={
                          hashlist.status === 'ready' ? 'success' :
                          hashlist.status === 'error' ? 'error' :
                          'primary'
                        }
                      />
                    </TableCell>
                    <TableCell align="right">{hashlist.total_hashes.toLocaleString()}</TableCell>
                    <TableCell align="right">
                      <Typography
                        component="span"
                        sx={{
                          cursor: 'pointer',
                          color: 'primary.main',
                          '&:hover': {
                            textDecoration: 'underline'
                          }
                        }}
                        onClick={() => navigate(`/pot/hashlist/${hashlist.id}`)}
                      >
                        {hashlist.cracked_hashes.toLocaleString()}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                        <LinearProgress 
                          variant="determinate" 
                          value={crackPercentage(hashlist)} 
                          sx={{ flexGrow: 1, height: 6 }}
                        />
                        <Typography variant="caption" sx={{ minWidth: 35 }}>
                          {crackPercentage(hashlist)}%
                        </Typography>
                      </Box>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
          
          <TablePagination
            component="div"
            count={totalCount}
            page={page}
            onPageChange={handleChangePage}
            rowsPerPage={rowsPerPage}
            onRowsPerPageChange={handleChangeRowsPerPage}
            rowsPerPageOptions={[5, 10, 25]}
            labelRowsPerPage={t('pagination.rowsPerPage', { ns: 'common' }) as string}
          />
        </>
      )}
    </Paper>
  );
}