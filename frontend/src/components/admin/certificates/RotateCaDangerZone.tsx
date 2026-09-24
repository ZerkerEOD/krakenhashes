import React, { useState } from 'react';
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Alert,
  Button,
  Paper,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { useTranslation } from 'react-i18next';
import { ROTATE_CA_CONFIRMATION } from '../../../services/serverCertificate';

interface RotateCaDangerZoneProps {
  onRotate: (confirm: string) => void;
  busy?: boolean;
  disabled?: boolean;
}

const RotateCaDangerZone: React.FC<RotateCaDangerZoneProps> = ({
  onRotate,
  busy = false,
  disabled = false,
}) => {
  const { t } = useTranslation('admin');
  const [confirm, setConfirm] = useState('');

  return (
    <Accordion>
      <AccordionSummary expandIcon={<ExpandMoreIcon />}>
        <Typography variant="subtitle2">
          {t('serverCertificate.rotateCa.section') as string}
        </Typography>
      </AccordionSummary>
      <AccordionDetails>
        <Paper variant="outlined" sx={{ p: 3, borderColor: 'error.main' }}>
          <Typography variant="subtitle1" color="error" gutterBottom>
            {t('serverCertificate.rotateCa.title') as string}
          </Typography>

          <Alert severity="error" sx={{ mb: 2 }}>
            {t('serverCertificate.rotateCa.warning') as string}
          </Alert>

          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {
              t('serverCertificate.rotateCa.instruction', {
                phrase: ROTATE_CA_CONFIRMATION,
              }) as string
            }
          </Typography>

          <Stack direction="row" spacing={1} alignItems="center">
            <TextField
              size="small"
              value={confirm}
              disabled={disabled || busy}
              onChange={(e) => setConfirm(e.target.value)}
              placeholder={ROTATE_CA_CONFIRMATION}
              sx={{ maxWidth: 260 }}
            />
            <Button
              variant="contained"
              color="error"
              disabled={disabled || busy || confirm !== ROTATE_CA_CONFIRMATION}
              onClick={() => {
                onRotate(confirm);
                setConfirm('');
              }}
            >
              {t('serverCertificate.rotateCa.action') as string}
            </Button>
          </Stack>
        </Paper>
      </AccordionDetails>
    </Accordion>
  );
};

export default RotateCaDangerZone;
