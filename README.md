# HashDom

HashDom is a multi-facet application consisting of a frontend, backend, and agent components.

## Components

- hashdom-frontend: React-based frontend application
- hashdom-backend: Go-based backend server
- hashdom-agent: Go-based agent for distributed job processing

## Development

Instructions for setting up and running each component can be found in their respective directories.

## Version 1.0 Roadmap

### Core Infrastructure
- [ ] Implement TLS support
  - [ ] Self-signed certificate support
  - [ ] User-provided certificate support
  - [ ] Certbot integration
- [ ] Docker containerization
  - [ ] Environment variable configuration
  - [ ] Database initialization handling
  - [ ] Deployment to DockerHub with documentation
- [ ] Email notification system
  - [ ] Security event notifications
  - [ ] Job completion notifications
  - [ ] Admin error notifications
  - [ ] Email-based MFA (optional)

### Authentication & Authorization
- [ ] Enhanced user management
  - [ ] User groups (Admin/User roles)
  - [ ] MFA implementation
    - [ ] Email-based authentication
    - [ ] Backup codes (6 codes)
    - [ ] Admin MFA override capability
  - [ ] Password change functionality
  - [ ] Account management features
- [ ] Team management
  - [ ] Team manager role implementation
    - [ ] Team settings modification
    - [ ] Member management
  - [ ] User-team assignment system
  - [ ] Team-based agent access control
    - [ ] Multi-team agent assignment
    - [ ] Agent ownership override rules

### Job Processing System
- [ ] Hashlist management
  - [ ] Comprehensive hashcat hash type support
  - [ ] Hash configuration database
    - [ ] Salt status tracking
    - [ ] Performance characteristics (slow/fast)
  - [ ] Agent-side validation with error parsing
- [ ] Task management
  - [ ] Multi-level priority system
    - [ ] FIFO within priority levels
    - [ ] Pre-defined task templates
  - [ ] Intelligent job distribution
    - [ ] Team-based routing
    - [ ] Agent availability tracking
    - [ ] Owner priority handling
  - [ ] Progress tracking
  - [ ] Result storage
- [ ] Resource management
  - [ ] Wordlist upload/management
  - [ ] Rules file management
  - [ ] Tool version management

### Agent Enhancements
- [ ] Job processing
  - [ ] Hashcat integration
    - [ ] Command generation based on hash type
    - [ ] Error handling and reporting
  - [ ] Benchmark system
  - [ ] Dynamic workload calculation
- [ ] Advanced monitoring
  - [ ] GPU/CPU temperature tracking
  - [ ] Resource usage history
  - [ ] Performance metrics
- [ ] Scheduling system
  - [ ] On/Off toggle
  - [ ] Daily schedule configuration
  - [ ] Resource usage limits

### Frontend Features
- [ ] Dashboard
  - [ ] User-specific job status
  - [ ] Performance statistics
  - [ ] System health indicators
- [ ] Task management interface
  - [ ] Job creation/modification
  - [ ] Priority level assignment
  - [ ] Task template management
  - [ ] Progress monitoring
  - [ ] Result viewing
- [ ] Resource management pages
  - [ ] Wordlist management
  - [ ] Rules management
  - [ ] Tool configuration
- [ ] Admin panel
  - [ ] System configuration
  - [ ] User management
    - [ ] MFA management
    - [ ] Team assignment
  - [ ] Security settings
- [ ] Account management
  - [ ] Profile settings
  - [ ] Security settings
    - [ ] MFA setup/recovery
  - [ ] Team management

### Documentation
- [ ] API documentation
- [ ] Deployment guides
- [ ] User manual
  - [ ] Priority system guidelines
  - [ ] Hash type reference
  - [ ] Best practices
- [ ] Administrator guide
  - [ ] Team management guidelines
  - [ ] Security recommendations

### Version 2.0 Considerations
- [ ] Passkey support for MFA
- [ ] Additional authentication methods
- [ ] Team resource quotas
- [ ] Advanced job dependencies
