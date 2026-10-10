import React, { useEffect, useState } from 'react';
import { Box, IconButton, InputAdornment, Stack, TextField, Typography } from '@mui/material';
import SearchIcon from '@mui/icons-material/Search';
import ClearIcon from '@mui/icons-material/Clear';
import { useTranslation } from 'react-i18next';
import useDebounce from '../../../hooks/useDebounce';
import type { DataTableToolbarProps } from './types';

const SearchBox: React.FC<NonNullable<DataTableToolbarProps['search']>> = ({
  value,
  onChange,
  placeholder,
  debounceMs = 400,
  autoFocus,
}) => {
  const { t } = useTranslation('common');
  const [draft, setDraft] = useState(value);
  const debounced = useDebounce(draft, debounceMs);

  // Push debounced edits up; accept external resets.
  useEffect(() => {
    if (debounced !== value) onChange(debounced);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced]);
  useEffect(() => {
    if (value !== draft && value !== debounced) setDraft(value);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  return (
    <TextField
      size="small"
      value={draft}
      autoFocus={autoFocus}
      onChange={(e) => setDraft(e.target.value)}
      placeholder={placeholder ?? (t('buttons.search') as string)}
      inputProps={{ 'aria-label': placeholder ?? (t('buttons.search') as string) }}
      sx={{ minWidth: 240 }}
      InputProps={{
        startAdornment: (
          <InputAdornment position="start">
            <SearchIcon fontSize="small" />
          </InputAdornment>
        ),
        endAdornment: draft ? (
          <InputAdornment position="end">
            <IconButton size="small" aria-label={t('table.clearSearch') as string} onClick={() => setDraft('')}>
              <ClearIcon fontSize="small" />
            </IconButton>
          </InputAdornment>
        ) : undefined,
      }}
    />
  );
};

const DataTableToolbar: React.FC<DataTableToolbarProps> = ({ title, subtitle, search, filters, actions }) => {
  if (!title && !subtitle && !search && !filters && !actions) return null;
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        flexWrap: 'wrap',
        px: 2,
        py: 1.5,
        borderBottom: 1,
        borderColor: 'divider',
      }}
    >
      {(title || subtitle) && (
        <Box sx={{ mr: 1, minWidth: 0 }}>
          {title && (
            <Typography variant="h6" component="h3" noWrap>
              {title}
            </Typography>
          )}
          {subtitle && (
            <Typography variant="caption" color="text.secondary" noWrap component="div">
              {subtitle}
            </Typography>
          )}
        </Box>
      )}
      {search && <SearchBox {...search} />}
      {filters && (
        <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
          {filters}
        </Stack>
      )}
      <Box sx={{ flexGrow: 1 }} />
      {actions && (
        <Stack direction="row" spacing={1} alignItems="center">
          {actions}
        </Stack>
      )}
    </Box>
  );
};

export default DataTableToolbar;
