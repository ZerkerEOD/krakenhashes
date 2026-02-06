import React from 'react';
import {
  FormControl,
  Select,
  MenuItem,
  InputLabel,
  SelectChangeEvent,
  Box,
  Chip,
} from '@mui/material';
import GroupsIcon from '@mui/icons-material/Groups';
import { useTeamFilter } from '../../contexts/TeamFilterContext';

export const TeamFilter: React.FC = () => {
  const { teamsEnabled, userTeams, selectedTeamId, setSelectedTeamId, isLoading } = useTeamFilter();

  // Don't render if teams not enabled
  if (!teamsEnabled) {
    return null;
  }

  const handleChange = (event: SelectChangeEvent<string>) => {
    const value = event.target.value;
    setSelectedTeamId(value === 'all' ? null : value);
  };

  return (
    <Box sx={{ minWidth: 200, display: 'flex', alignItems: 'center', gap: 1 }}>
      <GroupsIcon sx={{ color: 'text.secondary' }} />
      <FormControl size="small" fullWidth>
        <InputLabel id="team-filter-label">Team</InputLabel>
        <Select
          labelId="team-filter-label"
          id="team-filter"
          value={selectedTeamId || 'all'}
          label="Team"
          onChange={handleChange}
          disabled={isLoading}
        >
          <MenuItem value="all">
            <em>All Teams</em>
          </MenuItem>
          {userTeams.map((team) => (
            <MenuItem key={team.id} value={team.id}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                {team.name}
                {team.user_role === 'admin' && (
                  <Chip label="Admin" size="small" color="primary" />
                )}
              </Box>
            </MenuItem>
          ))}
        </Select>
      </FormControl>
    </Box>
  );
};

export default TeamFilter;
