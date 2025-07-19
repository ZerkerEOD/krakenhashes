import React from 'react';
import { Container, Typography, Box } from '@mui/material';
import PotTable from '../components/pot/PotTable';
import { potService } from '../services/pot';

export default function Pot() {
  const fetchData = async (limit: number, offset: number) => {
    return await potService.getPot({ limit, offset });
  };

  return (
    <Container maxWidth="lg" sx={{ mt: 4, mb: 4 }}>
      <Box sx={{ mb: 3 }}>
        <Typography variant="h4" component="h1" gutterBottom>
          Cracked Hashes (Pot)
        </Typography>
        <Typography variant="body1" color="text.secondary">
          View all successfully cracked password hashes across all hashlists and clients.
        </Typography>
      </Box>
      
      <PotTable
        title="All Cracked Hashes"
        fetchData={fetchData}
        contextType="master"
        contextName="master"
      />
    </Container>
  );
}