/**
 * LanguageSelector - Language selection dropdown component
 *
 * Allows users to change the application language. The selected language
 * is persisted in localStorage and automatically restored on page load.
 *
 * Uses SVG flag icons from country-flag-icons for consistent rendering
 * across all platforms (Windows, Linux, macOS).
 *
 * When adding a new language:
 * 1. Add the language entry to supportedLanguages in i18n/index.ts
 * 2. Import the flag component below and add it to flagComponents
 * 3. Create translation files in public/locales/{code}/
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
    Tooltip,
    Box,
} from '@mui/material';
import {
    Language as LanguageIcon,
    Check as CheckIcon,
} from '@mui/icons-material';
import { supportedLanguages, SupportedLanguage } from '../../i18n';

// Import SVG flag components (add new flags here when adding languages)
import { US, CN, DE, NL, ES, RU } from 'country-flag-icons/react/3x2';

/**
 * Map of country codes to flag React components.
 * When adding a new language, import its flag above and add it here.
 */
const flagComponents: Record<string, typeof US> = {
    US,
    CN,
    DE,
    NL,
    ES,
    RU,
};

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
    const CurrentFlag = flagComponents[currentLangInfo.countryCode];

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
                    <Box
                        component="span"
                        sx={{
                            ml: 0.5,
                            display: { xs: 'none', sm: 'inline-flex' },
                            alignItems: 'center',
                            width: 24,
                            height: 16,
                        }}
                    >
                        {CurrentFlag && (
                            <CurrentFlag
                                title={currentLangInfo.nativeName}
                                style={{ width: '100%', height: '100%' }}
                            />
                        )}
                    </Box>
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
                    ([code, { nativeName, countryCode }]) => {
                        const FlagComponent = flagComponents[countryCode];
                        return (
                            <MenuItem
                                key={code}
                                onClick={() =>
                                    handleLanguageChange(
                                        code as SupportedLanguage
                                    )
                                }
                                selected={currentLanguage === code}
                            >
                                <ListItemIcon sx={{ minWidth: 36 }}>
                                    <Box
                                        sx={{
                                            width: 24,
                                            height: 16,
                                            display: 'inline-flex',
                                            alignItems: 'center',
                                        }}
                                    >
                                        {FlagComponent && (
                                            <FlagComponent
                                                title={nativeName}
                                                style={{
                                                    width: '100%',
                                                    height: '100%',
                                                }}
                                            />
                                        )}
                                    </Box>
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
                        );
                    }
                )}
            </Menu>
        </Box>
    );
};

export default LanguageSelector;
