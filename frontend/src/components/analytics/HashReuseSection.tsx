/**
 * Hash Reuse Analytics Section
 * Displays hash-based password reuse analysis for NTLM/LM hashes
 */
import React, { useState } from 'react';
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

function HashReuseRow({ item }: { item: any }) {
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
                Users with this hash:
              </Typography>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>Username</TableCell>
                    <TableCell align="right">Hashlist Count</TableCell>
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
  if (!data || data.hash_reuse_info.length === 0) {
    return (
      <Paper sx={{ p: 3, mb: 3 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
          <FingerprintIcon sx={{ fontSize: 32, color: 'primary.main', mr: 1 }} />
          <Typography variant="h5" component="h2">
            Hash-Based Password Reuse
          </Typography>
        </Box>
        <Alert severity="info">
          No hash reuse detected. All NTLM/LM hash values are unique.
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
          Hash-Based Password Reuse
        </Typography>
      </Box>

      <Typography variant="body2" color="text.secondary" paragraph>
        Analysis of identical hash values across different users (NTLM/LM only). This detects cases where
        multiple users have the exact same password, even if the plaintext is unknown.
      </Typography>

      {/* Summary Statistics */}
      <Box sx={{ display: 'flex', gap: 3, mb: 3 }}>
        <Box>
          <Typography variant="body2" color="text.secondary">
            Total Reused
          </Typography>
          <Typography variant="h6">{data.total_reused.toLocaleString()}</Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            Total Unique
          </Typography>
          <Typography variant="h6">{data.total_unique.toLocaleString()}</Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            Reuse Percentage
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
              <TableCell>Hash Value</TableCell>
              <TableCell>Type</TableCell>
              <TableCell>Password</TableCell>
              <TableCell align="right">Users</TableCell>
              <TableCell align="right">Occurrences</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {data.hash_reuse_info.map((item, idx) => (
              <HashReuseRow key={idx} item={item} />
            ))}
          </TableBody>
        </Table>
      </TableContainer>

      {data.hash_reuse_info.length >= 50 && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
          Showing top 50 most reused hashes
        </Typography>
      )}
    </Paper>
  );
}
