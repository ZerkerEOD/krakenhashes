import React from 'react';
import { Stack } from '@mui/material';
import AccountCard from '../../../components/settings/AccountCard';
import PreferencesCard from '../../../components/settings/PreferencesCard';

const ProfileSection: React.FC = () => (
  <Stack spacing={3}>
    <AccountCard />
    <PreferencesCard />
  </Stack>
);

export default ProfileSection;
