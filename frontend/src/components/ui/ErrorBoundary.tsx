import React from 'react';
import { QueryErrorResetBoundary } from '@tanstack/react-query';
import ErrorState from './ErrorState';

interface ErrorBoundaryProps {
  children: React.ReactNode;
  /** Custom fallback; receives the error and a reset callback. */
  fallback?: (error: Error, reset: () => void) => React.ReactNode;
  /** When any of these change, the boundary resets (e.g. the route key). */
  resetKeys?: unknown[];
  onError?: (error: Error, info: React.ErrorInfo) => void;
  compact?: boolean;
}

interface State {
  error: Error | null;
}

class Boundary extends React.Component<ErrorBoundaryProps & { onReset?: () => void }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    this.props.onError?.(error, info);
    // eslint-disable-next-line no-console
    console.error('[ErrorBoundary]', error, info.componentStack);
  }

  componentDidUpdate(prev: ErrorBoundaryProps) {
    if (this.state.error && prev.resetKeys !== this.props.resetKeys) {
      const a = prev.resetKeys ?? [];
      const b = this.props.resetKeys ?? [];
      if (a.length !== b.length || a.some((v, i) => v !== b[i])) this.reset();
    }
  }

  reset = () => {
    this.props.onReset?.();
    this.setState({ error: null });
  };

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    if (this.props.fallback) return this.props.fallback(error, this.reset);
    return <ErrorState error={error} onRetry={this.reset} compact={this.props.compact} />;
  }
}

/** Error boundary that also resets React Query errors on retry. */
const ErrorBoundary: React.FC<ErrorBoundaryProps> = (props) => (
  <QueryErrorResetBoundary>
    {({ reset }) => <Boundary {...props} onReset={reset} />}
  </QueryErrorResetBoundary>
);

export default ErrorBoundary;
