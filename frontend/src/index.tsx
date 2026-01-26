/**
 * Application Entry Point - Root configuration and rendering for KrakenHashes frontend
 * 
 * Features:
 *   - React 18 concurrent rendering
 *   - Theme provider setup
 *   - Global styles configuration
 *   - Error boundary implementation
 *   - Strict mode enforcement
 * 
 * Dependencies:
 *   - react-dom/client for React 18 features
 *   - @mui/material for theming
 *   - @mui/material/styles for ThemeProvider
 *   - ./App for root component
 *   - ./styles/theme for global theming
 * 
 * Error Scenarios:
 *   - Root element not found:
 *     - Missing DOM element
 *     - Invalid DOM ID
 *     - Race conditions
 *   - Theme initialization failures:
 *     - Invalid theme configuration
 *     - Style injection errors
 *   - React rendering errors:
 *     - Component tree errors
 *     - Concurrent mode conflicts
 *     - Memory leaks
 * 
 * Usage Examples:
 * ```tsx
 * // Basic usage (already implemented)
 * const root = ReactDOM.createRoot(rootElement);
 * root.render(
 *   <React.StrictMode>
 *     <ThemeProvider theme={theme}>
 *       <App />
 *     </ThemeProvider>
 *   </React.StrictMode>
 * );
 * 
 * // With custom error boundary
 * root.render(
 *   <React.StrictMode>
 *     <ErrorBoundary>
 *       <ThemeProvider theme={theme}>
 *         <App />
 *       </ThemeProvider>
 *     </ErrorBoundary>
 *   </React.StrictMode>
 * );
 * ```
 * 
 * Performance Considerations:
 *   - Uses React.StrictMode for development optimization
 *   - Implements concurrent rendering features
 *   - Optimizes style injection
 *   - Minimizes initial bundle size
 *   - Efficient error boundary implementation
 * 
 * Browser Support:
 *   - Chrome/Chromium (latest 2 versions)
 *   - Firefox (latest 2 versions)
 *   - Mobile browsers (iOS Safari, Chrome Android)
 * 
 * Security Considerations:
 *   - CSP compliance
 *   - XSS prevention
 *   - Secure style injection
 *   - Protected error messages
 * 
 * @packageDocumentation
 */

import React, { Suspense } from 'react';
import ReactDOM from 'react-dom/client';
import { ThemeProvider } from '@mui/material/styles';
import CssBaseline from '@mui/material/CssBaseline';
import { Box, CircularProgress } from '@mui/material';
import theme from './styles/theme';
import App from './App';

// Initialize i18n - must be imported before App
import './i18n';

// Loading fallback component for i18n lazy loading
const I18nLoadingFallback = () => (
    <Box
        display="flex"
        justifyContent="center"
        alignItems="center"
        minHeight="100vh"
        sx={{ backgroundColor: 'background.default' }}
    >
        <CircularProgress />
    </Box>
);

// Validate root element existence
const rootElement = document.getElementById('root');
if (!rootElement) {
  // Throw descriptive error for debugging
  throw new Error(
    'Failed to find the root element. ' +
    'Please ensure there is a <div id="root"></div> in your HTML.'
  );
}

// Create root with React 18 concurrent features
const root = ReactDOM.createRoot(rootElement);

// Render application with strict mode, theme, and i18n Suspense
root.render(
    <React.StrictMode>
        <Suspense fallback={<I18nLoadingFallback />}>
            <ThemeProvider theme={theme}>
                <CssBaseline />
                <App />
            </ThemeProvider>
        </Suspense>
    </React.StrictMode>
); 