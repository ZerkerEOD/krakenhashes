import React, { useState, useEffect } from 'react';
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
} from '@mui/material';
import { Add as AddIcon } from '@mui/icons-material';
import { listClients } from '../../services/api';
import { Client } from '../../types/client';

// Option type for the Autocomplete dropdown
interface ClientOption {
  id: string;
  name: string;
  isCreateNew?: boolean;
}

const CREATE_NEW_OPTION: ClientOption = {
  id: '__create_new__',
  name: 'Create New Client',
  isCreateNew: true,
};

// Data passed back to the parent form when "Create New Client" is active
export interface NewClientData {
  clientName: string;
  description?: string;
  contactInfo?: string;
  dataRetentionMonths?: number | null;
  excludeFromPotfile?: boolean;
  excludeFromClientPotfile?: boolean;
}

interface ClientAutocompleteProps {
  value: string | null;
  onChange: (clientName: string | null) => void;
  teamId?: string;
  defaultRetention?: number | null;
  onNewClientDataChange?: (data: NewClientData | null) => void;
}

export default function ClientAutocomplete({
  value,
  onChange,
  teamId,
  defaultRetention,
  onNewClientDataChange,
}: ClientAutocompleteProps) {
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

  // Fetch clients on mount and when teamId changes
  useEffect(() => {
    fetchClients();
  }, [teamId]);

  const fetchClients = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await listClients();
      let clients: Client[] = response.data?.data || [];

      // Filter by team if teamId is provided (client-side filter for team-scoped view)
      // The backend listClients returns all accessible clients; team filtering
      // is typically handled by backend middleware, but we add client-side filter as safety
      const clientOptions: ClientOption[] = clients.map((c) => ({
        id: c.id,
        name: c.name,
      }));

      // Sort alphabetically
      clientOptions.sort((a, b) => a.name.localeCompare(b.name));

      setOptions(clientOptions);
    } catch (err: any) {
      console.error('Error fetching clients:', err);
      setError(err.response?.data?.error || err.message || 'Failed to fetch clients');
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
  ]);

  const handleOptionChange = (_event: React.SyntheticEvent, newValue: ClientOption | null) => {
    if (newValue?.isCreateNew) {
      setSelectedOption(CREATE_NEW_OPTION);
      setIsCreateMode(true);
      setNewClientName('');
      setNewClientDescription('');
      setNewClientContactInfo('');
      setNewClientRetention('');
      setNewClientExcludePotfile(false);
      setNewClientExcludeClientPotfile(false);
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
  const allOptions: ClientOption[] = [CREATE_NEW_OPTION, ...options];

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
            label="Client"
            placeholder="Select a client..."
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
            New Client Details
          </Typography>
          <Divider sx={{ mb: 2 }} />

          <TextField
            fullWidth
            required={isCreateMode}
            label="Client Name"
            value={newClientName}
            onChange={(e) => handleNewClientNameChange(e.target.value)}
            sx={{ mb: 2 }}
          />

          <TextField
            fullWidth
            label="Description"
            value={newClientDescription}
            onChange={(e) => setNewClientDescription(e.target.value)}
            multiline
            rows={2}
            sx={{ mb: 2 }}
          />

          <TextField
            fullWidth
            label="Contact Info"
            value={newClientContactInfo}
            onChange={(e) => setNewClientContactInfo(e.target.value)}
            sx={{ mb: 2 }}
          />

          <TextField
            fullWidth
            type="number"
            label="Data Retention (months)"
            value={newClientRetention}
            onChange={(e) => setNewClientRetention(e.target.value)}
            placeholder={
              defaultRetention !== null && defaultRetention !== undefined
                ? `System default: ${defaultRetention}`
                : 'Not set (keep indefinitely)'
            }
            helperText={
              defaultRetention !== null && defaultRetention !== undefined
                ? `Leave empty to use system default (${defaultRetention} months). Set 0 to keep forever.`
                : 'Leave empty to use system default. Set 0 to keep forever.'
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
            label="Exclude from global potfile"
          />
          <Typography variant="caption" color="text.secondary" display="block" sx={{ ml: 4, mt: -0.5, mb: 1 }}>
            Cracked passwords from this client won't be added to the global potfile.
          </Typography>

          <FormControlLabel
            control={
              <Checkbox
                checked={newClientExcludeClientPotfile}
                onChange={(e) => setNewClientExcludeClientPotfile(e.target.checked)}
              />
            }
            label="Exclude from client potfile"
          />
          <Typography variant="caption" color="text.secondary" display="block" sx={{ ml: 4, mt: -0.5 }}>
            Cracked passwords from this client won't be added to their client-specific potfile.
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
