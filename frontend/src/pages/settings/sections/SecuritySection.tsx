import React from 'react';
import { Stack } from '@mui/material';
import { useAuth } from '../../../contexts/AuthContext';
import PasswordCard from '../../../components/settings/PasswordCard';
import MFACard from '../../../components/settings/MFACard';
import LinkedAccountsCard from '../../../components/settings/LinkedAccountsCard';
import CaCertificateCard from '../../../components/settings/CaCertificateCard';

const SecuritySection: React.FC = () => {
  const { user, setUser } = useAuth();
  return (
    <Stack spacing={3}>
      <PasswordCard />
      <MFACard onMFAChange={() => setUser && user && setUser({ ...user })} />
      <LinkedAccountsCard />
      <CaCertificateCard />
    </Stack>
  );
};

export default SecuritySection;
