import React, { useState, useEffect, useRef } from 'react';
import {
  TextField,
  CircularProgress,
  Alert,
  Box,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  Paper,
  Popper,
  ClickAwayListener
} from '@mui/material';
import { api } from '../../services/api';
import { Client } from '../../types/client';
import useDebounce from '../../hooks/useDebounce';
import { AxiosError } from 'axios';

// Define the Option type for Autocomplete
type ClientOption = Pick<Client, 'id' | 'name'>; // Only need id and name for options

export default function ClientAutocomplete({
  value, // Represents the client *name*
  onChange, // Expects to receive the client *name*
  teamId, // Optional: filter clients by team
}: {
  value: string | null;
  onChange: (value: string | null) => void;
  teamId?: string;
}) {
  const [inputValue, setInputValue] = useState(value || '');
  const [options, setOptions] = useState<ClientOption[]>([]);
  const [searchLoading, setSearchLoading] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [showSuggestions, setShowSuggestions] = useState(false);
  const justSelectedRef = useRef(false); // Track if user just selected from dropdown
  const anchorEl = useRef<HTMLDivElement>(null); // Anchor for the Popper

  const debouncedSearch = useDebounce(inputValue, 300);

  // Effect to fetch suggestions
  useEffect(() => {
    // Don't search if user just selected an option
    if (justSelectedRef.current) {
      justSelectedRef.current = false;
      return;
    }

    if (debouncedSearch) {
      setSearchLoading(true);
      setSearchError(null);
      const searchUrl = teamId
        ? `/api/clients/search?q=${debouncedSearch}&team_id=${teamId}`
        : `/api/clients/search?q=${debouncedSearch}`;
      api.get<Client[]>(searchUrl)
        .then((res) => {
          const fetchedOptions = Array.isArray(res.data) ? res.data.map(c => ({ id: c.id, name: c.name })) : [];
          setOptions(fetchedOptions);
          setShowSuggestions(fetchedOptions.length > 0); // Show suggestions only if results exist
          console.log('[API Success] Setting options:', fetchedOptions);
          console.log('[API Success] Setting showSuggestions to:', fetchedOptions.length > 0);
        })
        .catch((err: AxiosError) => {
          console.error("Error searching clients:", err);
          setSearchError((err.response?.data as any)?.error || err.message || 'Failed to fetch clients');
          setOptions([]);
          setShowSuggestions(false);
        })
        .finally(() => {
          setSearchLoading(false);
        });
    } else {
      setOptions([]);
      setShowSuggestions(false);
      setSearchError(null);
    }
  }, [debouncedSearch]);

  // Reset options when teamId changes (clients are team-scoped)
  useEffect(() => {
    setOptions([]);
    setShowSuggestions(false);
    setSearchError(null);
  }, [teamId]);

  // Update internal state if controlled value changes
  useEffect(() => {
    if (value !== inputValue) { // Avoid infinite loop
        setInputValue(value || '');
    }
  }, [value]);

  const handleInputChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = event.target.value;
    setInputValue(newValue);
    onChange(newValue.trim() || null); // Update form state as user types
    setShowSuggestions(true); // Try to show suggestions while typing
  };

  const handleSuggestionClick = (option: ClientOption) => {
    justSelectedRef.current = true; // Mark that user just selected an option
    setInputValue(option.name); // Update text field
    onChange(option.name); // Update form state with selected name
    setShowSuggestions(false); // Close suggestions immediately
    setOptions([]); // Clear options after selection
  };

  const handleCloseSuggestions = () => {
    console.log('[handleCloseSuggestions] Setting showSuggestions to false');
    setShowSuggestions(false);
  };

  // ---> ADD LOGGING <---
  console.log('[Render] showSuggestions:', showSuggestions);
  console.log('[Render] options:', options);
  // ---> ADD ANCHOR LOGGING <---
  if (showSuggestions && options.length > 0) {
    console.log('[Render] Anchor Width:', anchorEl.current?.clientWidth);
  }
  // ---> END LOGGING <---

  return (
    <Box sx={{ position: 'relative', my: 2 }} ref={anchorEl}>
        <TextField
            fullWidth // Make TextField take full width
            label="Client Name"
            placeholder="Search or type client name..."
            value={inputValue}
            onChange={handleInputChange}
            onFocus={() => { if (options.length > 0) setShowSuggestions(true); }} // Show suggestions on focus if available
            InputProps={{
              endAdornment: (
                <>
                  {searchLoading && <CircularProgress color="inherit" size={20} />}
                </>
              ),
            }}
        />
        <Popper
            open={showSuggestions && options.length > 0}
            anchorEl={anchorEl.current}
            placement="bottom-start"
            style={{ zIndex: 1400 }} // Ensure it's above Dialog (often ~1300)
            modifiers={[
                {
                    name: 'offset',
                    options: {
                        offset: [0, 8], // Offset below the text field
                    },
                },
                {
                     name: 'preventOverflow',
                     options: {
                         boundary: 'clippingParents',
                     },
                },
            ]}
        >
             <ClickAwayListener onClickAway={handleCloseSuggestions}>
                <Paper elevation={3} sx={{ width: anchorEl.current?.clientWidth, maxHeight: 200, overflow: 'auto' }}>
                    <List dense>
                        {options.map((option) => (
                            <ListItem key={option.id} disablePadding>
                                <ListItemButton onClick={() => handleSuggestionClick(option)}>
                                    <ListItemText primary={option.name} />
                                </ListItemButton>
                            </ListItem>
                        ))}
                    </List>
                </Paper>
            </ClickAwayListener>
        </Popper>

        {searchError && (
            <Alert severity="error" sx={{ mt: 1 }}>
                {searchError}
            </Alert>
        )}
    </Box>
  );
}