import React, { useEffect } from 'react';
import { useBlocker } from 'react-router-dom';
import { Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle } from '@mui/material';
import { useTranslation } from 'react-i18next';

/**
 * Blocks in-app navigation and browser unload while `isDirty` is true.
 * Render the returned `dialog` somewhere in the component tree.
 *
 * Only for the few explicit-Apply areas (certificate, email provider, network
 * share, cloud threshold policy); autosaving fields never need it.
 */
export const useUnsavedChangesGuard = (isDirty: boolean): { dialog: React.ReactNode } => {
  const { t } = useTranslation('admin');
  const blocker = useBlocker(({ currentLocation, nextLocation }) => isDirty && currentLocation.pathname !== nextLocation.pathname);

  useEffect(() => {
    if (!isDirty) return undefined;
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, [isDirty]);

  // If the page becomes clean while blocked (e.g. Apply succeeded), let the navigation through.
  useEffect(() => {
    if (blocker.state === 'blocked' && !isDirty) blocker.proceed();
  }, [blocker, isDirty]);

  const dialog = (
    <Dialog open={blocker.state === 'blocked'} onClose={() => blocker.reset?.()} maxWidth="xs" fullWidth>
      <DialogTitle>{t('unsaved.title') as string}</DialogTitle>
      <DialogContent>
        <DialogContentText>{t('unsaved.body') as string}</DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={() => blocker.reset?.()} color="inherit">
          {t('unsaved.stay') as string}
        </Button>
        <Button onClick={() => blocker.proceed?.()} color="error" variant="contained">
          {t('unsaved.leave') as string}
        </Button>
      </DialogActions>
    </Dialog>
  );

  return { dialog };
};

export default useUnsavedChangesGuard;
