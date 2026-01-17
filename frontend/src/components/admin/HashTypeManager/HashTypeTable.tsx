import React, { useState } from 'react';
import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  IconButton,
  Chip,
  Tooltip,
  TextField,
  Box,
  TablePagination,
  Typography,
  Checkbox,
} from '@mui/material';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/Delete';
import WarningIcon from '@mui/icons-material/Warning';
import WaterDropIcon from '@mui/icons-material/WaterDrop';
import BoltIcon from '@mui/icons-material/Bolt';
import SearchIcon from '@mui/icons-material/Search';
import { HashType } from '../../../types/hashType';

interface HashTypeTableProps {
  hashTypes: HashType[];
  onEdit: (hashType: HashType) => void;
  onDelete: (hashType: HashType) => void;
  loading?: boolean;
}

const HashTypeTable: React.FC<HashTypeTableProps> = ({
  hashTypes,
  onEdit,
  onDelete,
  loading = false,
}) => {
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(25);
  const [searchTerm, setSearchTerm] = useState('');

  const filteredHashTypes = hashTypes.filter((ht) => {
    const search = searchTerm.toLowerCase();
    return (
      ht.id.toString().includes(search) ||
      ht.name.toLowerCase().includes(search) ||
      (ht.description && ht.description.toLowerCase().includes(search))
    );
  });

  const handleChangePage = (event: unknown, newPage: number) => {
    setPage(newPage);
  };

  const handleChangeRowsPerPage = (event: React.ChangeEvent<HTMLInputElement>) => {
    setRowsPerPage(parseInt(event.target.value, 10));
    setPage(0);
  };

  const paginatedHashTypes = filteredHashTypes.slice(
    page * rowsPerPage,
    page * rowsPerPage + rowsPerPage
  );

  const truncateText = (text: string | null | undefined, maxLength: number = 50): string => {
    if (!text) return '';
    return text.length > maxLength ? text.substring(0, maxLength) + '...' : text;
  };

  return (
    <Box>
      <Box sx={{ mb: 2, display: 'flex', alignItems: 'center' }}>
        <SearchIcon sx={{ mr: 1, color: 'text.secondary' }} />
        <TextField
          placeholder="Search hash types..."
          variant="outlined"
          size="small"
          value={searchTerm}
          onChange={(e) => setSearchTerm(e.target.value)}
          sx={{ flexGrow: 1, maxWidth: 400 }}
        />
        <Typography variant="body2" sx={{ ml: 2, color: 'text.secondary' }}>
          {filteredHashTypes.length} hash types found
        </Typography>
      </Box>

      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell width="100">ID</TableCell>
              <TableCell>Name</TableCell>
              <TableCell>Description</TableCell>
              <TableCell>Example</TableCell>
              <TableCell align="center" width="80">Slow</TableCell>
              <TableCell align="center" width="80">Salted</TableCell>
              <TableCell align="center" width="120">Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={7} align="center">
                  Loading hash types...
                </TableCell>
              </TableRow>
            ) : paginatedHashTypes.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} align="center">
                  No hash types found
                </TableCell>
              </TableRow>
            ) : (
              paginatedHashTypes.map((hashType) => (
                <TableRow key={hashType.id} hover>
                  <TableCell>
                    <Typography variant="body2" sx={{ fontWeight: 'bold' }}>
                      {hashType.id}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Typography variant="body2">{hashType.name}</Typography>
                      {hashType.id === 1000 && (
                        <Tooltip title="Requires special processing">
                          <Chip
                            label="Processing"
                            size="small"
                            color="info"
                            icon={<BoltIcon />}
                          />
                        </Tooltip>
                      )}
                    </Box>
                  </TableCell>
                  <TableCell>
                    <Tooltip title={hashType.description || ''} arrow>
                      <Typography variant="body2" sx={{ cursor: hashType.description ? 'help' : 'default' }}>
                        {truncateText(hashType.description)}
                      </Typography>
                    </Tooltip>
                  </TableCell>
                  <TableCell>
                    <Tooltip title={hashType.example || ''} arrow>
                      <Typography
                        variant="body2"
                        sx={{
                          fontFamily: 'monospace',
                          fontSize: '0.85rem',
                          cursor: hashType.example ? 'help' : 'default',
                        }}
                      >
                        {truncateText(hashType.example, 30)}
                      </Typography>
                    </Tooltip>
                  </TableCell>
                  <TableCell align="center">
                    {hashType.slow && (
                      <Tooltip title="Slow hash algorithm (computationally expensive)">
                        <WarningIcon color="warning" fontSize="small" />
                      </Tooltip>
                    )}
                  </TableCell>
                  <TableCell align="center">
                    {hashType.is_salted && (
                      <Tooltip title="Uses per-hash salts (affects keyspace calculation)">
                        <WaterDropIcon color="info" fontSize="small" />
                      </Tooltip>
                    )}
                  </TableCell>
                  <TableCell align="center">
                    <IconButton
                      size="small"
                      onClick={() => onEdit(hashType)}
                      color="primary"
                    >
                      <EditIcon fontSize="small" />
                    </IconButton>
                    <IconButton
                      size="small"
                      onClick={() => onDelete(hashType)}
                      color="error"
                    >
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </TableContainer>

      <TablePagination
        rowsPerPageOptions={[10, 25, 50, 100]}
        component="div"
        count={filteredHashTypes.length}
        rowsPerPage={rowsPerPage}
        page={page}
        onPageChange={handleChangePage}
        onRowsPerPageChange={handleChangeRowsPerPage}
      />
    </Box>
  );
};

export default HashTypeTable;