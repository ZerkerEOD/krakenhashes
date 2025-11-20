# KrakenHashes Glossary

This glossary provides definitions for terms used throughout the KrakenHashes system, organized by category.

## Password Cracking Terminology

### A-Z

**Attack Mode**: The method used by hashcat to attempt password recovery. Common modes include dictionary attack (-a 0), combinator attack (-a 1), brute-force/mask attack (-a 3), and hybrid attacks (-a 6, -a 7).

**Benchmark**: A test run to measure the cracking speed (hashes per second) of specific hardware against various hash algorithms.

**Brute Force Attack**: An attack method that systematically tries all possible character combinations within a defined character set and length range.

**Candidate**: A potential password generated during the cracking process that will be tested against the target hash.

**Charset**: A defined set of characters used in mask or brute-force attacks (e.g., ?l = lowercase, ?u = uppercase, ?d = digits, ?s = special characters).

**Combinator Attack**: An attack that combines words from two wordlists to create password candidates (e.g., "password" + "123" = "password123").

**Cracked Hash**: A hash that has been successfully reversed to reveal its plaintext password.

**Dictionary Attack**: An attack using a wordlist of common passwords and variations to attempt hash cracking.

**Hash**: A one-way cryptographic function output that represents a password. Common types include MD5, SHA1, SHA256, bcrypt, and NTLM.

**Hash Algorithm**: The specific cryptographic function used to create a hash (e.g., MD5, SHA-1, SHA-256, bcrypt, scrypt, Argon2).

**Hash Rate**: The speed at which password candidates are tested, measured in hashes per second (H/s), kilohashes/s (KH/s), megahashes/s (MH/s), or gigahashes/s (GH/s).

**Hashcat**: The underlying password recovery tool used by KrakenHashes for distributed cracking operations.

**Hashlist**: A collection of password hashes to be cracked, typically organized by source, client, or campaign.

**Hybrid Attack**: An attack combining wordlist entries with masks or rules to generate password candidates.

**Keyspace**: The total number of possible password combinations for a given attack configuration.

**Mask**: A pattern defining the structure of passwords to generate in a mask attack (e.g., ?u?l?l?l?d?d?d?d for Abcd1234 format).

**Mask Attack**: A targeted brute-force approach using patterns to generate password candidates based on known password structures.

**Password Candidate**: A potential password being tested against a hash during the cracking process.

**Plaintext**: The original, unencrypted password that produces a given hash.

**Potfile**: A file storing previously cracked hashes and their plaintext passwords to avoid redundant work.

**Rainbow Table**: Pre-computed tables of hash-to-plaintext mappings (not used by hashcat/KrakenHashes).

**Rule**: A transformation applied to wordlist entries to generate password variants (e.g., appending numbers, capitalizing letters, character substitution).

**Rule Splitting**: KrakenHashes feature that divides large rule files into chunks for distributed processing across multiple agents.

**Salt**: Random data added to passwords before hashing to prevent identical passwords from producing identical hashes.

**Wordlist**: A file containing potential passwords, one per line, used as input for dictionary attacks.

## System Architecture Terms

### A-Z

**Agent**: A compute node running the KrakenHashes agent software that executes hashcat jobs and reports results to the backend.

**Agent Pool**: A group of agents that can be assigned to work together on jobs.

**API Key**: Authentication credential used by agents to communicate with the backend server.

**Backend**: The central KrakenHashes server that manages jobs, stores data, and coordinates agent activities.

**Claim Code**: A one-time voucher code used to register new agents with the system.

**Client**: In KrakenHashes context, a customer or engagement for which password cracking services are performed.

**Chunk**: A portion of work (keyspace segment or rule subset) assigned to an individual agent for processing.

**Chunking**: The process of dividing large cracking jobs into smaller segments for distributed processing.

**Data Retention**: Policies and mechanisms for automatically removing old data based on configured time periods.

**Heartbeat**: Regular status updates sent by agents to the backend to indicate they are alive and processing.

**Job**: A single password cracking task with specific parameters, wordlists, rules, and target hashes.

**Job Execution**: An instance of a job being run, which may involve multiple agents and chunks.

**Job Template**: A reusable job configuration that can be applied to different hashlists.

**Job Workflow**: A sequence of jobs designed to implement a comprehensive attack strategy.

**Preset**: Pre-configured job templates or workflows for common attack scenarios.

**Repository Pattern**: Software design pattern used in KrakenHashes for database access abstraction.

**Service Layer**: Business logic layer in the backend that processes requests between handlers and repositories.

**WebSocket**: Protocol used for real-time bidirectional communication between agents and the backend.

**Work Directory**: Temporary directory where agents store files and data during job execution.

## Security and Authentication Terms

### A-Z

**2FA/MFA**: Two-Factor/Multi-Factor Authentication requiring multiple verification methods for user login.

**Access Token**: Short-lived JWT token used for API authentication.

**API Authentication**: Token-based authentication system for programmatic access to KrakenHashes.

**Backup Codes**: One-time use codes for account recovery when primary MFA method is unavailable.

**Certificate Authority (CA)**: Entity that issues digital certificates for TLS/SSL encryption.

**CORS**: Cross-Origin Resource Sharing - security feature controlling which domains can access the API.

**JWT**: JSON Web Token - standard for securely transmitting information between parties as a JSON object.

**LDAP**: Lightweight Directory Access Protocol - external authentication system support.

**Rate Limiting**: Security measure limiting the number of API requests per time period.

**RBAC**: Role-Based Access Control - authorization system based on user roles (admin, user, agent, system).

**Refresh Token**: Long-lived token used to obtain new access tokens without re-authentication.

**Self-Signed Certificate**: TLS certificate signed by its creator rather than a trusted CA.

**Session Management**: System for tracking and controlling user login sessions.

**TLS/SSL**: Transport Layer Security/Secure Sockets Layer - encryption protocols for secure communication.

**TOTP**: Time-based One-Time Password - MFA method using authenticator apps.

**Voucher**: Authorization code for specific actions like agent registration or user invitation.

## Performance and Optimization Terms

### A-Z

**Benchmark Score**: Measured performance of hardware against specific hash algorithms.

**Cache**: Temporary storage of frequently accessed data to improve performance.

**Concurrency**: Number of simultaneous operations or connections the system can handle.

**GPU**: Graphics Processing Unit - primary hardware for high-speed password cracking.

**GPU Utilization**: Percentage of GPU resources being used during cracking operations.

**Hash Rate**: Speed of password testing, measured in hashes per second (H/s).

**Keyspace Distribution**: Method of dividing the total keyspace among multiple agents for parallel processing.

**Load Balancing**: Distribution of work across multiple agents based on their capabilities.

**Memory Usage**: RAM consumption by hashcat and the agent during operations.

**Optimization**: Techniques to improve cracking speed or resource efficiency.

**Parallel Processing**: Simultaneous execution of job chunks across multiple agents.

**Performance Metrics**: Measurements of system efficiency including hash rate, completion time, and resource usage.

**Resource Allocation**: Assignment of CPU, GPU, and memory resources to cracking operations.

**Thermal Throttling**: Automatic reduction in GPU performance to prevent overheating.

**Workload Distribution**: Strategy for assigning job chunks to agents based on their capabilities.

## Job and Task Statuses

### Job Execution Statuses

**Pending**: Job has been created but has not yet started execution. Waiting for available agents or higher priority jobs to complete.

**Running**: Job is actively executing with one or more tasks assigned to agents. Agents are processing the job's keyspace.

**Paused**: Job execution has been temporarily halted by user action. Tasks are stopped but can be resumed later.

**Processing**: Job has finished execution but is waiting for crack batches to be transmitted from agents and processed by the backend. Progress shows 100% but final completion is pending crack data receipt.

**Completed**: Job has finished all work, all crack batches have been received and processed, and email notifications (if enabled) have been sent.

**Failed**: Job encountered an unrecoverable error during execution and cannot continue.

**Cancelled**: Job was manually cancelled by a user before completion.

### Task Statuses

**Pending**: Task has been created but not yet assigned to an agent.

**Assigned**: Task has been assigned to a specific agent but the agent hasn't started executing it yet.

**Reconnect Pending**: Task was assigned to an agent that disconnected. Waiting for agent to reconnect or task to be reassigned.

**Running**: Task is actively executing on an agent. Hashcat is processing the assigned keyspace.

**Processing**: Task has completed hashcat execution but is waiting for all crack batches to be transmitted to the backend. Agent has signaled completion but crack data is still being sent.

**Completed**: Task has finished execution, all crack batches have been received, and the task is fully complete.

**Failed**: Task encountered an error during execution and could not complete successfully.

**Cancelled**: Task was manually cancelled before completion, either by user action or due to job cancellation.

## Common Abbreviations

### A-Z

**API**: Application Programming Interface

**CA**: Certificate Authority

**CLI**: Command Line Interface

**CPU**: Central Processing Unit

**CRUD**: Create, Read, Update, Delete (database operations)

**CSV**: Comma-Separated Values

**DB**: Database

**DNS**: Domain Name System

**DTO**: Data Transfer Object

**GPU**: Graphics Processing Unit

**H/s**: Hashes per second

**HTTP/HTTPS**: Hypertext Transfer Protocol (Secure)

**ID**: Identifier

**IP**: Internet Protocol

**JSON**: JavaScript Object Notation

**JWT**: JSON Web Token

**KH/s**: Kilohashes per second (1,000 H/s)

**LDAP**: Lightweight Directory Access Protocol

**MFA**: Multi-Factor Authentication

**MH/s**: Megahashes per second (1,000,000 H/s)

**NTLM**: NT LAN Manager (Windows password hash format)

**ORM**: Object-Relational Mapping

**OS**: Operating System

**RAM**: Random Access Memory

**RBAC**: Role-Based Access Control

**REST**: Representational State Transfer

**SHA**: Secure Hash Algorithm

**SMTP**: Simple Mail Transfer Protocol (email transmission protocol)

**SQL**: Structured Query Language

**SSL**: Secure Sockets Layer

**TLS**: Transport Layer Security

**TOTP**: Time-based One-Time Password

**UI/UX**: User Interface/User Experience

**URI/URL**: Uniform Resource Identifier/Locator

**UUID**: Universally Unique Identifier

**VRAM**: Video Random Access Memory (GPU memory)

**WS**: WebSocket

**XML**: Extensible Markup Language

---

*This glossary is continuously updated as new features and terminology are introduced to the KrakenHashes system.*