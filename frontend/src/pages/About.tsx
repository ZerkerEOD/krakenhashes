import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Box, Typography, Paper, Link, CircularProgress } from '@mui/material';
import { getVersionInfo, VersionInfo } from '../api/version';

const About: React.FC = () => {
    const { t } = useTranslation('common');
    const [versions, setVersions] = useState<VersionInfo | null>(null);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        const fetchVersions = async () => {
            try {
                const data = await getVersionInfo();
                setVersions(data);
            } catch (err) {
                setError(err instanceof Error ? err.message : t('about.errors.fetchFailed') as string);
            }
        };

        fetchVersions();
    }, []);

    if (error) {
        return (
            <Box sx={{ p: 3 }}>
                <Typography color="error">{t('labels.error') as string}: {error}</Typography>
            </Box>
        );
    }

    if (!versions) {
        return (
            <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
                <CircularProgress />
            </Box>
        );
    }

    return (
        <Box sx={{ p: 3 }}>
            <Typography variant="h4" gutterBottom>
                {t('about.title') as string}
            </Typography>

            <Paper sx={{ p: 3, mb: 3 }}>
                <Typography variant="h6" gutterBottom>
                    {t('about.versionInfo.title') as string}
                </Typography>
                <Box sx={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: 2 }}>
                    {versions.release && (
                        <>
                            <Typography><strong>{t('about.versionInfo.release') as string}:</strong></Typography>
                            <Typography sx={{ fontWeight: 600 }}>{versions.release}</Typography>
                        </>
                    )}

                    <Typography><strong>{t('about.versionInfo.backend') as string}:</strong></Typography>
                    <Typography>{versions.backend}</Typography>

                    <Typography><strong>{t('about.versionInfo.frontend') as string}:</strong></Typography>
                    <Typography>{versions.frontend}</Typography>

                    <Typography><strong>{t('about.versionInfo.agent') as string}:</strong></Typography>
                    <Typography>{versions.agent}</Typography>

                    <Typography><strong>{t('about.versionInfo.api') as string}:</strong></Typography>
                    <Typography>{versions.api}</Typography>

                    <Typography><strong>{t('about.versionInfo.database') as string}:</strong></Typography>
                    <Typography>{versions.database}</Typography>
                </Box>
            </Paper>

            <Paper sx={{ p: 3 }}>
                <Typography variant="h6" gutterBottom>
                    {t('about.projectInfo.title') as string}
                </Typography>
                <Typography paragraph>
                    {t('about.projectInfo.description') as string}
                </Typography>
                <Typography paragraph>
                    {t('about.projectInfo.license') as string}
                </Typography>
                <Box sx={{ mt: 2 }}>
                    <Link
                        href="https://github.com/ZerkerEOD/krakenhashes"
                        target="_blank"
                        rel="noopener noreferrer"
                    >
                        {t('about.projectInfo.githubLink') as string}
                    </Link>
                </Box>
            </Paper>
        </Box>
    );
};

export default About; 