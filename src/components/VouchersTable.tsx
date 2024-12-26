import React, { useEffect } from 'react';
import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  Button,
  Typography
} from '@mui/material';
import { format } from 'date-fns';
import { useVouchers } from '../hooks/useVouchers';

export const VouchersTable: React.FC = () => {
  const { vouchers, disableVoucher, fetchVouchers } = useVouchers();

  useEffect(() => {
    fetchVouchers();
  }, [fetchVouchers]);

  return (
    <TableContainer component={Paper}>
      <Table>
        <TableHead>
          <TableRow>
            <TableCell>Voucher Code</TableCell>
            <TableCell>Created By</TableCell>
            <TableCell>Multiple Use</TableCell>
            <TableCell>Created At</TableCell>
            <TableCell>Actions</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {vouchers.map((voucher) => (
            <TableRow key={voucher.code}>
              <TableCell>{voucher.code}</TableCell>
              <TableCell>{voucher.createdBy.username}</TableCell>
              <TableCell>{voucher.isContinuous ? 'Yes' : 'No'}</TableCell>
              <TableCell>
                {format(new Date(voucher.createdAt), 'yyyy-MM-dd HH:mm:ss')}
              </TableCell>
              <TableCell>
                <Button
                  variant="outlined"
                  color="error"
                  onClick={() => disableVoucher(voucher.code)}
                  disabled={!voucher.isActive}
                >
                  {!voucher.isActive ? 'Disabled' : 'Disable'}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}; 