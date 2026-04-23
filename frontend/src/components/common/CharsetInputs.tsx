import React, { useEffect, useState } from 'react';
import {
  Box,
  Grid,
  TextField,
  Typography,
  Autocomplete,
  Chip,
  IconButton,
  FormControlLabel,
  Switch,
  Tooltip,
} from '@mui/material';
import { Clear as ClearIcon } from '@mui/icons-material';
import { CustomCharset } from '../../types/customCharsets';
import { calculateMaskKeyspace, formatKeyspace, resolveCharsetSize, validateHexCharsetDefinition } from '../../utils/charsetUtils';

interface CharsetInputsProps {
  customCharsets: Record<string, string>;
  charsetFileIds?: Record<string, string>;
  onChange: (charsets: Record<string, string>, fileIds?: Record<string, string>) => void;
  mask: string;
  savedCharsets?: CustomCharset[];
  hexCharset?: boolean;
  onHexCharsetChange?: (hex: boolean) => void;
}

const SLOTS = ['1', '2', '3', '4'] as const;

const CharsetInputs: React.FC<CharsetInputsProps> = ({
  customCharsets,
  charsetFileIds = {},
  onChange,
  mask,
  savedCharsets = [],
  hexCharset = false,
  onHexCharsetChange,
}) => {
  const [keyspaceEstimate, setKeyspaceEstimate] = useState<number>(0);
  const [charsetSizes, setCharsetSizes] = useState<Record<string, number>>({});

  // Build a map of file charset byte counts for keyspace resolution
  const fileCharsetByteCounts: Record<string, number> = {};
  for (const slot of SLOTS) {
    if (charsetFileIds[slot]) {
      const charset = savedCharsets.find(c => c.id === charsetFileIds[slot]);
      if (charset?.byte_count) {
        fileCharsetByteCounts[slot] = charset.byte_count;
      }
    }
  }

  useEffect(() => {
    // Resolve sizes for display
    const resolved: Record<string, number> = {};
    for (const slot of SLOTS) {
      if (fileCharsetByteCounts[slot]) {
        resolved[slot] = fileCharsetByteCounts[slot];
      } else {
        const def = customCharsets[slot];
        if (def) {
          resolved[slot] = resolveCharsetSize(def, customCharsets, resolved, hexCharset);
        }
      }
    }
    setCharsetSizes(resolved);

    // Calculate keyspace estimate
    if (mask) {
      setKeyspaceEstimate(calculateMaskKeyspace(mask, customCharsets, fileCharsetByteCounts, hexCharset));
    } else {
      setKeyspaceEstimate(0);
    }
  }, [customCharsets, charsetFileIds, mask, savedCharsets, hexCharset]);

  const handleCharsetChange = (slot: string, value: string) => {
    const updated = { ...customCharsets };
    const updatedFiles = { ...charsetFileIds };
    // Clear file charset if user types inline
    delete updatedFiles[slot];
    if (value) {
      updated[slot] = value;
    } else {
      delete updated[slot];
    }
    onChange(updated, updatedFiles);
  };

  const handleSavedCharsetSelect = (slot: string, charset: CustomCharset | null) => {
    if (!charset) {
      // Cleared
      const updated = { ...customCharsets };
      const updatedFiles = { ...charsetFileIds };
      delete updated[slot];
      delete updatedFiles[slot];
      onChange(updated, updatedFiles);
      return;
    }

    if (charset.charset_type === 'file') {
      // File charset — store ID, clear inline definition for this slot
      const updated = { ...customCharsets };
      const updatedFiles = { ...charsetFileIds };
      delete updated[slot];
      updatedFiles[slot] = charset.id;
      onChange(updated, updatedFiles);
    } else {
      // Inline charset — use definition directly
      const updated = { ...customCharsets };
      const updatedFiles = { ...charsetFileIds };
      delete updatedFiles[slot];
      updated[slot] = charset.definition || '';
      onChange(updated, updatedFiles);

      // Auto-toggle hex mode to match the selected charset
      if (onHexCharsetChange && charset.is_hex !== hexCharset) {
        onHexCharsetChange(!!charset.is_hex);
      }
    }
  };

  const handleClearFileCharset = (slot: string) => {
    const updatedFiles = { ...charsetFileIds };
    delete updatedFiles[slot];
    onChange(customCharsets, updatedFiles);
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

  // Find selected file charset for a slot
  const getFileCharset = (slot: string): CustomCharset | undefined => {
    const fileId = charsetFileIds[slot];
    if (!fileId) return undefined;
    return savedCharsets.find(c => c.id === fileId);
  };

  // Group saved charsets: compatible first (matching hex mode + file charsets), then others
  // All charsets are shown so users can discover hex charsets without manually toggling
  const sortedSavedCharsets = [...savedCharsets].sort((a, b) => {
    const aCompat = a.charset_type === 'file' || a.is_hex === hexCharset;
    const bCompat = b.charset_type === 'file' || b.is_hex === hexCharset;
    if (aCompat && !bCompat) return -1;
    if (!aCompat && bCompat) return 1;
    return 0;
  });

  // Validate inline charset for hex mode
  const getInlineError = (slot: string): string | undefined => {
    if (!hexCharset) return undefined;
    const def = customCharsets[slot];
    if (!def) return undefined;
    const err = validateHexCharsetDefinition(def);
    return err || undefined;
  };

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
        <Typography variant="subtitle2" color="text.secondary">
          Custom Charsets (optional - define ?1 through ?4 for use in mask)
        </Typography>
        {onHexCharsetChange && (
          <Tooltip title="When enabled, inline charset definitions are interpreted as hex byte pairs (e.g., 41424344 = ABCD). File charsets are unaffected.">
            <FormControlLabel
              control={
                <Switch
                  size="small"
                  checked={hexCharset}
                  onChange={(e) => onHexCharsetChange(e.target.checked)}
                />
              }
              label={<Typography variant="caption">Hex-encoded charsets</Typography>}
              sx={{ ml: 1, mr: 0 }}
            />
          </Tooltip>
        )}
      </Box>
      <Grid container spacing={2}>
        {SLOTS.map((slot) => {
          const fileCharset = getFileCharset(slot);
          const isFileSlot = !!fileCharset;

          return (
            <Grid item xs={12} sm={6} key={slot}>
              <Box sx={{ display: 'flex', gap: 1, alignItems: 'flex-start' }}>
                {isFileSlot ? (
                  // File charset selected — show read-only display
                  <Box sx={{ flex: 1 }}>
                    <TextField
                      label={`Charset ${slot} (-${slot})`}
                      value={`[File: ${fileCharset.name} — ${fileCharset.byte_count} bytes]`}
                      fullWidth
                      size="small"
                      InputProps={{
                        readOnly: true,
                        endAdornment: (
                          <IconButton size="small" onClick={() => handleClearFileCharset(slot)}>
                            <ClearIcon fontSize="small" />
                          </IconButton>
                        ),
                      }}
                      helperText={
                        usedSlots.has(slot)
                          ? `${fileCharset.byte_count} unique bytes`
                          : `${fileCharset.byte_count} unique bytes (not referenced in mask)`
                      }
                      sx={{ flex: 1, '& input': { fontFamily: 'monospace' } }}
                    />
                  </Box>
                ) : (
                  <TextField
                    label={`Charset ${slot} (-${slot})`}
                    value={customCharsets[slot] || ''}
                    onChange={(e) => handleCharsetChange(slot, e.target.value)}
                    fullWidth
                    size="small"
                    placeholder={hexCharset ? 'e.g., 41424344 (hex byte pairs)' : 'e.g., ?u?d or abcdef0123456789'}
                    helperText={
                      getInlineError(slot) ||
                      (charsetSizes[slot]
                        ? `${charsetSizes[slot]} ${hexCharset ? 'bytes' : 'chars'}${usedSlots.has(slot) ? '' : ' (not referenced in mask)'}`
                        : usedSlots.has(slot) ? 'Referenced in mask but not defined' : undefined)
                    }
                    error={(usedSlots.has(slot) && !customCharsets[slot]) || !!getInlineError(slot)}
                    sx={{ flex: 1 }}
                  />
                )}
                {sortedSavedCharsets.length > 0 && (
                  <Autocomplete
                    size="small"
                    options={sortedSavedCharsets}
                    getOptionLabel={(option) => option.name}
                    onChange={(_, value) => handleSavedCharsetSelect(slot, value)}
                    renderInput={(params) => (
                      <TextField {...params} label="Saved" size="small" />
                    )}
                    renderOption={(props, option) => {
                      const isIncompatible = option.charset_type !== 'file' && option.is_hex !== hexCharset;
                      return (
                        <li {...props}>
                          <Box sx={{ opacity: isIncompatible ? 0.6 : 1 }}>
                            <Box sx={{ display: 'flex', gap: 0.5, alignItems: 'center' }}>
                              <Typography variant="body2">{option.name}</Typography>
                              {option.charset_type === 'file' && (
                                <Chip label="File" size="small" color="info" sx={{ height: 18, fontSize: '0.65rem' }} />
                              )}
                              {option.is_hex && (
                                <Chip label="Hex" size="small" color="warning" sx={{ height: 18, fontSize: '0.65rem' }} />
                              )}
                            </Box>
                            <Typography variant="caption" color="text.secondary">
                              {option.charset_type === 'file'
                                ? `${option.byte_count} unique bytes`
                                : option.is_hex
                                  ? `${Math.floor((option.definition?.length || 0) / 2)} bytes (hex)`
                                  : option.definition}
                              {isIncompatible && (option.is_hex ? ' — will enable hex mode' : ' — will disable hex mode')}
                            </Typography>
                          </Box>
                        </li>
                      );
                    }}
                    sx={{ minWidth: 140 }}
                  />
                )}
              </Box>
            </Grid>
          );
        })}
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
              label={`?${slot} = ${size} ${hexCharset && !fileCharsetByteCounts[slot] ? 'bytes' : 'chars'}`}
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
