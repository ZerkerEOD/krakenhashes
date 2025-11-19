import React, { useEffect, useState, useCallback } from 'react';
import {
  Box,
  Typography,
  Button,
  Paper,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  CircularProgress,
  Alert
} from '@mui/material';
import {
  Security as SecurityIcon,
  Download as DownloadIcon,
  Refresh as RefreshIcon,
  CheckCircle as CheckCircleIcon,
  ContentCopy as ContentCopyIcon,
  Warning as WarningIcon
} from '@mui/icons-material';
import { setCookie } from '../utils/cookies';

interface CertificateCheckProps {
  onCertVerified: () => void;
}

const CertificateCheck: React.FC<CertificateCheckProps> = ({ onCertVerified }) => {
  const [checking, setChecking] = useState(true);
  const [certValid, setCertValid] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const checkCertificate = useCallback(async () => {
    try {
      setChecking(true);
      setError(null);

      /* TODO: Use httpApiUrl for configurable API endpoints once environment configuration is complete
       * Currently using window.location.hostname directly for development
       * const httpApiUrl = process.env.REACT_APP_HTTP_API_URL || 'http://localhost:1337';
       */
      const apiUrl = process.env.REACT_APP_API_URL || 'https://localhost:31337';

      // ALWAYS use HTTP port 1337 for CA certificate availability check
      // This avoids chicken-and-egg problem where HTTPS might not be available yet
      // (especially on first boot when certbot is obtaining certificates)
      const httpPort = 1337;
      const caCheckUrl = `http://${window.location.hostname}:${httpPort}/ca.crt`;

      // First try to fetch the CA cert to see if it exists
      const caResponse = await fetch(caCheckUrl, {
        method: 'HEAD',
        credentials: 'include',
        mode: 'cors',
        headers: {
          'Accept': 'application/x-x509-ca-cert'
        }
      });

      if (!caResponse.ok) {
        console.error('CA certificate not available');
        setCertValid(false);
        setError('CA certificate not available from server. Please contact support.');
        return;
      }

      // Now try to make a request to a secure endpoint using the HTTPS API URL
      const response = await fetch(`${apiUrl}/api/health`, {
        method: 'GET',
        credentials: 'include',
        headers: {
          'Accept': 'application/json',
        }
      });
      
      if (response.ok) {
        setCertValid(true);
        localStorage.setItem('cert_valid', 'true');
        onCertVerified();
      } else {
        console.error('Health check failed:', response.status, response.statusText);
        setCertValid(false);
        localStorage.removeItem('cert_valid');
        setError('Failed to verify certificate. Please ensure you have installed the CA certificate and try again.');
      }
    } catch (error) {
      console.debug('[CertCheck] Certificate validation failed:', error);
      // If we get a security error, it means the certificate isn't trusted
      setCertValid(false);
      localStorage.removeItem('cert_valid');
      setError('Certificate not trusted. Please install the CA certificate and ensure it is trusted by your browser.');
    } finally {
      setChecking(false);
    }
  }, [onCertVerified]);

  const handleIgnoreWarning = () => {
    if (window.confirm(
      'WARNING: This will bypass SSL certificate validation for 30 days.\n\n' +
      'This should ONLY be used in development/testing environments.\n' +
      'Your connection will not be secure.\n\n' +
      'Are you sure you want to continue?'
    )) {
      setCookie('ignoreSSL', 'true', 30);
      onCertVerified();
    }
  };

  const copyDownloadUrl = async () => {
    try {
      setError(null);
      
      // Always use HTTP for CA certificate download to avoid chicken-and-egg problem
      const httpPort = 1337;
      const downloadUrl = `http://${window.location.hostname}:${httpPort}/ca.crt`;
      
      // Copy to clipboard
      await navigator.clipboard.writeText(downloadUrl);
      
      // Show success message with instructions
      setError(`Certificate download URL copied to clipboard!

Please follow these steps:
1. Open a new browser tab
2. Paste the URL (${downloadUrl}) and press Enter
3. Save the file as 'krakenhashes-ca.crt'
4. Install the certificate according to your operating system:
   • Windows: Double-click and install to "Trusted Root Certification Authorities"
   • macOS: Double-click and add to System keychain, then trust it
   • Linux: See documentation for your distribution
5. Restart your browser
6. Click "Verify Certificate" below to check the installation`);

    } catch (error) {
      console.error('Failed to copy URL:', error);
      // Fallback: show the URL for manual copying
      const httpPort = 1337;
      const downloadUrl = `http://${window.location.hostname}:${httpPort}/ca.crt`;
      setError(`Could not copy to clipboard. Please manually copy this URL:

${downloadUrl}

Then paste it in a new browser tab to download the certificate.`);
    }
  };

  useEffect(() => {
    // Don't use stored validation state anymore - always check
    checkCertificate();
  }, [checkCertificate]);

  if (checking) {
    return (
      <Box
        display="flex"
        flexDirection="column"
        alignItems="center"
        justifyContent="center"
        minHeight="100vh"
        gap={2}
      >
        <CircularProgress />
        <Typography>Checking SSL Certificate...</Typography>
      </Box>
    );
  }

  if (!certValid) {
    return (
      <Box
        display="flex"
        justifyContent="center"
        alignItems="center"
        minHeight="100vh"
        padding={3}
      >
        <Paper elevation={3} sx={{ p: 4, maxWidth: 600 }}>
          <Box display="flex" flexDirection="column" gap={3}>
            <Box display="flex" alignItems="center" gap={2}>
              <SecurityIcon color="primary" sx={{ fontSize: 40 }} />
              <Typography variant="h5" component="h1">
                Security Certificate Required
              </Typography>
            </Box>

            {error && (
              <Alert severity={error.includes('Please follow these steps') ? 'info' : 'error'} onClose={() => setError(null)}>
                {error}
              </Alert>
            )}

            <Typography>
              To ensure secure communication with KrakenHashes, you need to install our certificate authority (CA) certificate.
            </Typography>

            <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
              Note: Since the HTTPS certificate is not yet trusted, you need to download the CA certificate via HTTP first.
            </Typography>

            <List>
              <ListItem>
                <ListItemIcon>
                  <ContentCopyIcon />
                </ListItemIcon>
                <ListItemText 
                  primary="Copy Download URL"
                  secondary="Click the button below to copy the certificate download URL to your clipboard"
                />
              </ListItem>
              <ListItem>
                <ListItemIcon>
                  <DownloadIcon />
                </ListItemIcon>
                <ListItemText 
                  primary="Download CA Certificate"
                  secondary="Open a new tab, paste the URL, and download the krakenhashes-ca.crt file"
                />
              </ListItem>
              <ListItem>
                <ListItemIcon>
                  <SecurityIcon />
                </ListItemIcon>
                <ListItemText 
                  primary="Install CA Certificate"
                  secondary="Install the certificate as a trusted root CA (see instructions after copying URL)"
                />
              </ListItem>
              <ListItem>
                <ListItemIcon>
                  <RefreshIcon />
                </ListItemIcon>
                <ListItemText 
                  primary="Restart Browser & Verify"
                  secondary="Restart your browser, then click Verify to check the installation"
                />
              </ListItem>
            </List>

            <Box display="flex" gap={2} flexWrap="wrap">
              <Button
                variant="contained"
                startIcon={<ContentCopyIcon />}
                onClick={copyDownloadUrl}
                size="large"
              >
                Copy Certificate URL
              </Button>
              <Button
                variant="outlined"
                startIcon={<RefreshIcon />}
                onClick={checkCertificate}
                size="large"
              >
                Verify Certificate
              </Button>
              <Button
                variant="text"
                color="warning"
                startIcon={<WarningIcon />}
                onClick={handleIgnoreWarning}
                size="large"
              >
                Ignore Warning for 30 Days
              </Button>
            </Box>
            
            <Typography variant="caption" color="text.secondary" sx={{ mt: 2, textAlign: 'center' }}>
              For detailed installation instructions, see the SSL/TLS documentation.
            </Typography>
          </Box>
        </Paper>
      </Box>
    );
  }

  return (
    <Box
      display="flex"
      flexDirection="column"
      alignItems="center"
      justifyContent="center"
      minHeight="100vh"
      gap={2}
    >
      <CheckCircleIcon color="success" sx={{ fontSize: 48 }} />
      <Typography>Certificates verified! Redirecting...</Typography>
    </Box>
  );
};

export default CertificateCheck; 