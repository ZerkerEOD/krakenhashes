# Preset Jobs and Job Workflows

## Overview

KrakenHashes provides two powerful features for standardizing and automating password cracking strategies:

- **Preset Jobs**: Pre-configured job templates that define specific attack strategies
- **Job Workflows**: Ordered sequences of preset jobs that execute systematically

These features allow administrators to create reusable attack strategies that can be consistently applied across different hashlists, ensuring thorough and efficient password recovery.

## Preset Jobs

### What are Preset Jobs?

Preset jobs are templates that encapsulate all the parameters needed for a specific password cracking attack. Think of them as recipes that define:
- Which wordlists to use
- Which rules to apply
- What attack mode to employ
- How much priority the job should have
- Various execution parameters

### Creating a Preset Job

To create a new preset job:

1. Navigate to **Admin** > **Preset Jobs**
2. Click the **"Create Preset Job"** button

<screenshot: Preset Jobs list page with Create button highlighted>

3. Fill in the preset job details:

#### Basic Information
- **Name**: A unique, descriptive name (e.g., "Common Passwords with Rules")
- **Attack Mode**: Select the hashcat attack mode (see Attack Modes section below)
- **Priority**: Set execution priority (0-1000, higher = more important)
- **Binary Version**: Select the hashcat binary version to use

<screenshot: Preset job form showing basic fields>

#### Attack-Specific Configuration
Based on the selected attack mode, different fields will appear:

- **Wordlists**: Select one or more wordlists (depending on attack mode)
- **Rules**: Select rule files to apply transformations
- **Mask**: Define patterns for brute force attacks (e.g., `?d?d?d?d` for 4 digits)

<screenshot: Attack mode dropdown with dynamic fields appearing>

#### Advanced Options
- **Chunk Size**: Time allocation per work unit (default: 900 seconds)
- **Small Job**: Check if this is a quick-running job
- **Allow High Priority Override**: Enable this job to interrupt lower priority running jobs (see details below)
- **Status Updates**: Enable real-time progress reporting

### High Priority Override Feature

The **Allow High Priority Override** option gives a preset job special privileges in the scheduling system:

#### What It Does
When enabled, this preset job can interrupt lower priority jobs that are currently running, but only when:
1. No agents are available for assignment
2. This job has a higher priority than running jobs
3. The system-wide "Job Interruption Enabled" setting is active

#### When to Use It
Enable high priority override for jobs that:
- Respond to security incidents or breaches
- Have strict compliance or legal deadlines
- Support time-sensitive investigations
- Require immediate results for critical business decisions

#### How Interruption Works
1. **Automatic Process**: The system automatically identifies the lowest priority running job
2. **Graceful Interruption**: Agents receive stop commands and save their progress
3. **Status Change**: Interrupted jobs change from "running" to "pending" status
4. **Automatic Resumption**: Interrupted jobs resume automatically when agents become available
5. **No Work Lost**: All completed work is preserved and jobs continue from their last checkpoint

#### Best Practices
- **Use Sparingly**: Reserve this feature for truly critical jobs
- **Set Appropriate Priority**: Jobs with override should have priority 70+ to justify interruption
- **Monitor Impact**: Track how often jobs are interrupted to ensure system efficiency
- **Document Usage**: Maintain clear policies about when to enable this feature

#### Example Scenarios

**Emergency Incident Response**:
- Priority: 95
- Allow High Priority Override: ✓ Yes
- Justification: Active security breach requires immediate password analysis

**Routine Security Audit**:
- Priority: 50
- Allow High Priority Override: ✗ No
- Justification: Standard assessment with flexible timeline

**Compliance Deadline Today**:
- Priority: 85
- Allow High Priority Override: ✓ Yes
- Justification: Regulatory requirement with penalty for non-compliance

### Attack Modes

KrakenHashes supports six different attack modes:

#### 1. Straight Attack (Mode 0)
The most common dictionary attack with optional rule transformations.
- **Requirements**: 1 wordlist, 0 or more rules
- **Example**: Using `rockyou.txt` with `best64.rule`

#### 2. Combination Attack (Mode 1)
Combines words from two different wordlists.
- **Requirements**: Exactly 2 wordlists, no rules
- **Example**: Combining `firstnames.txt` with `years.txt` to get "John2023"

<screenshot: Combination mode showing two wordlist dropdowns>

#### 3. Brute Force Attack (Mode 3)
Generates passwords based on mask patterns.
- **Requirements**: Mask pattern only, no wordlists
- **Common Masks**:
  - `?d?d?d?d` - 4 digits (0000-9999)
  - `?l?l?l?l?l?l` - 6 lowercase letters
  - `?u?l?l?l?d?d` - Capital + 3 lowercase + 2 digits

<screenshot: Mask field with pattern examples>

##### Increment Mode (Optional)

Enable increment mode for brute force attacks to automatically test mask patterns of increasing lengths:

- **Increment Mode**: Toggle to enable (`increment` or `increment_inverse`)
- **Increment Min**: Minimum mask length to start with (default: 1)
- **Increment Max**: Maximum mask length to end with (default: mask length)

**Example**: Mask `?d?d?d?d` with min=2, max=4 tests:

1. `?d?d` (00-99)
2. `?d?d?d` (000-999)
3. `?d?d?d?d` (0000-9999)

**Increment Inverse**: Starts from longest mask and works down to shortest. Useful when longer passwords are more likely.

!!! tip "When to Use Increment Mode"
    - **PIN Discovery**: Testing 4-8 digit PINs with a single preset job
    - **Length Exploration**: When password length policies are unknown
    - **Exhaustive Searches**: Covering all lengths up to a maximum systematically

!!! note "Technical Details"
    KrakenHashes decomposes increment mode jobs into discrete "layers" (one per mask length) for efficient distributed processing. Each layer is scheduled and tracked independently, allowing multiple agents to work on different layers in parallel. See [Increment Mode Architecture](../../reference/architecture/increment-mode.md) for implementation details.

#### 4. Hybrid Wordlist + Mask (Mode 6)
Appends mask-generated characters to dictionary words.
- **Requirements**: 1 wordlist and mask pattern
- **Example**: `passwords.txt` + `?d?d?d` = "password123"

#### 5. Hybrid Mask + Wordlist (Mode 7)
Prepends mask-generated characters to dictionary words.
- **Requirements**: 1 wordlist and mask pattern
- **Example**: `?d?d?d` + `passwords.txt` = "123password"

#### 6. Association Attack (Mode 9)
*Currently not implemented*

### Managing Preset Jobs

#### Viewing Preset Jobs
The preset jobs list shows:
- Name and attack mode
- Wordlist and rule counts
- Priority level
- Binary version
- Action buttons (Edit/Delete)

![Preset Job Management](../../assets/images/screenshots/preset_jobs_management.png)
*Preset Job Management interface showing three configured preset jobs with their attack modes, priorities, agent limits, and resource assignments*

#### Editing Preset Jobs
1. Click the **Edit** button on any preset job
2. Modify the desired fields
3. Click **Update Preset Job**

#### Deleting Preset Jobs
- Click the **Delete** button
- Confirm the deletion
- Note: You cannot delete preset jobs that are used in workflows

## Job Workflows

### What are Job Workflows?

Job workflows are ordered sequences of preset jobs that execute one after another. They allow you to:
- Create comprehensive attack strategies
- Ensure consistent methodology
- Prioritize efficient attacks first
- Automate complex multi-stage attacks

### Creating a Job Workflow

1. Navigate to **Admin** > **Job Workflows**
2. Click **"Create Job Workflow"**

![Job Workflows](../../assets/images/screenshots/workflows_management.png)
*Job Workflows interface displaying a workflow with multiple jobs, priority indicators, and interruption capabilities*

3. Enter a workflow name (e.g., "Standard Password Audit")
4. Add preset jobs to the workflow:
   - Type in the search box to find preset jobs
   - Click on a preset job to add it as a step
   - Added jobs appear in the steps list below

<screenshot: Workflow form with autocomplete search and steps list>

5. Arrange the execution order:
   - Steps are automatically sorted by priority (highest first)
   - Within the same priority, they execute in the order added

6. Click **Create Job Workflow**

### Understanding Workflow Execution

When a workflow runs:
1. All preset jobs are queued in priority order
2. Higher priority jobs execute first
3. Jobs with the same priority run in sequence
4. Each job completes before the next begins

### Managing Workflows

#### Viewing Workflows
The workflow list displays:
- Workflow name
- Number of steps
- Creation date
- Action buttons

<screenshot: Workflow list showing multiple workflows>

#### Workflow Details
Click on a workflow name to see:
- Complete list of preset jobs in order
- Priority of each step
- Wordlists and rules for each step

<screenshot: Detailed workflow view>

#### Editing Workflows
1. Click **Edit** on any workflow
2. Add or remove preset jobs
3. Reorder steps as needed
4. Click **Update Job Workflow**

Note: When updating a workflow, all existing steps are replaced with the new configuration.

## Best Practices

### Preset Job Design

1. **Start Simple**: Begin with common, fast attacks
   - Straight attack with common passwords
   - Small, targeted wordlists with effective rules

2. **Progressive Complexity**: Order jobs from fast/likely to slow/exhaustive
   - Priority 100: Common passwords
   - Priority 80: Leaked password lists
   - Priority 60: Hybrid attacks
   - Priority 40: Targeted brute force
   - Priority 20: Exhaustive searches

3. **Naming Conventions**: Use descriptive names that indicate:
   - Attack type
   - Target pattern
   - Approximate runtime

### Workflow Strategy

1. **Standard Workflows**: Create templates for common scenarios
   - "Quick Audit" - Fast, high-probability attacks
   - "Comprehensive Audit" - Thorough multi-day approach
   - "Compliance Check" - Specific policy violations

2. **Resource Management**:
   - Group small jobs together
   - Save intensive attacks for last
   - Use chunk sizes appropriate to job complexity

3. **Monitoring**: Enable status updates for long-running jobs

## Examples

### Example 1: Quick Security Audit Workflow

Create these preset jobs:
1. **"Top 1000 Passwords"** (Priority: 100)
   - Mode: Straight
   - Wordlist: top1000.txt
   - No rules

2. **"Common with Basic Rules"** (Priority: 90)
   - Mode: Straight
   - Wordlist: common-passwords.txt
   - Rules: basic.rule

3. **"4-6 Digit PINs"** (Priority: 80)
   - Mode: Brute Force
   - Mask: `?d?d?d?d?d?d`
   - Increment Mode: enabled
   - Increment Min: 4
   - Increment Max: 6

### Example 2: Targeted Corporate Audit

1. **"Corporate Dictionary"** (Priority: 100)
   - Mode: Straight
   - Wordlist: corporate-terms.txt
   - Rules: best64.rule

2. **"Names + Years"** (Priority: 90)
   - Mode: Combination
   - Wordlist 1: employee-names.txt
   - Wordlist 2: years-2020-2024.txt

3. **"Corporate + Numbers"** (Priority: 80)
   - Mode: Hybrid (Wordlist + Mask)
   - Wordlist: corporate-terms.txt
   - Mask: ?d?d?d

## Troubleshooting

### Common Issues

1. **"Wordlist not found"**: Ensure wordlists are uploaded before creating preset jobs
2. **"Invalid mask pattern"**: Check mask syntax (?d=digit, ?l=lowercase, ?u=uppercase, ?s=special)
3. **"Priority exceeds maximum"**: Check system settings for max priority value

### Performance Tips

- Use smaller, targeted wordlists for initial attempts
- Apply rules selectively - more isn't always better
- Set appropriate chunk sizes (larger for simple attacks, smaller for complex)
- Monitor system resources when running multiple workflows

## Future Enhancements

The preset jobs and workflows system is designed to integrate with:
- Automated job distribution to agents
- Real-time progress monitoring
- Success rate analytics
- Dynamic workflow optimization

As KrakenHashes evolves, these features will provide the foundation for intelligent, adaptive password auditing strategies.