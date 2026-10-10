import React, { Suspense } from 'react';
import { useLocation } from 'react-router-dom';
import ErrorBoundary from './ErrorBoundary';
import PageSkeleton, { PageSkeletonVariant } from './PageSkeleton';
import { PageTransition } from './motion';

interface RouteBoundaryProps {
  skeleton?: PageSkeletonVariant;
  children: React.ReactNode;
}

/**
 * Per-route boundary: a page-shaped skeleton while the lazy chunk loads, an
 * error state (with retry) if the page throws, and a fade on entry. Keeps the
 * nav usable when a single page breaks.
 */
const RouteBoundary: React.FC<RouteBoundaryProps> = ({ skeleton = 'list', children }) => {
  const location = useLocation();
  return (
    <ErrorBoundary resetKeys={[location.key]}>
      <Suspense fallback={<PageSkeleton variant={skeleton} />}>
        <PageTransition>{children}</PageTransition>
      </Suspense>
    </ErrorBoundary>
  );
};

export default RouteBoundary;
