import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Box, Typography } from '@mui/material';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import CancelIcon from '@mui/icons-material/Cancel';
import { PasswordPolicy } from '../../types/auth';
import { getPasswordPolicy } from '../../services/auth';

interface PasswordValidationProps {
  password: string;
}

interface ValidationState {
  length: boolean;
  uppercase: boolean;
  lowercase: boolean;
  numbers: boolean;
  specialChars: boolean;
}

const PasswordValidation: React.FC<PasswordValidationProps> = ({ password }) => {
  const { t } = useTranslation('common');
  const [policy, setPolicy] = useState<PasswordPolicy | null>(null);
  const [validation, setValidation] = useState<ValidationState>({
    length: false,
    uppercase: false,
    lowercase: false,
    numbers: false,
    specialChars: false,
  });

  useEffect(() => {
    const loadPolicy = async () => {
      try {
        const policyData = await getPasswordPolicy();
        setPolicy(policyData);
      } catch (error) {
        console.error('Failed to load password policy:', error);
      }
    };
    loadPolicy();
  }, []);

  useEffect(() => {
    if (!policy) return;

    setValidation({
      length: password.length >= (policy.minPasswordLength || 8),
      uppercase: !policy.requireUppercase || /[A-Z]/.test(password),
      lowercase: !policy.requireLowercase || /[a-z]/.test(password),
      numbers: !policy.requireNumbers || /[0-9]/.test(password),
      specialChars: !policy.requireSpecialChars || /[!@#$%^&*(),.?":{}|<>]/.test(password),
    });
  }, [password, policy]);

  if (!policy) return null;

  return (
    <Box sx={{ mt: 1 }}>
      <Typography variant="subtitle2" color="textSecondary" gutterBottom>
        {t('passwordValidation.requirements')}
      </Typography>
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
        <ValidationItem
          valid={validation.length}
          text={t('passwordValidation.minLength', { length: policy.minPasswordLength })}
        />
        {policy.requireUppercase && (
          <ValidationItem
            valid={validation.uppercase}
            text={t('passwordValidation.uppercase')}
          />
        )}
        {policy.requireLowercase && (
          <ValidationItem
            valid={validation.lowercase}
            text={t('passwordValidation.lowercase')}
          />
        )}
        {policy.requireNumbers && (
          <ValidationItem
            valid={validation.numbers}
            text={t('passwordValidation.number')}
          />
        )}
        {policy.requireSpecialChars && (
          <ValidationItem
            valid={validation.specialChars}
            text={t('passwordValidation.specialChar')}
          />
        )}
      </Box>
    </Box>
  );
};

interface ValidationItemProps {
  valid: boolean;
  text: string;
}

const ValidationItem: React.FC<ValidationItemProps> = ({ valid, text }) => (
  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
    {valid ? (
      <CheckCircleIcon color="success" sx={{ fontSize: 16 }} />
    ) : (
      <CancelIcon color="error" sx={{ fontSize: 16 }} />
    )}
    <Typography variant="body2" color={valid ? 'success.main' : 'error.main'}>
      {text}
    </Typography>
  </Box>
);

export default PasswordValidation; 