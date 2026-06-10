import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Typography,
  ToggleButton,
  ToggleButtonGroup,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  TextField,
  Alert,
  Stack,
  Paper,
} from '@mui/material';
import {
  Download as DownloadIcon,
  Dns as DnsIcon,
  Terminal as TerminalIcon,
} from '@mui/icons-material';
import { ClaimVoucher } from '../../types/agent';
import CommandBlock from './CommandBlock';

/** A downloadable binary for a given OS/arch (from /api/public/agent[/launcher]/platforms). */
export interface Platform {
  os: string;
  arch: string;
  display_name: string;
  download_url: string;
  file_name: string;
  file_size: number;
  checksum: string;
}

type Mode = 'launcher' | 'standalone';

const OS_DISPLAY: Record<string, string> = { linux: 'Linux', windows: 'Windows', darwin: 'macOS' };

const ARCH_PRIORITY: Record<string, number> = {
  amd64: 1, x64: 1, '386': 2, x86: 2, arm64: 3, arm: 4,
};

interface Props {
  open: boolean;
  os: string | null;
  agentPlatforms: Platform[];
  launcherPlatforms: Platform[];
  vouchers: ClaimVoucher[];
  /** Pre-selected claim code (e.g. one just generated). */
  defaultCode?: string;
  /** Generate a new claim code; resolves to the new code (or null on failure). */
  onGenerateCode: () => Promise<string | null>;
  onClose: () => void;
}

const REGISTER_LATER = '__register_later__';

function SectionHeader({ icon, title }: { icon: React.ReactNode; title: string }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
      <Box sx={{ color: 'text.secondary', display: 'flex' }}>{icon}</Box>
      <Typography variant="subtitle1" fontWeight={600}>{title}</Typography>
    </Box>
  );
}

export default function AgentInstallWizard({
  open, os, agentPlatforms, launcherPlatforms, vouchers, defaultCode, onGenerateCode, onClose,
}: Props) {
  const { t } = useTranslation('agents');
  const [mode, setMode] = useState<Mode>('launcher');
  const [arch, setArch] = useState<string>('');
  const [code, setCode] = useState<string>(defaultCode || REGISTER_LATER);
  const [serverHost, setServerHost] = useState<string>(window.location.origin);
  const [generating, setGenerating] = useState(false);

  const isWindows = os === 'windows';

  // Platforms for the chosen OS + mode, sorted by arch preference (arm64 first on macOS).
  const platformsForOsMode = useMemo(() => {
    const src = mode === 'launcher' ? launcherPlatforms : agentPlatforms;
    return src
      .filter((p) => p.os === os)
      .sort((a, b) => {
        if (os === 'darwin') {
          if (a.arch === 'arm64' && b.arch === 'amd64') return -1;
          if (a.arch === 'amd64' && b.arch === 'arm64') return 1;
        }
        return (ARCH_PRIORITY[a.arch] || 99) - (ARCH_PRIORITY[b.arch] || 99);
      });
  }, [mode, os, agentPlatforms, launcherPlatforms]);

  // Keep arch valid whenever os/mode (and thus the available list) changes.
  useEffect(() => {
    if (platformsForOsMode.length === 0) {
      setArch('');
      return;
    }
    if (!platformsForOsMode.some((p) => p.arch === arch)) {
      setArch(platformsForOsMode[0].arch);
    }
  }, [platformsForOsMode, arch]);

  // Re-seed the selected code when the dialog (re)opens.
  useEffect(() => {
    if (open) {
      setCode(defaultCode || REGISTER_LATER);
      setMode('launcher');
      setServerHost(window.location.origin);
    }
  }, [open, defaultCode]);

  if (!os) return null;

  const origin = serverHost.replace(/\/+$/, '');
  const hostArg = origin.replace(/^https?:\/\//, '');
  const claimArg = code && code !== REGISTER_LATER ? ` --claim ${code}` : '';
  const launcherUrl = `${origin}/api/public/agent/launcher/download/${os}/${arch}`;
  const agentUrl = `${origin}/api/public/agent/download/${os}/${arch}`;
  const downloadUrl = mode === 'launcher' ? launcherUrl : agentUrl;
  const binary = mode === 'launcher' ? 'krakenhashes-launcher' : 'krakenhashes-agent';
  const exe = isWindows ? '.exe' : '';

  const dl = (url: string, name: string) =>
    isWindows
      ? `Invoke-WebRequest -Uri ${url} -OutFile ${name}.exe`
      : `curl -k -o ${name} ${url}\nchmod +x ${name}`;
  const wgetLine = (url: string, name: string) => `wget --no-check-certificate -O ${name} ${url}`;
  const exec = (args: string) => (isWindows ? `.\\${binary}.exe ${args}` : `./${binary} ${args}`);

  const serviceCmd = `${dl(downloadUrl, binary)}\n${exec(`install --host ${hostArg}${claimArg}`)}`;
  const runCmd = `${dl(downloadUrl, binary)}\n${exec(`--host ${hostArg}${claimArg}`)}`;
  const downloadOnly = isWindows
    ? `Invoke-WebRequest -Uri ${downloadUrl} -OutFile ${binary}.exe`
    : `curl -k -o ${binary} ${downloadUrl}`;

  const handleGenerate = async () => {
    setGenerating(true);
    try {
      const newCode = await onGenerateCode();
      if (newCode) setCode(newCode);
    } finally {
      setGenerating(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>
        {t('install.dialogTitle', { os: OS_DISPLAY[os] || os }) as string}
      </DialogTitle>
      <DialogContent dividers>
        {/* Controls */}
        <Paper variant="outlined" sx={{ p: 2, mb: 2.5 }}>
          <Stack spacing={2}>
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 3 }}>
              <Box>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
                  {t('install.mode') as string}
                </Typography>
                <ToggleButtonGroup value={mode} exclusive size="small" onChange={(_, v) => v && setMode(v)}>
                  <ToggleButton value="launcher">{t('install.modeAutoUpdate') as string}</ToggleButton>
                  <ToggleButton value="standalone">{t('install.modeStandalone') as string}</ToggleButton>
                </ToggleButtonGroup>
              </Box>
              <Box>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
                  {t('install.architecture') as string}
                </Typography>
                <ToggleButtonGroup value={arch} exclusive size="small" onChange={(_, v) => v && setArch(v)}>
                  {platformsForOsMode.map((p) => (
                    <ToggleButton key={p.arch} value={p.arch}>{p.display_name}</ToggleButton>
                  ))}
                </ToggleButtonGroup>
              </Box>
            </Box>

            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1.5, alignItems: 'center' }}>
              <FormControl size="small" sx={{ minWidth: 320, flex: 1 }}>
                <InputLabel>{t('install.key') as string}</InputLabel>
                <Select value={code} label={t('install.key') as string} onChange={(e) => setCode(e.target.value)}>
                  <MenuItem value={REGISTER_LATER}>{t('install.registerLater') as string}</MenuItem>
                  {vouchers.map((v) => (
                    <MenuItem key={v.code} value={v.code}>
                      {v.code} {v.is_continuous ? `(${t('vouchers.continuous')})` : `(${t('vouchers.singleUse')})`}
                      {v.created_by?.username ? ` — ${v.created_by.username}` : ''}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
              <Button variant="text" size="small" onClick={handleGenerate} disabled={generating}>
                {t('install.generateCode') as string}
              </Button>
            </Box>

            <TextField
              label={t('install.serverUrl') as string}
              value={serverHost}
              onChange={(e) => setServerHost(e.target.value)}
              size="small"
              fullWidth
            />
          </Stack>
        </Paper>

        {code === REGISTER_LATER && (
          <Alert severity="info" sx={{ mb: 2 }}>{t('install.registerLaterNote') as string}</Alert>
        )}

        {platformsForOsMode.length === 0 ? (
          <Alert severity="warning">{t('install.noBinaries') as string}</Alert>
        ) : (
          <Stack spacing={2.5}>
            {mode === 'launcher' && (
              <Box>
                <SectionHeader icon={<DnsIcon fontSize="small" />} title={t('install.serviceTitle') as string} />
                <Alert severity="info" variant="outlined" sx={{ mb: 1 }}>
                  {isWindows ? (t('install.serviceRootWindows') as string) : (t('install.serviceRootUnix') as string)}
                </Alert>
                <CommandBlock command={serviceCmd} />
              </Box>
            )}

            <Box>
              <SectionHeader icon={<TerminalIcon fontSize="small" />} title={t('install.runTitle') as string} />
              <CommandBlock command={runCmd} />
            </Box>

            <Box>
              <SectionHeader icon={<DownloadIcon fontSize="small" />} title={t('install.downloadTitle') as string} />
              <Box sx={{ display: 'flex', gap: 1, mb: 1, flexWrap: 'wrap', alignItems: 'center' }}>
                <Button
                  variant="contained"
                  size="small"
                  startIcon={<DownloadIcon />}
                  onClick={() => window.open(downloadUrl, '_blank')}
                >
                  {t('install.downloadButton') as string}
                </Button>
                <Typography variant="caption" color="text.secondary">
                  {binary}{exe} · {arch}
                </Typography>
              </Box>
              <CommandBlock label={isWindows ? 'PowerShell' : 'curl'} command={downloadOnly} />
              {!isWindows && <CommandBlock label="wget" command={wgetLine(downloadUrl, binary)} />}
            </Box>
          </Stack>
        )}

        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 2 }}>
          {mode === 'launcher'
            ? (t('install.launcherBlurb') as string)
            : (t('install.standaloneBlurb') as string)}
        </Typography>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('buttons.close') as string}</Button>
      </DialogActions>
    </Dialog>
  );
}
