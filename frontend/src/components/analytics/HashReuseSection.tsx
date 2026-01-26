/**
 * Hash Reuse Analytics Section
 * Displays hash-based password reuse analysis for NTLM/LM hashes
 */
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Paper,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Chip,
  Collapse,
  IconButton,
  Alert,
} from '@mui/material';
import {
  KeyboardArrowDown as ExpandMoreIcon,
  KeyboardArrowUp as ExpandLessIcon,
  Fingerprint as FingerprintIcon,
} from '@mui/icons-material';

interface HashReuseData {
  total_reused: number;
  percentage_reused: number;
  total_unique: number;
  hash_reuse_info: Array<{
    hash_value: string;
    hash_type: string;
    password?: string;
    users: Array<{
      username: string;
      hashlist_count: number;
    }>;
    total_occurrences: number;
    user_count: number;
  }>;
}

interface HashReuseSectionProps {
  data: HashReuseData;
}

interface HashReuseRowProps {
  item: any;
  t: (key: string) => string;
}

function HashReuseRow({ item, t }: HashReuseRowProps) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <TableRow sx={{ '& > *': { borderBottom: 'unset' } }}>
        <TableCell>
          <IconButton size="small" onClick={() => setOpen(!open)}>
            {open ? <ExpandLessIcon /> : <ExpandMoreIcon />}
          </IconButton>
        </TableCell>
        <TableCell>
          <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: '0.85rem' }}>
            {item.hash_value.substring(0, 16)}...
          </Typography>
        </TableCell>
        <TableCell>
          <Chip label={item.hash_type} size="small" color="primary" variant="outlined" />
        </TableCell>
        <TableCell>
          {item.password ? (
            <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
              {item.password}
            </Typography>
          ) : (
            <Typography variant="body2" color="text.secondary">
              —
            </Typography>
          )}
        </TableCell>
        <TableCell align="right">{item.user_count}</TableCell>
        <TableCell align="right">{item.total_occurrences}</TableCell>
      </TableRow>
      <TableRow>
        <TableCell style={{ paddingBottom: 0, paddingTop: 0 }} colSpan={6}>
          <Collapse in={open} timeout="auto" unmountOnExit>
            <Box sx={{ margin: 2 }}>
              <Typography variant="subtitle2" gutterBottom>
                {t('labels.usersWithHash')}
              </Typography>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>{t('columns.username')}</TableCell>
                    <TableCell align="right">{t('columns.hashlistCount')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {item.users.map((user: any, idx: number) => (
                    <TableRow key={idx}>
                      <TableCell>{user.username}</TableCell>
                      <TableCell align="right">{user.hashlist_count}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Box>
          </Collapse>
        </TableCell>
      </TableRow>
    </>
  );
}

export default function HashReuseSection({ data }: HashReuseSectionProps) {
  const { t } = useTranslation('analytics');

  if (!data || data.hash_reuse_info.length === 0) {
    return (
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
          <FingerprintIcon sx={{ fontSize: 32, color: 'primary.main', mr: 1 }} />
          <Typography variant="h5" component="h2">
            {t('sections.hashReuse')}
          </Typography>
        </Box>
        <Alert severity="info">
          {t('messages.noHashReuse')}
        </Alert>
      </Paper>
    );
  }

  const formatPercentage = (value: number) => value.toFixed(2) + '%';

  return (
    <Paper sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
        <FingerprintIcon sx={{ fontSize: 32, color: 'primary.main', mr: 1 }} />
        <Typography variant="h5" component="h2">
          {t('sections.hashReuse')}
        </Typography>
      </Box>

      <Typography variant="body2" color="text.secondary" paragraph>
        {t('descriptions.hashReuseAnalysis')}
      </Typography>

      {/* Summary Statistics */}
      <Box sx={{ display: 'flex', gap: 3, mb: 3 }}>
        <Box>
          <Typography variant="body2" color="text.secondary">
            {t('labels.totalReused')}
          </Typography>
          <Typography variant="h6">{data.total_reused.toLocaleString()}</Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            {t('labels.totalUnique')}
          </Typography>
          <Typography variant="h6">{data.total_unique.toLocaleString()}</Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            {t('labels.reusePercentage')}
          </Typography>
          <Typography variant="h6" color={data.percentage_reused > 20 ? 'error.main' : 'text.primary'}>
            {formatPercentage(data.percentage_reused)}
          </Typography>
        </Box>
      </Box>

      {/* Reused Hashes Table */}
      <TableContainer>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell width="50px" />
              <TableCell>{t('columns.hashValue')}</TableCell>
              <TableCell>{t('columns.type')}</TableCell>
              <TableCell>{t('columns.password')}</TableCell>
              <TableCell align="right">{t('columns.users')}</TableCell>
              <TableCell align="right">{t('columns.occurrences')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {data.hash_reuse_info.map((item, idx) => (
              <HashReuseRow key={idx} item={item} t={t} />
            ))}
          </TableBody>
        </Table>
      </TableContainer>

      {data.hash_reuse_info.length >= 50 && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
          {t('messages.top50Hashes')}
        </Typography>
      )}
    </Paper>
  );
}
