# KrakenHashes Frontend Development Guide

This guide provides comprehensive documentation for developing the KrakenHashes frontend application. The frontend is built with React, TypeScript, Material-UI, and React Query.

## Table of Contents

1. [Development Environment Setup](#development-environment-setup)
2. [Project Structure and Organization](#project-structure-and-organization)
3. [Component Development Patterns](#component-development-patterns)
4. [State Management with React Query](#state-management-with-react-query)
5. [API Integration and Services](#api-integration-and-services)
6. [Material-UI Theming and Styling](#material-ui-theming-and-styling)
7. [TypeScript Conventions](#typescript-conventions)
8. [Testing Approaches](#testing-approaches)

## Development Environment Setup

### Prerequisites

- Node.js 18+ and npm 9+
- Docker and Docker Compose (for backend services)
- A modern web browser with developer tools

### Initial Setup

1. **Clone the repository:**
   ```bash
   git clone https://github.com/yourusername/krakenhashes.git
   cd krakenhashes/frontend
   ```

2. **Install dependencies:**
   ```bash
   npm install
   ```

3. **Configure environment variables:**
   Create a `.env.local` file in the frontend directory:
   ```env
   REACT_APP_API_URL=https://localhost:31337
   REACT_APP_WS_URL=wss://localhost:31337
   ```

4. **Start the development server:**
   ```bash
   npm start
   ```
   The application will be available at `http://localhost:3000`

### Running with Backend Services

For full functionality, run the backend services using Docker:

```bash
# From the project root
docker-compose up -d --build
```

### Development Scripts

```bash
npm start       # Start development server (port 3000)
npm run build   # Production build
npm test        # Run tests
npm run eject   # Eject from Create React App (use with caution)
```

## Project Structure and Organization

The frontend follows a feature-based organization pattern:

```
frontend/src/
├── api/                 # API version checking
│   └── version.ts
├── components/          # Reusable components
│   ├── admin/          # Admin-specific components
│   ├── agent/          # Agent management components
│   ├── auth/           # Authentication components
│   ├── common/         # Shared common components
│   ├── hashlist/       # Hashlist management components
│   ├── pot/            # Cracked passwords components
│   └── settings/       # Settings components
├── contexts/           # React contexts
│   └── AuthContext.tsx # Authentication context provider
├── hooks/              # Custom React hooks
│   ├── useAuth.tsx     # Authentication hook
│   ├── useConfirm.tsx  # Confirmation dialog hook
│   ├── useDebounce.ts  # Debounce hook
│   └── useVouchers.ts  # Voucher management hook
├── pages/              # Page components (routes)
│   ├── admin/          # Admin pages
│   ├── settings/       # Settings pages
│   └── ...             # Other page components
├── services/           # API service layer
│   ├── api.ts          # Axios configuration
│   ├── auth.ts         # Authentication services
│   └── ...             # Domain-specific services
├── styles/             # Global styles
│   └── theme.ts        # Material-UI theme
├── types/              # TypeScript type definitions
│   ├── agent.ts        # Agent types
│   ├── auth.ts         # Authentication types
│   └── ...             # Domain-specific types
├── utils/              # Utility functions
│   ├── formatters.ts   # Data formatting utilities
│   ├── validation.ts   # Validation utilities
│   └── ...             # Other utilities
├── App.tsx             # Root application component
├── config.ts           # Application configuration
└── index.tsx           # Application entry point
```

## Component Development Patterns

### Component Structure

Components follow a consistent structure with TypeScript interfaces and comprehensive documentation:

```tsx
/**
 * ComponentName - Brief description of the component
 * 
 * Features:
 *   - Key feature 1
 *   - Key feature 2
 * 
 * Dependencies:
 *   - External libraries used
 *   - Internal components/hooks
 * 
 * Error Scenarios:
 *   - Possible error conditions
 *   - How they're handled
 */

import React, { useState, useCallback } from 'react';
import { Box, Typography } from '@mui/material';

interface ComponentNameProps {
  title: string;
  onAction?: () => void;
}

const ComponentName: React.FC<ComponentNameProps> = ({ title, onAction }) => {
  const [state, setState] = useState<string>('');

  const handleAction = useCallback(() => {
    // Handle action
    onAction?.();
  }, [onAction]);

  return (
    <Box>
      <Typography variant="h5">{title}</Typography>
      {/* Component content */}
    </Box>
  );
};

export default ComponentName;
```

### Layout Components

The application uses a main Layout component with navigation:

```tsx
// components/Layout.tsx
const Layout: React.FC = () => {
  const [open, setOpen] = useState<boolean>(true);
  const navigate = useNavigate();
  const { userRole } = useAuth();

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh' }}>
      <AppBar position="fixed">
        <Toolbar>
          <Typography variant="h6">KrakenHashes</Typography>
          <UserMenu />
        </Toolbar>
      </AppBar>
      <Drawer variant="permanent" open={open}>
        <List>
          {menuItems.map((item) => (
            <ListItem button key={item.text} onClick={() => navigate(item.path)}>
              <ListItemIcon>{item.icon}</ListItemIcon>
              <ListItemText primary={item.text} />
            </ListItem>
          ))}
        </List>
        {userRole === 'admin' && <AdminMenu />}
      </Drawer>
      <Box component="main" sx={{ flexGrow: 1, p: 3 }}>
        <Outlet />
      </Box>
    </Box>
  );
};
```

### Form Components

Forms use React Hook Form with Material-UI integration:

```tsx
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import * as z from 'zod';

const schema = z.object({
  name: z.string().min(1, 'Name is required'),
  email: z.string().email('Invalid email'),
});

type FormData = z.infer<typeof schema>;

const FormComponent: React.FC = () => {
  const { register, handleSubmit, formState: { errors } } = useForm<FormData>({
    resolver: zodResolver(schema),
  });

  const onSubmit = (data: FormData) => {
    // Handle form submission
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)}>
      <TextField
        {...register('name')}
        error={!!errors.name}
        helperText={errors.name?.message}
        label="Name"
        fullWidth
      />
      <Button type="submit" variant="contained">
        Submit
      </Button>
    </form>
  );
};
```

### Paginated Table Components

#### HashlistHashesTable Component

**File**: `frontend/src/components/hashlist/HashlistHashesTable.tsx`

A full-featured paginated table component demonstrating best practices for large dataset display.

**Key Features:**
- React Query integration with automatic caching and refetching
- Material-UI TablePagination with custom page size options [500, 1000, 1500, 2000, All]
- Real-time search with debouncing (300ms delay)
- Dynamic column widths for responsive layout
- Copy-to-clipboard functionality with visual feedback
- Automatic sorting (cracked hashes first)

**Usage Example:**
```tsx
<HashlistHashesTable
  hashlistId="uuid-here"
  hashlistName="My Hashlist"
  totalHashes={1000}
  crackedHashes={250}
/>
```

**Table Layout Best Practice:**

Use dynamic layout with min/max width constraints instead of fixed percentages:

```tsx
<Table size="small">
  <TableHead>
    <TableRow>
      {/* Long content with flexible width */}
      <TableCell sx={{ minWidth: 300, maxWidth: 600 }}>
        Original Hash
      </TableCell>

      {/* Fixed width columns */}
      <TableCell sx={{ minWidth: 120, width: 150 }}>
        Username
      </TableCell>

      {/* Icon-only column */}
      <TableCell sx={{ width: 80 }} align="center">
        Actions
      </TableCell>
    </TableRow>
  </TableHead>
</Table>
```

**Pagination with "All" Option:**

```tsx
const rowsPerPageOptions = [
  500,
  1000,
  1500,
  2000,
  { label: 'All', value: -1 }
];

// Backend API supports -1 for unlimited results
const limit = rowsPerPage === -1 ? -1 : rowsPerPage;
```

**Search Implementation:**

```tsx
const [searchTerm, setSearchTerm] = useState('');
const debouncedSearch = useDebounce(searchTerm, 300);

// Use debounced value in API query
const { data } = useQuery(['hashes', hashlistId, debouncedSearch], ...);
```

**Copy Button Pattern:**

```tsx
// Copy password if cracked, otherwise copy hash
<Tooltip title={hash.is_cracked ? 'Copy password' : 'Copy hash'}>
  <IconButton
    onClick={() => copyToClipboard(
      hash.is_cracked && hash.password
        ? hash.password
        : hash.original_hash
    )}
  >
    <ContentCopyIcon fontSize="small" />
  </IconButton>
</Tooltip>
```

#### Integration Pattern

**File**: `frontend/src/components/hashlist/HashlistDetailView.tsx`

Shows how to integrate paginated table components:

```tsx
// Add domain field to interface
interface HashDetail {
  id: string;
  hash_value: string;
  original_hash: string;
  username?: string;
  domain?: string;        // Added in v1.1+
  hash_type_id: number;
  is_cracked: boolean;
  password?: string;
  last_updated: string;
}

// Replace simple list with paginated table
{hashlist && (
  <HashlistHashesTable
    hashlistId={id!}
    hashlistName={hashlist.name}
    totalHashes={hashlist.total_hashes || 0}
    crackedHashes={hashlist.cracked_hashes || 0}
  />
)}
```

## State Management with React Query

### Query Client Configuration

The application uses React Query for server state management:

```tsx
// App.tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5 * 60 * 1000, // 5 minutes
      refetchOnWindowFocus: true,
    },
  },
});

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      {/* Application components */}
    </QueryClientProvider>
  );
}
```

### Using Queries

Example of fetching data with React Query:

```tsx
import { useQuery } from '@tanstack/react-query';
import { getWordlists } from '../services/wordlists';

const WordlistsComponent: React.FC = () => {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['wordlists'],
    queryFn: getWordlists,
  });

  if (isLoading) return <CircularProgress />;
  if (error) return <Alert severity="error">Failed to load wordlists</Alert>;

  return (
    <Box>
      {data?.map((wordlist) => (
        <WordlistItem key={wordlist.id} wordlist={wordlist} />
      ))}
    </Box>
  );
};
```

### Using Mutations

Example of mutating data with React Query:

```tsx
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { updateWordlist } from '../services/wordlists';

const EditWordlistDialog: React.FC<{ wordlist: Wordlist }> = ({ wordlist }) => {
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

  const mutation = useMutation({
    mutationFn: (data: UpdateWordlistData) => updateWordlist(wordlist.id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['wordlists'] });
      enqueueSnackbar('Wordlist updated successfully', { variant: 'success' });
    },
    onError: (error) => {
      enqueueSnackbar('Failed to update wordlist', { variant: 'error' });
    },
  });

  const handleSubmit = (data: UpdateWordlistData) => {
    mutation.mutate(data);
  };

  return (
    <Dialog open onClose={onClose}>
      <DialogContent>
        {/* Form fields */}
      </DialogContent>
      <DialogActions>
        <Button onClick={() => handleSubmit(formData)} disabled={mutation.isPending}>
          Save
        </Button>
      </DialogActions>
    </Dialog>
  );
};
```

## API Integration and Services

### Axios Configuration

The API service layer uses Axios with interceptors:

```tsx
// services/api.ts
import axios from 'axios';

const API_URL = process.env.REACT_APP_API_URL || 'https://localhost:31337';

export const api = axios.create({
  baseURL: API_URL,
  withCredentials: true, // Required for cookies/session
});

// Request interceptor
api.interceptors.request.use((config) => {
  // Log requests in development
  if (process.env.NODE_ENV === 'development') {
    console.debug(`[API] ${config.method?.toUpperCase()} ${config.url}`, config.data);
  }
  return config;
});

// Response interceptor
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    if (error.response?.status === 401) {
      // Handle authentication errors
      window.location.href = '/login';
    }
    return Promise.reject(error);
  }
);
```

### Service Layer Pattern

Services encapsulate API calls:

```tsx
// services/wordlists.ts
import { api } from './api';
import { Wordlist, WordlistUploadData } from '../types/wordlists';

export const wordlistService = {
  getWordlists: async (): Promise<Wordlist[]> => {
    const response = await api.get('/api/wordlists');
    return response.data;
  },

  uploadWordlist: async (
    formData: FormData,
    onProgress?: (progress: number) => void
  ): Promise<Wordlist> => {
    const response = await api.post('/api/wordlists/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
      onUploadProgress: (progressEvent) => {
        if (progressEvent.total) {
          const progress = Math.round((progressEvent.loaded * 100) / progressEvent.total);
          onProgress?.(progress);
        }
      },
    });
    return response.data;
  },

  updateWordlist: async (id: string, data: Partial<Wordlist>): Promise<Wordlist> => {
    const response = await api.put(`/api/wordlists/${id}`, data);
    return response.data;
  },

  deleteWordlist: async (id: string): Promise<void> => {
    await api.delete(`/api/wordlists/${id}`);
  },
};
```

### Authentication Service

Authentication is handled through a dedicated service:

```tsx
// services/auth.ts
import { api } from './api';
import { LoginCredentials, AuthResponse } from '../types/auth';

export const authService = {
  login: async (credentials: LoginCredentials): Promise<AuthResponse> => {
    const response = await api.post('/api/login', credentials);
    return response.data;
  },

  logout: async (): Promise<void> => {
    await api.post('/api/logout');
  },

  isAuthenticated: async (): Promise<{ authenticated: boolean; role?: string }> => {
    const response = await api.get('/api/check-auth');
    return response.data;
  },

  refreshToken: async (): Promise<void> => {
    await api.post('/api/refresh-token');
  },
};
```

## Material-UI Theming and Styling

### Theme Configuration

The application uses a dark theme with custom overrides:

```tsx
// styles/theme.ts
import { createTheme, Theme } from '@mui/material/styles';

const theme: Theme = createTheme({
  palette: {
    mode: 'dark',
    primary: {
      main: '#ff0000',
    },
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
      styleOverrides: {
        root: {
          border: 'none',
          backgroundColor: '#121212',
          '& .MuiDataGrid-columnHeaders': {
            backgroundColor: 'rgba(255, 255, 255, 0.08)',
          },
          '& .MuiDataGrid-row:hover': {
            backgroundColor: 'rgba(255, 255, 255, 0.05)',
          },
        },
      },
    },
  },
});

export default theme;
```

### Component Styling

Use the `sx` prop for component-specific styles:

```tsx
<Box
  sx={{
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    p: 3,
    backgroundColor: 'background.paper',
    borderRadius: 1,
    '&:hover': {
      backgroundColor: 'action.hover',
    },
  }}
>
  {/* Content */}
</Box>
```

### Responsive Design

Use Material-UI's responsive utilities:

```tsx
<Grid container spacing={2}>
  <Grid item xs={12} sm={6} md={4}>
    {/* Content adapts to screen size */}
  </Grid>
</Grid>

<Box
  sx={{
    width: { xs: '100%', sm: '60%', md: '40%' },
    display: { xs: 'none', md: 'block' },
  }}
>
  {/* Responsive visibility and sizing */}
</Box>
```

## TypeScript Conventions

### Type Definitions

All types are defined in the `types/` directory:

```tsx
// types/wordlists.ts
export enum WordlistType {
  GENERAL = 'general',
  SPECIALIZED = 'specialized',
  TARGETED = 'targeted',
  CUSTOM = 'custom',
}

export interface Wordlist {
  id: string;
  name: string;
  description: string;
  wordlist_type: WordlistType;
  file_size: number;
  word_count: number;
  verification_status: 'pending' | 'verified' | 'failed';
  created_at: string;
  updated_at: string;
}

export interface WordlistUploadData {
  file: File;
  name?: string;
  description?: string;
  wordlist_type: WordlistType;
}
```

### Component Props

Always define interfaces for component props:

```tsx
interface TableProps {
  data: Wordlist[];
  onEdit?: (wordlist: Wordlist) => void;
  onDelete?: (id: string) => void;
  loading?: boolean;
}

const WordlistTable: React.FC<TableProps> = ({ 
  data, 
  onEdit, 
  onDelete, 
  loading = false 
}) => {
  // Component implementation
};
```

### API Response Types

Define types for API responses:

```tsx
// types/api.ts
export interface ApiResponse<T> {
  data: T;
  message?: string;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ApiError {
  error: string;
  message: string;
  statusCode: number;
}
```

### Utility Types

Use TypeScript utility types effectively:

```tsx
// Partial for update operations
type UpdateWordlistData = Partial<Omit<Wordlist, 'id' | 'created_at' | 'updated_at'>>;

// Pick for specific field selections
type WordlistSummary = Pick<Wordlist, 'id' | 'name' | 'word_count'>;

// Union types for status
type JobStatus = 'pending' | 'running' | 'completed' | 'failed';
```

## Testing Approaches

### Unit Testing with Jest and React Testing Library

Although the project doesn't have extensive tests yet, here's the recommended approach:

```tsx
// WordlistTable.test.tsx
import { render, screen, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import WordlistTable from './WordlistTable';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false },
  },
});

const wrapper = ({ children }: { children: React.ReactNode }) => (
  <QueryClientProvider client={queryClient}>
    {children}
  </QueryClientProvider>
);

describe('WordlistTable', () => {
  const mockWordlists: Wordlist[] = [
    {
      id: '1',
      name: 'Test Wordlist',
      description: 'Test description',
      wordlist_type: WordlistType.GENERAL,
      file_size: 1024,
      word_count: 100,
      verification_status: 'verified',
      created_at: '2024-01-01',
      updated_at: '2024-01-01',
    },
  ];

  it('renders wordlist data correctly', () => {
    render(
      <WordlistTable data={mockWordlists} />,
      { wrapper }
    );

    expect(screen.getByText('Test Wordlist')).toBeInTheDocument();
    expect(screen.getByText('100')).toBeInTheDocument();
  });

  it('calls onEdit when edit button is clicked', () => {
    const handleEdit = jest.fn();
    render(
      <WordlistTable data={mockWordlists} onEdit={handleEdit} />,
      { wrapper }
    );

    fireEvent.click(screen.getByLabelText('Edit'));
    expect(handleEdit).toHaveBeenCalledWith(mockWordlists[0]);
  });
});
```

### Integration Testing

Test components with API interactions:

```tsx
// WordlistsManagement.integration.test.tsx
import { render, screen, waitFor } from '@testing-library/react';
import { rest } from 'msw';
import { setupServer } from 'msw/node';
import WordlistsManagement from './WordlistsManagement';

const server = setupServer(
  rest.get('/api/wordlists', (req, res, ctx) => {
    return res(
      ctx.json({
        data: [
          {
            id: '1',
            name: 'Test Wordlist',
            word_count: 100,
          },
        ],
      })
    );
  })
);

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

test('loads and displays wordlists', async () => {
  render(<WordlistsManagement />);

  await waitFor(() => {
    expect(screen.getByText('Test Wordlist')).toBeInTheDocument();
  });
});
```

### Testing Custom Hooks

```tsx
// useDebounce.test.ts
import { renderHook, act } from '@testing-library/react-hooks';
import useDebounce from './useDebounce';

describe('useDebounce', () => {
  jest.useFakeTimers();

  it('returns initial value immediately', () => {
    const { result } = renderHook(() => useDebounce('test', 500));
    expect(result.current).toBe('test');
  });

  it('debounces value changes', () => {
    const { result, rerender } = renderHook(
      ({ value, delay }) => useDebounce(value, delay),
      { initialProps: { value: 'test', delay: 500 } }
    );

    rerender({ value: 'updated', delay: 500 });
    expect(result.current).toBe('test');

    act(() => {
      jest.advanceTimersByTime(500);
    });

    expect(result.current).toBe('updated');
  });
});
```

### Running Tests

```bash
# Run all tests
npm test

# Run tests in watch mode
npm test -- --watch

# Run tests with coverage
npm test -- --coverage

# Run specific test file
npm test WordlistTable.test.tsx
```

## Best Practices

### Component Guidelines

1. **Keep components focused**: Each component should have a single responsibility
2. **Use TypeScript strictly**: Enable strict mode and avoid `any` types
3. **Implement proper error handling**: Use error boundaries and display user-friendly messages
4. **Optimize performance**: Use React.memo, useCallback, and useMemo appropriately
5. **Follow accessibility standards**: Use semantic HTML and ARIA attributes

### Code Organization

1. **Consistent file naming**: Use PascalCase for components, camelCase for utilities
2. **Co-locate related files**: Keep tests, styles, and types near their components
3. **Extract reusable logic**: Create custom hooks for shared functionality
4. **Document complex logic**: Add JSDoc comments for non-trivial functions

### Performance Optimization

1. **Lazy load routes**: Use React.lazy for code splitting
2. **Optimize re-renders**: Use React Query's staleTime and cacheTime
3. **Virtualize long lists**: Use react-window for large datasets
4. **Debounce user input**: Use the useDebounce hook for search fields

### Security Considerations

1. **Sanitize user input**: Validate and sanitize all user-provided data
2. **Use HTTPS**: Ensure all API calls use secure connections
3. **Handle authentication properly**: Store tokens securely and implement refresh logic
4. **Implement CSP**: Configure Content Security Policy headers

## Troubleshooting

### Common Issues

1. **Certificate errors**: The application includes a certificate check on startup
2. **CORS issues**: Ensure the backend is configured to accept frontend origin
3. **Authentication loops**: Check token refresh logic and API interceptors
4. **Build failures**: Clear node_modules and package-lock.json, then reinstall

### Debug Tips

1. Enable React Developer Tools
2. Use Redux DevTools for React Query debugging
3. Check network tab for API call failures
4. Review console for error messages

## Resources

- [React Documentation](https://react.dev/)
- [Material-UI Documentation](https://mui.com/)
- [React Query Documentation](https://tanstack.com/query/latest)
- [TypeScript Documentation](https://www.typescriptlang.org/)
- [React Hook Form](https://react-hook-form.com/)