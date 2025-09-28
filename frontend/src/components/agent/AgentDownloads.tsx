import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Grid,
  Card,
  CardContent,
  Button,
  IconButton,
  Tooltip,
  Chip,
  CircularProgress,
  Alert,
} from '@mui/material';
import {
  Download as DownloadIcon,
  ContentCopy as CopyIcon,
  Check as CheckIcon,
} from '@mui/icons-material';
import { api } from '../../services/api';

interface Platform {
  os: string;
  arch: string;
  display_name: string;
  download_url: string;
  file_name: string;
  file_size: number;
  checksum: string;
}

interface PlatformGroup {
  os: string;
  displayName: string;
  icon: string;
  platforms: Platform[];
}

const formatFileSize = (bytes: number): string => {
  const units = ['B', 'KB', 'MB', 'GB'];
  let size = bytes;
  let unitIndex = 0;

  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }

  return `${size.toFixed(1)} ${units[unitIndex]}`;
};

export default function AgentDownloads() {
  const [platforms, setPlatforms] = useState<PlatformGroup[]>([]);
  const [version, setVersion] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [copiedUrl, setCopiedUrl] = useState<string | null>(null);

  useEffect(() => {
    fetchPlatforms();
  }, []);

  const fetchPlatforms = async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await api.get<{
        version: string;
        platforms: Platform[];
      }>('/api/public/agent/platforms');

      setVersion(response.data.version);

      // Group platforms by OS
      const grouped: { [key: string]: Platform[] } = {};
      response.data.platforms.forEach(platform => {
        if (!grouped[platform.os]) {
          grouped[platform.os] = [];
        }
        grouped[platform.os].push(platform);
      });

      // Sort platforms within each OS group
      const sortPlatforms = (platforms: Platform[], os: string) => {
        return platforms.sort((a, b) => {
          // Special handling for macOS: Apple Silicon (arm64) before Intel (amd64)
          if (os === 'darwin') {
            if (a.arch === 'arm64' && b.arch === 'amd64') return -1;
            if (a.arch === 'amd64' && b.arch === 'arm64') return 1;
          }

          // General architecture priority for all OSes
          const archPriority: { [key: string]: number } = {
            'amd64': 1,  // 64-bit Intel/AMD
            'x64': 1,    // Alternative name for 64-bit
            '386': 2,    // 32-bit Intel
            'x86': 2,    // Alternative name for 32-bit
            'i386': 2,   // Alternative name for 32-bit
            'arm64': 3,  // ARM 64-bit
            'arm': 4,    // ARM 32-bit
            'armv7': 4,  // ARM v7
            'armv6': 5,  // ARM v6
          };

          const aPriority = archPriority[a.arch] || 99;
          const bPriority = archPriority[b.arch] || 99;

          return aPriority - bPriority;
        });
      };

      // Create platform groups with display info
      const groups: PlatformGroup[] = [
        {
          os: 'linux',
          displayName: 'Linux',
          icon: '🐧',
          platforms: sortPlatforms(grouped.linux || [], 'linux'),
        },
        {
          os: 'windows',
          displayName: 'Windows',
          icon: '🪟',
          platforms: sortPlatforms(grouped.windows || [], 'windows'),
        },
        {
          os: 'darwin',
          displayName: 'macOS',
          icon: '🍎',
          platforms: sortPlatforms(grouped.darwin || [], 'darwin'),
        },
      ].filter(g => g.platforms.length > 0);

      setPlatforms(groups);
    } catch (err) {
      console.error('Failed to fetch platforms:', err);
      setError('Failed to load download information');
    } finally {
      setLoading(false);
    }
  };

  const handleDownload = (platform: Platform) => {
    const url = `${window.location.origin}${platform.download_url}`;
    window.open(url, '_blank');
  };

  const handleCopyUrl = async (platform: Platform) => {
    const url = `${window.location.origin}${platform.download_url}`;
    try {
      await navigator.clipboard.writeText(url);
      setCopiedUrl(platform.download_url);
      setTimeout(() => {
        setCopiedUrl(null);
      }, 2000);
    } catch (err) {
      console.error('Failed to copy URL:', err);
    }
  };

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Alert severity="error" sx={{ mb: 3 }}>
        {error}
      </Alert>
    );
  }

  return (
    <Box sx={{ mb: 4 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
        <Typography variant="h5">
          Agent Downloads
        </Typography>
        <Chip
          label={`Version ${version}`}
          color="primary"
          size="small"
        />
      </Box>

      <Grid container spacing={2}>
        {platforms.map((group) => (
          <Grid item xs={12} md={4} key={group.os}>
            <Card variant="outlined">
              <CardContent sx={{ py: 2 }}>
                <Typography variant="h6" sx={{ display: 'flex', alignItems: 'center', mb: 1.5 }}>
                  <span style={{ marginRight: 8 }}>{group.icon}</span>
                  {group.displayName}
                </Typography>

                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                  {group.platforms.map((platform) => (
                    <Box
                      key={`${platform.os}-${platform.arch}`}
                      sx={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        gap: 1
                      }}
                    >
                      <Box sx={{ minWidth: 0, flex: 1 }}>
                        <Typography variant="body2" fontWeight="medium">
                          {platform.display_name}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {formatFileSize(platform.file_size)}
                        </Typography>
                      </Box>

                      <Box sx={{ display: 'flex', gap: 0.5 }}>
                        <Button
                          size="small"
                          variant="outlined"
                          startIcon={<DownloadIcon />}
                          onClick={() => handleDownload(platform)}
                        >
                          Download
                        </Button>
                        <Tooltip title={copiedUrl === platform.download_url ? 'Copied!' : 'Copy URL'}>
                          <IconButton
                            size="small"
                            onClick={() => handleCopyUrl(platform)}
                            color={copiedUrl === platform.download_url ? 'success' : 'default'}
                          >
                            {copiedUrl === platform.download_url ? <CheckIcon /> : <CopyIcon />}
                          </IconButton>
                        </Tooltip>
                      </Box>
                    </Box>
                  ))}
                </Box>
              </CardContent>
            </Card>
          </Grid>
        ))}
      </Grid>
    </Box>
  );
}