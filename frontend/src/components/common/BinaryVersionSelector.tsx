/**
 * BinaryVersionSelector - A reusable component for selecting binary version patterns.
 *
 * Fetches available patterns from the API and displays them in a grouped dropdown:
 * - Default (uses system default binary)
 * - Major wildcards (e.g., "7.x" - matches any 7.x.x version)
 * - Minor wildcards (e.g., "7.1.x" - matches any 7.1.x version)
 * - Exact versions (e.g., "7.1.2" - specific version)
 */

import React, { useEffect, useState } from 'react';
import {
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormHelperText,
  ListSubheader,
  CircularProgress,
  Box,
} from '@mui/material';
import type { SelectChangeEvent } from '@mui/material';
import { BinaryVersionPattern, BinaryPatternsResponse } from '../../types/agent';
import api from '../../services/api';

interface BinaryVersionSelectorProps {
  /** Currently selected pattern value */
  value: string;
  /** Callback when selection changes */
  onChange: (value: string) => void;
  /** Label for the form field */
  label?: string;
  /** Whether the field is required */
  required?: boolean;
  /** Helper text to display below the field */
  helperText?: string;
  /** Whether the field is disabled */
  disabled?: boolean;
  /** Error state */
  error?: boolean;
  /** Full width mode */
  fullWidth?: boolean;
  /** Size variant */
  size?: 'small' | 'medium';
  /** Margin mode */
  margin?: 'none' | 'dense' | 'normal';
  /** Custom name for the field (for form handling) */
  name?: string;
}

/**
 * Groups patterns by their type for organized dropdown display
 */
function groupPatterns(patterns: BinaryVersionPattern[]): {
  default: BinaryVersionPattern[];
  majorWildcard: BinaryVersionPattern[];
  minorWildcard: BinaryVersionPattern[];
  exact: BinaryVersionPattern[];
} {
  return {
    default: patterns.filter(p => p.type === 'default'),
    majorWildcard: patterns.filter(p => p.type === 'major_wildcard'),
    minorWildcard: patterns.filter(p => p.type === 'minor_wildcard'),
    exact: patterns.filter(p => p.type === 'exact'),
  };
}

export const BinaryVersionSelector: React.FC<BinaryVersionSelectorProps> = ({
  value,
  onChange,
  label = 'Binary Version',
  required = false,
  helperText = 'Select binary version pattern for this job',
  disabled = false,
  error = false,
  fullWidth = true,
  size = 'medium',
  margin = 'normal',
  name = 'binary_version',
}) => {
  const [patterns, setPatterns] = useState<BinaryVersionPattern[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);

  useEffect(() => {
    const fetchPatterns = async () => {
      try {
        setLoading(true);
        setFetchError(null);

        // Fetch patterns from the API
        const response = await api.get<BinaryPatternsResponse>('/api/binary/patterns');
        setPatterns(response.data.patterns || []);
      } catch (err) {
        console.error('Failed to fetch binary patterns:', err);
        setFetchError('Failed to load binary versions');
        // Set a default pattern so the form can still work
        setPatterns([{
          pattern: 'default',
          display: 'System Default',
          type: 'default',
          isDefault: true,
        }]);
      } finally {
        setLoading(false);
      }
    };

    fetchPatterns();
  }, []);

  const handleChange = (event: SelectChangeEvent<string>) => {
    onChange(event.target.value);
  };

  const grouped = groupPatterns(patterns);
  const labelId = `${name}-label`;

  // Build menu items with grouping
  const menuItems: React.ReactNode[] = [];

  // Default section
  if (grouped.default.length > 0) {
    menuItems.push(
      <ListSubheader key="header-default">Default</ListSubheader>
    );
    grouped.default.forEach(p => {
      menuItems.push(
        <MenuItem key={p.pattern} value={p.pattern}>
          {p.display}
        </MenuItem>
      );
    });
  }

  // Major wildcards section
  if (grouped.majorWildcard.length > 0) {
    menuItems.push(
      <ListSubheader key="header-major">Major Version (Latest Minor)</ListSubheader>
    );
    grouped.majorWildcard.forEach(p => {
      menuItems.push(
        <MenuItem key={p.pattern} value={p.pattern}>
          {p.display}
        </MenuItem>
      );
    });
  }

  // Minor wildcards section
  if (grouped.minorWildcard.length > 0) {
    menuItems.push(
      <ListSubheader key="header-minor">Minor Version (Latest Patch)</ListSubheader>
    );
    grouped.minorWildcard.forEach(p => {
      menuItems.push(
        <MenuItem key={p.pattern} value={p.pattern}>
          {p.display}
        </MenuItem>
      );
    });
  }

  // Exact versions section
  if (grouped.exact.length > 0) {
    menuItems.push(
      <ListSubheader key="header-exact">Exact Version</ListSubheader>
    );
    grouped.exact.forEach(p => {
      menuItems.push(
        <MenuItem key={p.pattern} value={p.pattern}>
          {p.display}
        </MenuItem>
      );
    });
  }

  if (loading) {
    return (
      <FormControl fullWidth={fullWidth} margin={margin} disabled>
        <InputLabel id={labelId}>{label}</InputLabel>
        <Select
          labelId={labelId}
          value=""
          label={label}
          size={size}
        >
          <MenuItem value="">
            <Box display="flex" alignItems="center" gap={1}>
              <CircularProgress size={16} />
              Loading...
            </Box>
          </MenuItem>
        </Select>
        <FormHelperText>Loading binary versions...</FormHelperText>
      </FormControl>
    );
  }

  return (
    <FormControl
      fullWidth={fullWidth}
      margin={margin}
      required={required}
      disabled={disabled}
      error={error || !!fetchError}
    >
      <InputLabel id={labelId}>{label}</InputLabel>
      <Select
        labelId={labelId}
        name={name}
        value={value || 'default'}
        onChange={handleChange}
        label={label}
        size={size}
      >
        {menuItems}
      </Select>
      <FormHelperText>
        {fetchError || helperText}
      </FormHelperText>
    </FormControl>
  );
};

export default BinaryVersionSelector;
