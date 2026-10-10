import React, { useState, useEffect, useMemo } from 'react';
import {
  TextField,
  CircularProgress,
  Alert,
  Box,
  Autocomplete,
  Typography,
  FormControlLabel,
  Checkbox,
  Collapse,
  Divider,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
} from '@mui/material';
import { Add as AddIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { listClients } from '../../services/api';
import { Client } from '../../types/client';
import { Team } from '../../types/team';

// Option type for the Autocomplete dropdown
interface ClientOption {
  id: string;
  name: string;
  isCreateNew?: boolean;
}

const CREATE_NEW_OPTION_ID = '__create_new__';

// Data passed back to the parent form when "Create New Client" is active
export interface NewClientData {
  clientName: string;
  description?: string;
  contactInfo?: string;
  dataRetentionMonths?: number | null;
  excludeFromPotfile?: boolean;
  excludeFromClientPotfile?: boolean;
  // Team to file the new client under. Only meaningful when the user belongs
  // to more than one team (existing clients derive their team from client_teams).
  teamId?: string;
}

interface ClientAutocompleteProps {
  value: string | null;
  onChange: (clientName: string | null) => void;
  // The user's teams. When creating a NEW client and the user belongs to more
  // than one team, a required Team picker is shown so the client is filed under
  // the intended team. Not used for selecting existing clients — the client list
  // is already scoped to the user's teams server-side.
  teams?: Team[];
  defaultRetention?: number | null;
  onNewClientDataChange?: (data: NewClientData | null) => void;
}

export default function ClientAutocomplete({
  value,
  onChange,
  teams,
  defaultRetention,
  onNewClientDataChange,
}: ClientAutocompleteProps) {
  const { t } = useTranslation('hashlists');
  const createNewOption: ClientOption = useMemo(() => ({
    id: CREATE_NEW_OPTION_ID,
    name: t('clientAutocomplete.createNewClient') as string,
    isCreateNew: true,
  }), [t]);
  const [options, setOptions] = useState<ClientOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedOption, setSelectedOption] = useState<ClientOption | null>(null);
  const [isCreateMode, setIsCreateMode] = useState(false);

  // New client form state
  const [newClientName, setNewClientName] = useState('');
  const [newClientDescription, setNewClientDescription] = useState('');
  const [newClientContactInfo, setNewClientContactInfo] = useState('');
  const [newClientRetention, setNewClientRetention] = useState<string>('');
  const [newClientExcludePotfile, setNewClientExcludePotfile] = useState(false);
  const [newClientExcludeClientPotfile, setNewClientExcludeClientPotfile] = useState(false);
  const [newClientTeamId, setNewClientTeamId] = useState<string>('');

  // Fetch clients on mount. The list is already scoped to the user's teams
  // server-side (GET /api/clients), so no client-side team filtering is needed.
  useEffect(() => {
    fetchClients();
  }, []);

  const fetchClients = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await listClients();
      let clients: Client[] = response.data?.data || [];

      const clientOptions: ClientOption[] = clients.map((c) => ({
        id: c.id,
        name: c.name,
      }));

      // Sort alphabetically
      clientOptions.sort((a, b) => a.name.localeCompare(b.name));

      setOptions(clientOptions);
    } catch (err: any) {
      console.error('Error fetching clients:', err);
      setError(err.response?.data?.error || err.message || (t('clientAutocomplete.errors.fetchFailed') as string));
      setOptions([]);
    } finally {
      setLoading(false);
    }
  };

  // Sync selected option with external value
  useEffect(() => {
    if (!value) {
      if (!isCreateMode) {
        setSelectedOption(null);
      }
      return;
    }
    // Find matching option
    const match = options.find((o) => o.name === value);
    if (match) {
      setSelectedOption(match);
      setIsCreateMode(false);
    }
  }, [value, options]);

  // Notify parent of new client data changes
  useEffect(() => {
    if (isCreateMode && newClientName.trim()) {
      const retentionValue = newClientRetention.trim() !== ''
        ? parseInt(newClientRetention, 10)
        : undefined;

      onNewClientDataChange?.({
        clientName: newClientName.trim(),
        description: newClientDescription.trim() || undefined,
        contactInfo: newClientContactInfo.trim() || undefined,
        dataRetentionMonths: retentionValue !== undefined && !isNaN(retentionValue)
          ? retentionValue
          : undefined,
        excludeFromPotfile: newClientExcludePotfile,
        excludeFromClientPotfile: newClientExcludeClientPotfile,
        teamId: newClientTeamId || undefined,
      });
    } else if (isCreateMode) {
      onNewClientDataChange?.(null);
    }
  }, [
    isCreateMode,
    newClientName,
    newClientDescription,
    newClientContactInfo,
    newClientRetention,
    newClientExcludePotfile,
    newClientExcludeClientPotfile,
    newClientTeamId,
  ]);

  const handleOptionChange = (_event: React.SyntheticEvent, newValue: ClientOption | null) => {
    if (newValue?.isCreateNew) {
      setSelectedOption(createNewOption);
      setIsCreateMode(true);
      setNewClientName('');
      setNewClientDescription('');
      setNewClientContactInfo('');
      setNewClientRetention('');
      setNewClientExcludePotfile(false);
      setNewClientExcludeClientPotfile(false);
      // Default to the sole team when the user has exactly one; otherwise force
      // an explicit choice (empty) so a multi-team user can't silently misfile.
      setNewClientTeamId(teams && teams.length === 1 ? teams[0].id : '');
      onChange(null); // Clear until user types a name
    } else {
      setSelectedOption(newValue);
      setIsCreateMode(false);
      onChange(newValue ? newValue.name : null);
      onNewClientDataChange?.(null);
    }
  };

  const handleNewClientNameChange = (name: string) => {
    setNewClientName(name);
    onChange(name.trim() || null);
  };

  // Build options list with "Create New Client" at top
  const allOptions: ClientOption[] = [createNewOption, ...options];

  return (
    <Box sx={{ my: 2 }}>
      <Autocomplete
        options={allOptions}
        getOptionLabel={(option) => option.name}
        value={selectedOption}
        onChange={handleOptionChange}
        loading={loading}
        isOptionEqualToValue={(option, val) => option.id === val.id}
        renderOption={(props, option) => {
          if (option.isCreateNew) {
            return (
              <Box component="li" {...props} key={option.id}>
                <AddIcon sx={{ mr: 1, color: 'primary.main' }} />
                <Typography color="primary.main" fontWeight="medium">
                  {option.name}
                </Typography>
              </Box>
            );
          }
          return (
            <Box component="li" {...props} key={option.id}>
              <Typography>{option.name}</Typography>
            </Box>
          );
        }}
        renderInput={(params) => (
          <TextField
            {...params}
            label={t('clientAutocomplete.label') as string}
            placeholder={t('clientAutocomplete.placeholder') as string}
            InputProps={{
              ...params.InputProps,
              endAdornment: (
                <>
                  {loading && <CircularProgress color="inherit" size={20} />}
                  {params.InputProps.endAdornment}
                </>
              ),
            }}
          />
        )}
      />

      {/* Create New Client inline form */}
      <Collapse in={isCreateMode}>
        <Box sx={{ mt: 2, p: 2, border: 1, borderColor: 'divider', borderRadius: 1 }}>
          <Typography variant="subtitle2" gutterBottom>
            {t('clientAutocomplete.newClientDetails') as string}
          </Typography>
          <Divider sx={{ mb: 2 }} />

          {/* Team picker — only needed when the user belongs to more than one
              team, so the new client is filed under the intended team. */}
          {teams && teams.length > 1 && (
            <FormControl fullWidth required={isCreateMode} sx={{ mb: 2 }}>
              <InputLabel id="new-client-team-label">{t('clientAutocomplete.team') as string}</InputLabel>
              <Select
                labelId="new-client-team-label"
                value={newClientTeamId}
                label={t('clientAutocomplete.team') as string}
                onChange={(e) => setNewClientTeamId(e.target.value)}
              >
                {teams.map((team) => (
                  <MenuItem key={team.id} value={team.id}>
                    {team.name}
                  </MenuItem>
                ))}
              </Select>
              <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, ml: 1.75 }}>
                {t('clientAutocomplete.teamHelper') as string}
              </Typography>
            </FormControl>
          )}

          <TextField
            fullWidth
            required={isCreateMode}
            label={t('clientAutocomplete.clientName') as string}
            value={newClientName}
            onChange={(e) => handleNewClientNameChange(e.target.value)}
            sx={{ mb: 2 }}
          />

          <TextField
            fullWidth
            label={t('clientAutocomplete.description') as string}
            value={newClientDescription}
            onChange={(e) => setNewClientDescription(e.target.value)}
            multiline
            rows={2}
            sx={{ mb: 2 }}
          />

          <TextField
            fullWidth
            label={t('clientAutocomplete.contactInfo') as string}
            value={newClientContactInfo}
            onChange={(e) => setNewClientContactInfo(e.target.value)}
            sx={{ mb: 2 }}
          />

          <TextField
            fullWidth
            type="number"
            label={t('clientAutocomplete.dataRetention') as string}
            value={newClientRetention}
            onChange={(e) => setNewClientRetention(e.target.value)}
            placeholder={
              defaultRetention !== null && defaultRetention !== undefined
                ? (t('clientAutocomplete.dataRetentionPlaceholderDefault', { months: defaultRetention }) as string)
                : (t('clientAutocomplete.dataRetentionPlaceholderUnset') as string)
            }
            helperText={
              defaultRetention !== null && defaultRetention !== undefined
                ? (t('clientAutocomplete.dataRetentionHelperDefault', { months: defaultRetention }) as string)
                : (t('clientAutocomplete.dataRetentionHelperUnset') as string)
            }
            InputProps={{ inputProps: { min: 0 } }}
            sx={{ mb: 2 }}
          />

          <FormControlLabel
            control={
              <Checkbox
                checked={newClientExcludePotfile}
                onChange={(e) => setNewClientExcludePotfile(e.target.checked)}
              />
            }
            label={t('clientAutocomplete.excludeFromGlobalPotfile') as string}
          />
          <Typography variant="caption" color="text.secondary" display="block" sx={{ ml: 4, mt: -0.5, mb: 1 }}>
            {t('clientAutocomplete.excludeFromGlobalPotfileHelper') as string}
          </Typography>

          <FormControlLabel
            control={
              <Checkbox
                checked={newClientExcludeClientPotfile}
                onChange={(e) => setNewClientExcludeClientPotfile(e.target.checked)}
              />
            }
            label={t('clientAutocomplete.excludeFromClientPotfile') as string}
          />
          <Typography variant="caption" color="text.secondary" display="block" sx={{ ml: 4, mt: -0.5 }}>
            {t('clientAutocomplete.excludeFromClientPotfileHelper') as string}
          </Typography>
        </Box>
      </Collapse>

      {error && (
        <Alert severity="error" sx={{ mt: 1 }}>
          {error}
        </Alert>
      )}
    </Box>
  );
}
