/**
 * LanguageSelector - Language selection dropdown component
 *
 * Allows users to change the application language. The selected language
 * is persisted in localStorage and automatically restored on page load.
 *
 * @returns {JSX.Element} Language selector dropdown
 */

import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
    IconButton,
    Menu,
    MenuItem,
    ListItemIcon,
    ListItemText,
    Typography,
    Tooltip,
    Box,
} from '@mui/material';
import {
    Language as LanguageIcon,
    Check as CheckIcon,
} from '@mui/icons-material';
import { supportedLanguages, SupportedLanguage } from '../../i18n';

const LanguageSelector: React.FC = () => {
    const { i18n, t } = useTranslation('common');
    const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null);
    const open = Boolean(anchorEl);

    const handleClick = (event: React.MouseEvent<HTMLElement>) => {
        setAnchorEl(event.currentTarget);
    };

    const handleClose = () => {
        setAnchorEl(null);
    };

    const handleLanguageChange = (languageCode: SupportedLanguage) => {
        i18n.changeLanguage(languageCode);
        handleClose();
    };

    // Get current language, falling back to 'en' if not recognized
    const currentLanguage = (
        Object.keys(supportedLanguages).includes(i18n.language)
            ? i18n.language
            : 'en'
    ) as SupportedLanguage;

    const currentLangInfo = supportedLanguages[currentLanguage];

    return (
        <Box>
            <Tooltip title={t('language.selectLanguage') as string}>
                <IconButton
                    onClick={handleClick}
                    color="inherit"
                    aria-label={t('language.selectLanguage') as string}
                    aria-controls={open ? 'language-menu' : undefined}
                    aria-haspopup="true"
                    aria-expanded={open ? 'true' : undefined}
                    size="small"
                >
                    <LanguageIcon />
                    <Typography
                        variant="caption"
                        sx={{
                            ml: 0.5,
                            display: { xs: 'none', sm: 'inline' },
                            fontSize: '1rem',
                        }}
                    >
                        {currentLangInfo.flag}
                    </Typography>
                </IconButton>
            </Tooltip>
            <Menu
                id="language-menu"
                anchorEl={anchorEl}
                open={open}
                onClose={handleClose}
                MenuListProps={{
                    'aria-labelledby': 'language-button',
                }}
                transformOrigin={{ horizontal: 'right', vertical: 'top' }}
                anchorOrigin={{ horizontal: 'right', vertical: 'bottom' }}
                PaperProps={{
                    elevation: 3,
                    sx: {
                        minWidth: 180,
                        mt: 1,
                    },
                }}
            >
                {Object.entries(supportedLanguages).map(
                    ([code, { nativeName, flag }]) => (
                        <MenuItem
                            key={code}
                            onClick={() =>
                                handleLanguageChange(code as SupportedLanguage)
                            }
                            selected={currentLanguage === code}
                        >
                            <ListItemIcon sx={{ minWidth: 36 }}>
                                <Typography variant="body1" component="span">
                                    {flag}
                                </Typography>
                            </ListItemIcon>
                            <ListItemText primary={nativeName} />
                            {currentLanguage === code && (
                                <CheckIcon
                                    fontSize="small"
                                    color="primary"
                                    sx={{ ml: 1 }}
                                />
                            )}
                        </MenuItem>
                    )
                )}
            </Menu>
        </Box>
    );
};

export default LanguageSelector;
