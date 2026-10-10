/**
 * Motion helpers. MUI transitions only (no extra dependency); every helper is
 * instant when the OS asks for reduced motion.
 */
import React, { useEffect, useState } from 'react';
import { Box, Fade, Grow } from '@mui/material';
import { keyframes } from '@mui/material/styles';
import { useLocation } from 'react-router-dom';
import { motion } from '../../../styles/tokens';

const REDUCED = '(prefers-reduced-motion: reduce)';

export const useReducedMotion = (): boolean => {
  const [reduced, setReduced] = useState(() =>
    typeof window !== 'undefined' && typeof window.matchMedia === 'function' ? window.matchMedia(REDUCED).matches : false
  );
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return undefined;
    const mq = window.matchMedia(REDUCED);
    const onChange = () => setReduced(mq.matches);
    mq.addEventListener?.('change', onChange);
    return () => mq.removeEventListener?.('change', onChange);
  }, []);
  return reduced;
};

export const pulse = keyframes`
  0% { opacity: 1; }
  50% { opacity: 0.45; }
  100% { opacity: 1; }
`;

export interface FadeInProps {
  in?: boolean;
  delay?: number;
  grow?: boolean;
  children: React.ReactElement;
}

/** Enter-only fade (or grow) for content that just arrived. */
export const FadeIn: React.FC<FadeInProps> = ({ in: inProp = true, delay = 0, grow, children }) => {
  const reduced = useReducedMotion();
  if (reduced) return children;
  const Comp = grow ? Grow : Fade;
  return (
    <Comp in={inProp} appear timeout={motion.duration.base} style={{ transitionDelay: `${delay}ms` }}>
      {children}
    </Comp>
  );
};

/** Fades a page in on every route change (enter only; no exit of the previous page). */
export const PageTransition: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const location = useLocation();
  const reduced = useReducedMotion();
  if (reduced) return <>{children}</>;
  return (
    <Fade key={location.pathname} in appear timeout={motion.duration.base}>
      <Box sx={{ minHeight: '100%' }}>{children}</Box>
    </Fade>
  );
};
