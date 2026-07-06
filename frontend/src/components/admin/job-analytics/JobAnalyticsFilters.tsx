import React from 'react';
import {
  Box,
  Paper,
  Grid,
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Button,
  Chip,
  OutlinedInput,
  Collapse,
  IconButton,
  Typography,
  SelectChangeEvent,
} from '@mui/material';
import {
  FilterList as FilterListIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  Clear as ClearIcon,
} from '@mui/icons-material';
import { JobAnalyticsFilterOptions, JobAnalyticsFilterParams } from '../../../types/jobAnalytics';

interface JobAnalyticsFiltersProps {
  filterOptions: JobAnalyticsFilterOptions | undefined;
  filter: JobAnalyticsFilterParams;
  onFilterChange: (filter: JobAnalyticsFilterParams) => void;
  onApply: () => void;
  onReset: () => void;
  loading?: boolean;
}

const JobAnalyticsFilters: React.FC<JobAnalyticsFiltersProps> = ({
  filterOptions,
  filter,
  onFilterChange,
  onApply,
  onReset,
  loading,
}) => {
  const [expanded, setExpanded] = React.useState(true);

  const statusOptions = ['pending', 'running', 'completed', 'cancelled', 'failed', 'paused'];

  const handleTextChange = (field: keyof JobAnalyticsFilterParams) =>
    (e: React.ChangeEvent<HTMLInputElement>) => {
      onFilterChange({ ...filter, [field]: e.target.value || undefined });
    };

  const handleSelectChange = (field: keyof JobAnalyticsFilterParams) =>
    (e: SelectChangeEvent<string>) => {
      const val = e.target.value;
      onFilterChange({ ...filter, [field]: val === '' ? undefined : Number(val) });
    };

  const handleStatusChange = (e: SelectChangeEvent<string[]>) => {
    const val = e.target.value;
    onFilterChange({ ...filter, status: typeof val === 'string' ? val.split(',') : val.length > 0 ? val : undefined });
  };

  const handleNumberChange = (field: keyof JobAnalyticsFilterParams) =>
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const val = e.target.value;
      onFilterChange({ ...filter, [field]: val === '' ? undefined : Number(val) });
    };

  const hasActiveFilters = filter.date_start || filter.date_end || filter.attack_mode !== undefined ||
    filter.hash_type !== undefined || filter.agent_id !== undefined || filter.hashlist_id !== undefined ||
    (filter.status && filter.status.length > 0) || filter.min_keyspace !== undefined || filter.max_keyspace !== undefined;

  return (
    <Paper sx={{ mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', px: 2, py: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <FilterListIcon color="action" />
          <Typography variant="subtitle1">Filters</Typography>
          {hasActiveFilters && <Chip label="Active" color="primary" size="small" />}
        </Box>
        <IconButton onClick={() => setExpanded(!expanded)} size="small">
          {expanded ? <ExpandLessIcon /> : <ExpandMoreIcon />}
        </IconButton>
      </Box>
      <Collapse in={expanded}>
        <Box sx={{ px: 2, pb: 2 }}>
          <Grid container spacing={2}>
            <Grid item xs={12} sm={6} md={3}>
              <TextField
                label="Date Start"
                type="date"
                size="small"
                fullWidth
                value={filter.date_start || ''}
                onChange={handleTextChange('date_start')}
                InputLabelProps={{ shrink: true }}
              />
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <TextField
                label="Date End"
                type="date"
                size="small"
                fullWidth
                value={filter.date_end || ''}
                onChange={handleTextChange('date_end')}
                InputLabelProps={{ shrink: true }}
              />
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <FormControl fullWidth size="small">
                <InputLabel>Attack Mode</InputLabel>
                <Select
                  value={filter.attack_mode !== undefined ? String(filter.attack_mode) : ''}
                  onChange={handleSelectChange('attack_mode')}
                  label="Attack Mode"
                >
                  <MenuItem value="">All</MenuItem>
                  {filterOptions?.attack_modes?.map(am => (
                    <MenuItem key={am.value} value={String(am.value)}>{am.label}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <FormControl fullWidth size="small">
                <InputLabel>Hash Type</InputLabel>
                <Select
                  value={filter.hash_type !== undefined ? String(filter.hash_type) : ''}
                  onChange={handleSelectChange('hash_type')}
                  label="Hash Type"
                >
                  <MenuItem value="">All</MenuItem>
                  {filterOptions?.hash_types?.map(ht => (
                    <MenuItem key={ht.id} value={String(ht.id)}>{ht.name} ({ht.id})</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <FormControl fullWidth size="small">
                <InputLabel>Agent</InputLabel>
                <Select
                  value={filter.agent_id !== undefined ? String(filter.agent_id) : ''}
                  onChange={handleSelectChange('agent_id')}
                  label="Agent"
                >
                  <MenuItem value="">All</MenuItem>
                  {filterOptions?.agents?.map(a => (
                    <MenuItem key={a.id} value={String(a.id)}>{a.name}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <FormControl fullWidth size="small">
                <InputLabel>Hashlist</InputLabel>
                <Select
                  value={filter.hashlist_id !== undefined ? String(filter.hashlist_id) : ''}
                  onChange={handleSelectChange('hashlist_id')}
                  label="Hashlist"
                >
                  <MenuItem value="">All</MenuItem>
                  {filterOptions?.hashlists?.map(h => (
                    <MenuItem key={h.id} value={String(h.id)}>{h.name}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <FormControl fullWidth size="small">
                <InputLabel>Status</InputLabel>
                <Select
                  multiple
                  value={filter.status || []}
                  onChange={handleStatusChange}
                  input={<OutlinedInput label="Status" />}
                  renderValue={(selected) => (
                    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                      {(selected as string[]).map(value => (
                        <Chip key={value} label={value} size="small" />
                      ))}
                    </Box>
                  )}
                >
                  {statusOptions.map(s => (
                    <MenuItem key={s} value={s}>{s}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12} sm={6} md={3}>
              <Box sx={{ display: 'flex', gap: 1 }}>
                <TextField
                  label="Min Keyspace"
                  type="number"
                  size="small"
                  fullWidth
                  value={filter.min_keyspace ?? ''}
                  onChange={handleNumberChange('min_keyspace')}
                />
                <TextField
                  label="Max Keyspace"
                  type="number"
                  size="small"
                  fullWidth
                  value={filter.max_keyspace ?? ''}
                  onChange={handleNumberChange('max_keyspace')}
                />
              </Box>
            </Grid>
          </Grid>
          <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 1, mt: 2 }}>
            <Button
              variant="outlined"
              startIcon={<ClearIcon />}
              onClick={onReset}
              disabled={!hasActiveFilters}
            >
              Reset
            </Button>
            <Button
              variant="contained"
              startIcon={<FilterListIcon />}
              onClick={onApply}
              disabled={loading}
            >
              Apply
            </Button>
          </Box>
        </Box>
      </Collapse>
    </Paper>
  );
};

export default JobAnalyticsFilters;
