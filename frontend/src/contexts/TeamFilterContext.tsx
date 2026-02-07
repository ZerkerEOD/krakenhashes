import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { Team } from '../types/team';
import { teamsService } from '../services/teams';
import { useAuth } from './AuthContext';

interface TeamFilterContextType {
  teamsEnabled: boolean;
  userTeams: Team[];
  selectedTeamId: string | null;
  setSelectedTeamId: (id: string | null) => void;
  isLoading: boolean;
  refreshTeams: () => Promise<void>;
}

const TeamFilterContext = createContext<TeamFilterContextType | undefined>(undefined);

export const TeamFilterProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const { user, isAuth } = useAuth();
  const [teamsEnabled, setTeamsEnabled] = useState(false);
  const [userTeams, setUserTeams] = useState<Team[]>([]);
  const [selectedTeamId, setSelectedTeamId] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Load teams_enabled setting and user's teams
  const loadTeamData = async () => {
    if (!isAuth) {
      setTeamsEnabled(false);
      setUserTeams([]);
      setIsLoading(false);
      return;
    }

    try {
      setIsLoading(true);

      // Check if teams are enabled via non-admin endpoint
      // This uses GET /api/settings/teams_enabled (not the admin endpoint)
      // so that all authenticated users can check this setting.
      // Step 5 must provide this endpoint.
      const enabled = await teamsService.getTeamsEnabled();
      setTeamsEnabled(enabled);

      if (enabled) {
        // Load user's teams
        const teams = await teamsService.listUserTeams();
        setUserTeams(teams);

        // Restore selected team from localStorage AFTER teams finish loading
        // This prevents a race condition where we restore a stale team ID
        // while the teams list is still loading
        const savedTeamId = localStorage.getItem('selectedTeamId');
        if (savedTeamId && teams.some(t => t.id === savedTeamId)) {
          setSelectedTeamId(savedTeamId);
        } else {
          // Saved team no longer valid (user removed from team, etc.)
          setSelectedTeamId(null);
          localStorage.removeItem('selectedTeamId');
        }
      } else {
        // Teams disabled — clear any stale state
        setUserTeams([]);
        setSelectedTeamId(null);
      }
    } catch (error) {
      console.error('Failed to load team data:', error);
      setTeamsEnabled(false);
      setUserTeams([]);
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    loadTeamData();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAuth, user?.id]);

  // Save selected team to localStorage
  useEffect(() => {
    if (selectedTeamId) {
      localStorage.setItem('selectedTeamId', selectedTeamId);
    } else {
      localStorage.removeItem('selectedTeamId');
    }
  }, [selectedTeamId]);

  const refreshTeams = async () => {
    await loadTeamData();
  };

  return (
    <TeamFilterContext.Provider
      value={{
        teamsEnabled,
        userTeams,
        selectedTeamId,
        setSelectedTeamId,
        isLoading,
        refreshTeams,
      }}
    >
      {children}
    </TeamFilterContext.Provider>
  );
};

export const useTeamFilter = (): TeamFilterContextType => {
  const context = useContext(TeamFilterContext);
  if (context === undefined) {
    throw new Error('useTeamFilter must be used within a TeamFilterProvider');
  }
  return context;
};

// Hook to get current team filter params for API calls
export const useTeamFilterParams = () => {
  const { teamsEnabled, selectedTeamId } = useTeamFilter();

  return {
    teamId: teamsEnabled ? selectedTeamId : undefined,
    teamsEnabled,
  };
};
