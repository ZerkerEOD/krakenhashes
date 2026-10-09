/**
 * BrandingContext (GitHub issue #41)
 *
 * Fetches the public branding once at bootstrap (before authentication), owns
 * the single MUI ThemeProvider, and keeps the document title and favicon in
 * sync. It never blocks rendering: the stock KrakenHashes branding renders
 * immediately and the configured identity swaps in when the request lands.
 */
import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { ThemeProvider } from '@mui/material/styles';
import CssBaseline from '@mui/material/CssBaseline';
import { buildTheme } from '../styles/theme';
import { DEFAULT_BRANDING, PublicBranding, getPublicBranding } from '../services/branding';

interface BrandingContextType {
  branding: PublicBranding;
  /** True once the first fetch has settled (successfully or not). */
  loaded: boolean;
  /** Re-fetch after an admin change so every consumer updates in place. */
  refresh: () => Promise<void>;
}

const BrandingContext = createContext<BrandingContextType | undefined>(undefined);

const BRANDED_ATTR = 'data-branding';
let originalIconLinks: HTMLLinkElement[] | null = null;

/**
 * Swap the favicon links in <head>. The stock links from index.html are kept
 * aside the first time a custom icon is applied so they can be restored when
 * the admin removes it again.
 */
const applyFavicon = (url: string | null) => {
  if (typeof document === 'undefined') return;
  const head = document.head;
  const current = Array.from(
    head.querySelectorAll<HTMLLinkElement>('link[rel~="icon"], link[rel="apple-touch-icon"]')
  );
  if (url) {
    if (originalIconLinks === null) {
      originalIconLinks = current
        .filter((el) => !el.hasAttribute(BRANDED_ATTR))
        .map((el) => el.cloneNode(true) as HTMLLinkElement);
    }
    current.forEach((el) => el.remove());
    const link = document.createElement('link');
    link.rel = 'icon';
    link.href = url;
    link.setAttribute(BRANDED_ATTR, '1');
    head.appendChild(link);
    return;
  }
  const branded = current.filter((el) => el.hasAttribute(BRANDED_ATTR));
  if (branded.length === 0) return;
  branded.forEach((el) => el.remove());
  (originalIconLinks ?? []).forEach((el) => head.appendChild(el.cloneNode(true)));
};

export const BrandingProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [branding, setBranding] = useState<PublicBranding>(DEFAULT_BRANDING);
  const [loaded, setLoaded] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const next = await getPublicBranding();
      setBranding(next);
    } catch (error) {
      console.warn('Branding unavailable, using defaults:', error);
      setBranding(DEFAULT_BRANDING);
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (typeof document === 'undefined') return;
    document.title = branding.page_title;
    applyFavicon(branding.favicon_url);
  }, [branding.page_title, branding.favicon_url]);

  const theme = useMemo(
    () => buildTheme(branding.primary_color, branding.secondary_color),
    [branding.primary_color, branding.secondary_color]
  );

  const value = useMemo(() => ({ branding, loaded, refresh }), [branding, loaded, refresh]);

  return (
    <BrandingContext.Provider value={value}>
      <ThemeProvider theme={theme}>
        <CssBaseline />
        {children}
      </ThemeProvider>
    </BrandingContext.Provider>
  );
};

export const useBranding = (): BrandingContextType => {
  const ctx = useContext(BrandingContext);
  if (!ctx) {
    // Allow use outside the provider (tests, isolated renders) with defaults.
    return { branding: DEFAULT_BRANDING, loaded: false, refresh: async () => undefined };
  }
  return ctx;
};

export default BrandingContext;
