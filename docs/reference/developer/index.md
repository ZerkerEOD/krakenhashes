# Developer Guide

Documentation for developers working on or extending KrakenHashes.

## In This Section

<div class="grid cards" markdown>

-   :material-architecture:{ .lg .middle } **[System Architecture](architecture.md)**

    ---

    Understanding the overall system design and components

-   :material-language-go:{ .lg .middle } **[Backend Development](backend.md)**

    ---

    Working with the Go backend, APIs, and services

-   :material-react:{ .lg .middle } **[Frontend Development](frontend.md)**

    ---

    React frontend, Material-UI components, and state management

-   :material-chip:{ .lg .middle } **[Agent Development](agent.md)**

    ---

    Agent architecture, hardware detection, and job execution

</div>

## Getting Started

### Prerequisites

- Go 1.21+ for backend development
- Node.js 18+ for frontend development  
- Docker and Docker Compose for testing
- PostgreSQL 15+ for database
- Git for version control

### Development Setup

1. **Clone the repository**
   ```bash
   git clone https://github.com/ZerkerEOD/krakenhashes.git
   cd krakenhashes
   ```

2. **Set up development environment**
   - Copy `.env.example` to `.env`
   - Configure database connection
   - Set development flags

3. **Start services**
   ```bash
   docker-compose up -d
   ```

## Development Workflow

### Code Organization

```
krakenhashes/
├── backend/         # Go backend service
├── frontend/        # React frontend
├── agent/          # Go agent system
├── docs/           # Documentation
└── scripts/        # Utility scripts
```

### Key Technologies

- **Backend**: Go, Gin, GORM, JWT, WebSocket
- **Frontend**: React, TypeScript, Material-UI, React Query
- **Database**: PostgreSQL with migrations
- **Agent**: Go, Hashcat integration, Hardware detection
- **Communication**: REST API, WebSocket, TLS

## Contributing Guidelines

!!! warning "Pre-v1.0 Status"
    External contributions are not being accepted until v1.0 release. This documentation is for understanding the codebase structure.

### Code Standards

- Follow Go conventions for backend code
- Use TypeScript strictly in frontend
- Write tests for new functionality
- Document complex algorithms
- Keep security in mind

### Testing

- Unit tests alongside code (`*_test.go`)
- Integration tests in `integration_test/`
- Frontend tests with React Testing Library
- Manual testing with Docker environment

## Architecture Principles

1. **Separation of Concerns**
   - Clear boundaries between layers
   - Repository pattern for data access
   - Service layer for business logic

2. **Security First**
   - Authentication on all endpoints
   - Input validation and sanitization
   - Secure communication channels

3. **Scalability**
   - Distributed agent architecture
   - Efficient job scheduling
   - Resource pooling

4. **Maintainability**
   - Clear code organization
   - Comprehensive error handling
   - Extensive logging

## Need Help?

- Check existing code patterns
- Review test files for examples
- Join [Discord](https://discord.gg/taafA9cSFV) development channel