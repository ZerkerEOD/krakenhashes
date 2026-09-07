import React, { useState } from 'react';
import { Box, Button, Chip, Stack, TextField, Typography } from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import LockIcon from '@mui/icons-material/Lock';
import { useTranslation } from 'react-i18next';
import { SanKind } from '../../../types/serverCertificate';
import { SanRejectionReason, validateSan } from '../../../utils/sanValidation';

interface SanListEditorProps {
  kind: SanKind;
  values: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
  /**
   * Entries that are always present and cannot be removed. Rendered without a
   * delete affordance rather than hidden, so an admin can see they are covered
   * and does not try to add them again.
   */
  locked?: string[];
}

const SanListEditor: React.FC<SanListEditorProps> = ({
  kind,
  values,
  onChange,
  disabled = false,
  locked = [],
}) => {
  const { t } = useTranslation('admin');
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<SanRejectionReason | null>(null);

  const handleDraftChange = (next: string) => {
    setDraft(next);
    if (!next.trim()) {
      setError(null);
      return;
    }
    const result = validateSan(kind, next, [...values, ...locked]);
    setError(result.ok ? null : result.reason);
  };

  const handleAdd = () => {
    const result = validateSan(kind, draft, [...values, ...locked]);
    if (!result.ok) {
      setError(result.reason);
      return;
    }
    onChange([...values, result.value]);
    setDraft('');
    setError(null);
  };

  const handleDelete = (value: string) => {
    onChange(values.filter((v) => v !== value));
  };

  const canAdd = draft.trim().length > 0 && error === null;

  return (
    <Box>
      <Typography variant="subtitle2" gutterBottom>
        {t(`serverCertificate.sans.${kind}.title`) as string}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t(`serverCertificate.sans.${kind}.help`) as string}
      </Typography>

      <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', gap: 1, mb: 2 }}>
        {locked.map((value) => (
          <Chip
            key={`locked-${value}`}
            label={value}
            size="small"
            icon={<LockIcon fontSize="small" />}
            variant="outlined"
            title={t('serverCertificate.sans.lockedTooltip') as string}
          />
        ))}
        {values.map((value) => (
          <Chip
            key={value}
            label={value}
            size="small"
            color="primary"
            variant="outlined"
            onDelete={disabled ? undefined : () => handleDelete(value)}
          />
        ))}
        {values.length === 0 && locked.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            {t('serverCertificate.sans.empty') as string}
          </Typography>
        )}
      </Stack>

      <Stack direction="row" spacing={1} alignItems="flex-start">
        <TextField
          size="small"
          fullWidth
          disabled={disabled}
          value={draft}
          onChange={(e) => handleDraftChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && canAdd) {
              e.preventDefault();
              handleAdd();
            }
          }}
          placeholder={t(`serverCertificate.sans.${kind}.placeholder`) as string}
          error={error !== null}
          helperText={
            error
              ? (t(`serverCertificate.validation.${error}`, { address: draft.trim() }) as string)
              : ' '
          }
        />
        <Button
          variant="outlined"
          startIcon={<AddIcon />}
          disabled={disabled || !canAdd}
          onClick={handleAdd}
          sx={{ mt: 0.25 }}
        >
          {t('serverCertificate.sans.add') as string}
        </Button>
      </Stack>
    </Box>
  );
};

export default SanListEditor;
