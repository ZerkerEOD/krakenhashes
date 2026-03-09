import React, { useEffect, useState } from 'react';
import {
  Box,
  Grid,
  TextField,
  Typography,
  Autocomplete,
  Chip,
} from '@mui/material';
import { CustomCharset } from '../../types/customCharsets';
import { calculateMaskKeyspace, formatKeyspace, resolveCharsetSize } from '../../utils/charsetUtils';

interface CharsetInputsProps {
  customCharsets: Record<string, string>;
  onChange: (charsets: Record<string, string>) => void;
  mask: string;
  savedCharsets?: CustomCharset[];
}

const SLOTS = ['1', '2', '3', '4'] as const;

const CharsetInputs: React.FC<CharsetInputsProps> = ({
  customCharsets,
  onChange,
  mask,
  savedCharsets = [],
}) => {
  const [keyspaceEstimate, setKeyspaceEstimate] = useState<number>(0);
  const [charsetSizes, setCharsetSizes] = useState<Record<string, number>>({});

  useEffect(() => {
    // Resolve sizes for display
    const resolved: Record<string, number> = {};
    for (const slot of SLOTS) {
      const def = customCharsets[slot];
      if (def) {
        resolved[slot] = resolveCharsetSize(def, customCharsets, resolved);
      }
    }
    setCharsetSizes(resolved);

    // Calculate keyspace estimate
    if (mask) {
      setKeyspaceEstimate(calculateMaskKeyspace(mask, customCharsets));
    } else {
      setKeyspaceEstimate(0);
    }
  }, [customCharsets, mask]);

  const handleCharsetChange = (slot: string, value: string) => {
    const updated = { ...customCharsets };
    if (value) {
      updated[slot] = value;
    } else {
      delete updated[slot];
    }
    onChange(updated);
  };

  const handleSavedCharsetSelect = (slot: string, charset: CustomCharset | null) => {
    handleCharsetChange(slot, charset?.definition || '');
  };

  // Check if mask references any custom charsets
  const usedSlots = new Set<string>();
  if (mask) {
    for (const slot of SLOTS) {
      if (mask.includes(`?${slot}`)) {
        usedSlots.add(slot);
      }
    }
  }

  return (
    <Box>
      <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1 }}>
        Custom Charsets (optional - define ?1 through ?4 for use in mask)
      </Typography>
      <Grid container spacing={2}>
        {SLOTS.map((slot) => (
          <Grid item xs={12} sm={6} key={slot}>
            <Box sx={{ display: 'flex', gap: 1, alignItems: 'flex-start' }}>
              <TextField
                label={`Charset ${slot} (-${slot})`}
                value={customCharsets[slot] || ''}
                onChange={(e) => handleCharsetChange(slot, e.target.value)}
                fullWidth
                size="small"
                placeholder="e.g., ?u?d or abcdef0123456789"
                helperText={
                  charsetSizes[slot]
                    ? `${charsetSizes[slot]} chars${usedSlots.has(slot) ? '' : ' (not referenced in mask)'}`
                    : usedSlots.has(slot) ? 'Referenced in mask but not defined' : undefined
                }
                error={usedSlots.has(slot) && !customCharsets[slot]}
                sx={{ flex: 1 }}
              />
              {savedCharsets.length > 0 && (
                <Autocomplete
                  size="small"
                  options={savedCharsets}
                  getOptionLabel={(option) => option.name}
                  onChange={(_, value) => handleSavedCharsetSelect(slot, value)}
                  renderInput={(params) => (
                    <TextField {...params} label="Saved" size="small" />
                  )}
                  renderOption={(props, option) => (
                    <li {...props}>
                      <Box>
                        <Typography variant="body2">{option.name}</Typography>
                        <Typography variant="caption" color="text.secondary">
                          {option.definition}
                        </Typography>
                      </Box>
                    </li>
                  )}
                  sx={{ minWidth: 140 }}
                />
              )}
            </Box>
          </Grid>
        ))}
      </Grid>

      {/* Keyspace preview */}
      {mask && keyspaceEstimate > 0 && (
        <Box sx={{ mt: 1, display: 'flex', flexWrap: 'wrap', gap: 1, alignItems: 'center' }}>
          <Chip
            label={`Estimated keyspace: ${formatKeyspace(keyspaceEstimate)}`}
            color="info"
            size="small"
            variant="outlined"
          />
          {Object.entries(charsetSizes).map(([slot, size]) => (
            <Chip
              key={slot}
              label={`?${slot} = ${size} chars`}
              size="small"
              variant="outlined"
            />
          ))}
        </Box>
      )}
    </Box>
  );
};

export default CharsetInputs;
