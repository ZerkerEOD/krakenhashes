import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  Box,
  Button,
  Chip,
  Paper,
  Typography,
} from '@mui/material';
import { api } from '../../services/api';
import { ClaimVoucher } from '../../types/agent';
import AgentInstallWizard, { Platform } from './AgentInstallWizard';

interface PlatformsResponse {
  version: string;
  platforms: Platform[];
}

const OS_OPTIONS: { os: string; icon: string; label: string }[] = [
  { os: 'linux', icon: '🐧', label: 'Linux' },
  { os: 'windows', icon: '🪟', label: 'Windows' },
  { os: 'darwin', icon: '🍎', label: 'macOS' },
];

interface Props {
  vouchers: ClaimVoucher[];
  /** Pre-select this code in the wizard (e.g. one just generated). */
  defaultCode?: string;
  /** Generate a new claim code; resolves to the new code (or null). */
  onGenerateCode: () => Promise<string | null>;
}

/**
 * Render a compact "Install an Agent" section with per-OS install buttons that open a platform-specific wizard.
 *
 * Fetches available agent and launcher platforms, shows an "expected version" chip when provided by the agent data,
 * and displays buttons only for OSes that have a corresponding binary. Clicking an OS button opens the AgentInstallWizard
 * with the resolved `agentPlatforms`, `launcherPlatforms`, `vouchers`, and optional `defaultCode`.
 *
 * @param vouchers - Claim vouchers passed through to the install wizard
 * @param defaultCode - Optional default enrollment code to prefill the wizard
 * @param onGenerateCode - Async callback to generate a new enrollment code for the wizard
 * @returns The outlined Paper containing the install section header, optional expected-version chip, blurb, per-OS buttons, and the AgentInstallWizard
 */
export default function AgentInstall({ vouchers, defaultCode, onGenerateCode }: Props) {
  const { t } = useTranslation('agents');
  const [openOs, setOpenOs] = useState<string | null>(null);

  const { data: agentData } = useQuery({
    queryKey: ['agent-platforms'],
    queryFn: async () => (await api.get<PlatformsResponse>('/api/public/agent/platforms')).data,
    staleTime: 5 * 60 * 1000,
  });

  const { data: launcherData } = useQuery({
    queryKey: ['launcher-platforms'],
    queryFn: async () => {
      try {
        return (await api.get<PlatformsResponse>('/api/public/agent/launcher/platforms')).data;
      } catch {
        return { version: '', platforms: [] } as PlatformsResponse;
      }
    },
    staleTime: 5 * 60 * 1000,
  });

  const agentPlatforms = agentData?.platforms || [];
  const launcherPlatforms = launcherData?.platforms || [];
  const expectedVersion = agentData?.version || '';

  // Only offer OSes we actually ship a binary for.
  const available = OS_OPTIONS.filter(
    (o) => agentPlatforms.some((p) => p.os === o.os) || launcherPlatforms.some((p) => p.os === o.os),
  );

  return (
    <Paper variant="outlined" sx={{ p: 3, mb: 3 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 1, flexWrap: 'wrap' }}>
        <Typography variant="h6">{t('install.sectionTitle') as string}</Typography>
        {expectedVersion && (
          <Chip size="small" color="primary" label={t('downloads.expectedVersion', { version: expectedVersion }) as string} />
        )}
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('install.sectionBlurb') as string}
      </Typography>
      <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
        {available.map((o) => (
          <Button
            key={o.os}
            variant="outlined"
            size="large"
            onClick={() => setOpenOs(o.os)}
            sx={{ minWidth: 150, py: 1.5, fontSize: '1rem' }}
          >
            <span style={{ marginRight: 8, fontSize: '1.25rem' }}>{o.icon}</span>
            {o.label}
          </Button>
        ))}
      </Box>

      <AgentInstallWizard
        open={openOs !== null}
        os={openOs}
        agentPlatforms={agentPlatforms}
        launcherPlatforms={launcherPlatforms}
        vouchers={vouchers}
        defaultCode={defaultCode}
        onGenerateCode={onGenerateCode}
        onClose={() => setOpenOs(null)}
      />
    </Paper>
  );
}
