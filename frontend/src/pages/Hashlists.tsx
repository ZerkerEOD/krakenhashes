import React, { useState } from 'react';
import { Box, Typography, Button } from '@mui/material';
import { Add as AddIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import HashlistsDashboard from '../components/hashlist/HashlistsDashboard';

const Hashlists: React.FC = () => {
  const { t } = useTranslation('hashlists');
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            {t('page.title') as string}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {t('page.description') as string}
          </Typography>
        </Box>
        <Button
          variant="contained"
          startIcon={<AddIcon />}
          onClick={() => setUploadDialogOpen(true)}
        >
          {t('uploadButton') as string}
        </Button>
      </Box>
      <HashlistsDashboard
        uploadDialogOpen={uploadDialogOpen}
        setUploadDialogOpen={setUploadDialogOpen}
      />
    </Box>
  );
};

export default Hashlists;