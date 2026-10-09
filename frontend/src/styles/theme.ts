/**
 * Theme - Global theme configuration for KrakenHashes frontend
 * 
 * Features:
 *   - Dark mode configuration
 *   - Custom palette definitions
 *   - Component style overrides
 *   - Responsive design support
 *   - Material-UI theme customization
 * 
 * Dependencies:
 *   - @mui/material for theming system
 *   - @mui/styles for advanced styling
 *   - @emotion/react for styling engine
 *   - @emotion/styled for styled components
 * 
 * Error Scenarios:
 *   - Theme initialization failures:
 *     - Invalid color values
 *     - Missing required theme properties
 *     - Component override conflicts
 *   - Runtime style injection errors:
 *     - CSS-in-JS failures
 *     - Style sheet conflicts
 *     - Browser compatibility issues
 * 
 * Usage Examples:
 * ```tsx
 * // Basic theme usage
 * import theme from './styles/theme';
 * 
 * <ThemeProvider theme={theme}>
 *   <App />
 * </ThemeProvider>
 * 
 * // Extending theme
 * import { createTheme } from '@mui/material';
 * import baseTheme from './styles/theme';
 * 
 * const extendedTheme = createTheme({
 *   ...baseTheme,
 *   // Custom overrides
 * });
 * 
 * // Accessing theme in components
 * const StyledComponent = styled('div')(({ theme }) => ({
 *   backgroundColor: theme.palette.background.default
 * }));
 * ```
 * 
 * Performance Considerations:
 *   - Optimized color calculations
 *   - Efficient style injection
 *   - Minimal CSS-in-JS overhead
 *   - Cached theme object
 * 
 * Browser Support:
 *   - Chrome/Chromium (latest 2 versions)
 *   - Firefox (latest 2 versions)
 *   - Mobile browsers (iOS Safari, Chrome Android)
 * 
 * Customization Guidelines:
 *   - Follow Material Design specifications
 *   - Maintain consistent color palette
 *   - Use relative units for spacing
 *   - Implement responsive breakpoints
 * 
 * @returns {Theme} Material-UI theme object
 * 
 * @example
 * // Custom component override
 * const theme = createTheme({
 *   components: {
 *     MuiButton: {
 *       styleOverrides: {
 *         root: {
 *           borderRadius: 8
 *         }
 *       }
 *     }
 *   }
 * });
 */

import { createTheme, Theme } from '@mui/material/styles';
import type {} from '@mui/x-data-grid/themeAugmentation'; // Import augmentation for theme typing

export const DEFAULT_PRIMARY = '#ff0000';

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;

export const isHexColor = (value: string | null | undefined): value is string =>
  typeof value === 'string' && HEX_COLOR.test(value);

/**
 * Build the application theme. The dark surfaces are fixed brand values; only
 * the primary / secondary accents are configurable by an admin (issue #41).
 */
export const buildTheme = (
  primary: string = DEFAULT_PRIMARY,
  secondary?: string | null
): Theme =>
  createTheme({
    palette: {
      mode: 'dark',
      primary: {
        main: isHexColor(primary) ? primary : DEFAULT_PRIMARY,
      },
      ...(secondary && isHexColor(secondary) ? { secondary: { main: secondary } } : {}),
      background: {
        default: '#000000',
        paper: '#121212',
      },
      text: {
        primary: '#ffffff',
      },
    },
    components: {
      MuiCssBaseline: {
        styleOverrides: {
          body: {
            backgroundColor: '#000000',
            color: '#ffffff',
          },
        },
      },
      MuiDataGrid: {
        defaultProps: {
          // Optional: Set default props if needed, e.g., disable borders globally
          // border: 0,
        },
        styleOverrides: {
          root: {
            border: 'none',
            backgroundColor: '#121212',
            color: '#ffffff',
            '& .MuiDataGrid-columnHeader, & .MuiDataGrid-cell': {
              borderBottom: '1px solid rgba(81, 81, 81, 1)',
              borderRight: 'none',
            },
            '& .MuiDataGrid-columnHeaders': {
              borderBottom: '1px solid rgba(81, 81, 81, 1)',
              backgroundColor: 'rgba(255, 255, 255, 0.08)',
            },
            '& .MuiDataGrid-columnHeaderTitle': {
              fontWeight: 'bold',
            },
            '& .MuiDataGrid-cell:focus, & .MuiDataGrid-cell:focus-within, & .MuiDataGrid-columnHeader:focus, & .MuiDataGrid-columnHeader:focus-within': {
              outline: 'none !important',
            },
            '& .MuiDataGrid-row:hover': {
              backgroundColor: 'rgba(255, 255, 255, 0.05)',
            },
            '& .MuiIconButton-root': {
              color: 'inherit',
            },
            '& .MuiTablePagination-root': {
              color: 'inherit',
            },
          },
        },
      },
    },
  });

const theme: Theme = buildTheme();

export default theme; 