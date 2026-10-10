import React, { useState } from 'react';
import { Box, Button } from '@mui/material';
import { PageHeader } from '../components/ui';
import { Add as AddIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import HashlistsDashboard from '../components/hashlist/HashlistsDashboard';

const Hashlists: React.FC = () => {
  const { t } = useTranslation('hashlists');
  const [uploadDialogOpen, setUploadDialogOpen] = useState(false);

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('page.title') as string}
        description={t('page.description') as string}
        actions={
          <Button variant="contained" startIcon={<AddIcon />} onClick={() => setUploadDialogOpen(true)}>
            {t('uploadButton') as string}
          </Button>
        }
      />
      <HashlistsDashboard
        uploadDialogOpen={uploadDialogOpen}
        setUploadDialogOpen={setUploadDialogOpen}
      />
    </Box>
  );
};

export default Hashlists;