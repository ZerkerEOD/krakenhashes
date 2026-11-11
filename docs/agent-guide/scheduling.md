# Agent Scheduling

!!! info "See also"
    Schedules are set per agent from the admin side: [Admin guide → Agent scheduling](../admin-guide/operations/scheduling.md). This page explains how the agent honours them.

## Overview

The Agent Scheduling feature in KrakenHashes allows administrators to define specific time windows when agents are available for job execution. This feature helps optimize resource usage, manage electricity costs, and ensure agents run during appropriate hours.

## Key Features

- **Daily Schedule Configuration**: Set different working hours for each day of the week
- **Timezone Support**: Schedules are configured in the user's local timezone but stored in UTC
- **Overnight Schedule Support**: Schedules can span midnight (e.g., 22:00 - 02:00)
- **Global Enable/Disable**: System-wide toggle to enable or disable all scheduling
- **Per-Agent Control**: Each agent can have scheduling enabled or disabled independently
- **Schedule Preservation**: Schedules are preserved even when disabled

## How It Works

### Schedule Enforcement

When scheduling is enabled:
1. The system checks if global scheduling is enabled (admin setting)
2. The system checks if the individual agent has scheduling enabled
3. The system checks if the current UTC time falls within the agent's schedule
4. Only agents that pass all checks are assigned jobs

### Time Storage and Display

- **Storage**: All times are stored in UTC in the database
- **Display**: Times are shown in the user's local timezone in the UI
- **Conversion**: Automatic conversion happens between local and UTC times

## Configuration

### Global Settings

The global scheduling setting can be found in **Admin Panel → System Settings**:

```
Enable Agent Scheduling System: [Toggle]
```

When disabled:
- All agent schedules are ignored
- Agents are always available for jobs
- Individual agent schedules are preserved but not enforced

### Per-Agent Configuration

Individual agent scheduling is configured on the agent details page:

1. Navigate to **Agents → [Agent Name]**
2. Find the **Scheduling** section
3. Toggle **Enable Scheduling** to activate scheduling for this agent
4. Click **Edit All Schedules** to configure daily schedules

### Schedule Configuration

When editing schedules:

1. **Add Schedule**: Click "Add Schedule" for any day to create a time window
2. **Set Times**: Enter start and end times in 24-hour format (HH:MM)
3. **Active Toggle**: Enable/disable the schedule for specific days
   - **Active (ON)**: Agent works during the specified hours
   - **Active (OFF)**: Agent does not work at all on this day
4. **Copy Schedule**: Use the copy icon to apply one day's schedule to all other days
5. **Delete Schedule**: Remove a schedule for a specific day

### Time Input Formats

The system accepts various time formats:
- `9` → `09:00:00`
- `17` → `17:00:00`
- `9:30` → `09:30:00`
- `09:00` → `09:00:00`
- `09:00:00` → `09:00:00`

## Examples

### Standard Business Hours (9-5, Monday-Friday)

```
Monday:    09:00 - 17:00 [Active]
Tuesday:   09:00 - 17:00 [Active]
Wednesday: 09:00 - 17:00 [Active]
Thursday:  09:00 - 17:00 [Active]
Friday:    09:00 - 17:00 [Active]
Saturday:  Not scheduled
Sunday:    Not scheduled
```

### 24/7 Operation with Weekend Maintenance

```
Monday:    00:00 - 23:59 [Active]
Tuesday:   00:00 - 23:59 [Active]
Wednesday: 00:00 - 23:59 [Active]
Thursday:  00:00 - 23:59 [Active]
Friday:    00:00 - 23:59 [Active]
Saturday:  00:00 - 06:00 [Active]  # Maintenance window 6 AM - Midnight
Sunday:    Not scheduled            # Full day maintenance
```

### Overnight Processing

```
Monday:    22:00 - 06:00 [Active]  # Runs overnight Mon-Tue
Tuesday:   22:00 - 06:00 [Active]  # Runs overnight Tue-Wed
Wednesday: 22:00 - 06:00 [Active]  # Runs overnight Wed-Thu
Thursday:  22:00 - 06:00 [Active]  # Runs overnight Thu-Fri
Friday:    22:00 - 06:00 [Active]  # Runs overnight Fri-Sat
Saturday:  Not scheduled
Sunday:    22:00 - 06:00 [Active]  # Runs overnight Sun-Mon
```

## Important Behavior Notes

### Running Jobs Are Not Interrupted

**The scheduling system only controls when new jobs are assigned, not when running jobs must complete.**

Key points:
- Schedules determine when an agent can **receive** new jobs
- Running jobs will **always complete**, even if they extend past the scheduled end time
- The agent will not accept new jobs outside its schedule, but will finish current work

#### Example Scenario

If an agent is scheduled to work until 17:00:
- At 16:59, the agent receives a job configured for 1-hour chunks
- The job will run to completion, potentially until 17:59 or later
- No new jobs will be assigned after 17:00
- The agent becomes available for new work at the next scheduled window

This design ensures:
- No work is lost due to scheduling boundaries
- Jobs complete successfully without interruption
- Predictable behavior for long-running tasks

## Schedule Priority

The scheduling system follows this priority order:

1. **Global Setting OFF**: All schedules ignored, all agents always available
2. **Global Setting ON + Agent Scheduling OFF**: Agent always available
3. **Global Setting ON + Agent Scheduling ON**: Agent follows configured schedule

## Technical Details

### Database Schema

Schedules are stored in the `agent_schedules` table:

```sql
CREATE TABLE agent_schedules (
    id SERIAL PRIMARY KEY,
    agent_id INTEGER NOT NULL REFERENCES agents(id),
    day_of_week INTEGER NOT NULL,  -- 0-6 (Sunday-Saturday)
    start_time TIME NOT NULL,       -- UTC time
    end_time TIME NOT NULL,         -- UTC time
    timezone VARCHAR(50) NOT NULL,  -- Original timezone for reference
    is_active BOOLEAN NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

### API Endpoints

- `GET /api/agents/{id}/schedules` - Get agent schedules
- `POST /api/agents/{id}/schedules` - Update single schedule
- `POST /api/agents/{id}/schedules/bulk` - Bulk update schedules
- `DELETE /api/agents/{id}/schedules/{day}` - Delete schedule for a day
- `PUT /api/agents/{id}/scheduling-enabled` - Toggle scheduling for agent

### Job Assignment Integration

The job assignment service (`GetAvailableAgents`) checks scheduling:

```go
if agent.SchedulingEnabled {
    schedulingSetting, err := s.systemSettingsRepo.GetSetting(ctx, "agent_scheduling_enabled")
    if err == nil && schedulingSetting.Value != nil && *schedulingSetting.Value == "true" {
        isScheduled, err := s.scheduleRepo.IsAgentScheduledNow(ctx, agent.ID)
        if err != nil || !isScheduled {
            continue // Skip this agent
        }
    }
}
```

## Best Practices

1. **Test Schedules**: Always test schedules with non-critical jobs first
2. **Timezone Awareness**: Be mindful of timezone differences when setting schedules
3. **Overlap Planning**: Ensure adequate agent coverage during peak hours
4. **Maintenance Windows**: Schedule maintenance during off-hours
5. **Documentation**: Document your scheduling strategy for team members

## Troubleshooting

### Agent Not Getting Jobs Despite Being Scheduled

1. Check global scheduling is enabled
2. Verify agent scheduling is enabled
3. Confirm current time falls within schedule
4. Check agent is otherwise eligible (enabled, online, etc.)

### Schedule Shows Wrong Times

1. Verify your browser timezone is correct
2. Check the timezone display in the UI
3. Remember all times are stored in UTC

### Overnight Schedules Not Working

1. Ensure end time is properly set for next day
2. Verify the schedule spans midnight correctly
3. Check both days involved in the overnight schedule

## Future Enhancements

Planned improvements for the scheduling system:

- Holiday calendar integration
- Schedule templates for common patterns
- Bulk schedule management across multiple agents
- Schedule conflict detection and warnings
- Historical schedule effectiveness reporting