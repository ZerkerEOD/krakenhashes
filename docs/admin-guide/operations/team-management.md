# Team Management

Multi-Team Mode partitions KrakenHashes so that users only see the clients, hashlists, jobs, and
cracked data belonging to teams they are members of. It also controls which agents may run which
jobs. This guide covers enabling the feature and administering teams.

For an end-user's perspective on what teams mean day to day, see the
[Teams](../../user-guide/teams.md) user guide.

## Overview

- **Purpose:** Isolate work between groups (internal squads, customers, engagements) so members of
  one team cannot see another team's data.
- **Default state:** Off. With Multi-Team Mode disabled, every user can access every client,
  hashlist, and job — the original shared-visibility behavior.
- **Access model:** Access flows `Team → Client → Hashlist → Job`. A team is granted access to
  clients; everything attached to those clients follows.
- **Administrators bypass teams:** System administrators can always access all data regardless of
  team membership.

## Enabling Multi-Team Mode

The feature is governed by the `teams_enabled` system setting.

1. Go to **Admin → Settings → Authentication**.
2. Find the **Multi-Team Mode** toggle and switch it to **Enabled**.

When you enable the feature, KrakenHashes automatically assigns every client that isn't already in
a team to the **Default Team**. This guarantees no client becomes orphaned (invisible to everyone)
the moment access scoping turns on.

!!! warning "Plan before enabling in production"
    The instant Multi-Team Mode is on, non-admin users lose visibility of any client not assigned
    to one of their teams. Create your teams and assign clients **first**, or be ready to do so
    immediately, so users aren't locked out of work they need.

Disabling the toggle reverts the system to global visibility — all data becomes visible to all
users again. Team definitions, memberships, and client assignments are preserved, so re-enabling
restores the previous scoping.

!!! note "Setting propagation"
    The `teams_enabled` value is cached briefly (a few seconds) for performance, so a toggle can
    take a moment to take effect everywhere.

## Concepts

### Teams and roles

A team has a unique name, an optional description, and a set of members. Each membership has a role:

| Role | Capabilities |
|------|--------------|
| **Member** | Access the team's clients, hashlists, jobs, and cracked data; run jobs on the team's (and trusted teams') agents. |
| **Admin** (Team Manager) | All member capabilities, plus manage the team: add/remove members, change roles, edit the team, and manage trust relationships. |

- Any authenticated user can create a team and becomes its admin.
- A user can belong to multiple teams; effective access is the union.
- **Assigning clients to teams** and **deleting teams** are restricted to **system administrators**,
  even though team admins manage everything else about their team. This prevents a team admin from
  granting their team access to clients they shouldn't see (privilege escalation).

### The Default Team

The **Default Team** (`00000000-0000-0000-0000-000000000001`) is a built-in safety net:

- It receives any unassigned client when Multi-Team Mode is enabled.
- It receives clients that would otherwise be orphaned when a team is deleted or a client is removed
  from its last team.
- **System-owned agents** belong to the Default Team; other teams trust the Default Team to use them.
- It **cannot be deleted** and by default only administrators have access to it.

### Access chain

```
Team  →  Client (via client_teams)  →  Hashlist  →  Job & cracked passwords
```

A hashlist uploaded without a client is private to its uploader even with teams on. Administrators
always have full access.

## Managing teams

Open **Teams** in the sidebar. As a system administrator you see a **Team Management** table listing
every team with member, client, hashlist, and agent counts.

### Create a team

Click **Create Team**, enter a name (required, unique) and optional description. The creator is added
as the team's admin.

### Edit a team

Use the **Edit** (pencil) action to change a team's name or description. Team admins can also edit
their own team from the team detail page.

### Delete a team

Use the **Delete** action (system administrators only). The Default Team cannot be deleted. Deletion
is transactional and protects against data loss:

1. Clients that belong **only** to the team being deleted are reassigned to the Default Team.
2. All of the team's client assignments are removed.
3. The team (and its memberships) is deleted.

Clients that also belong to other teams keep those other assignments and are left untouched.

## Managing members

Open a team and use the **Members** tab.

- **Add a member:** Click **Add Member**, search by username or email (type at least two
  characters), choose a role (**Member** or **Admin**), and confirm.
- **Change a role:** Use the inline role dropdown on each member row.
- **Remove a member:** Use the delete (trash) action.

!!! warning "Last-admin protection"
    A team must always retain at least one admin. Attempts to remove or demote the **last** admin
    are rejected. These checks are serialized with row-level locking, so two concurrent admins
    can't both remove the last admin.

## Assigning clients to teams

Granting a team access to a client is the core of access control, and is **system-administrator
only**. There are two ways to do it.

### From the team

On a team's **Clients** tab, click **Assign Client** and pick an unassigned client. Assigning a
client grants every member of that team access to the client's hashlists, jobs, and cracked data.

### From Client Management (bulk)

The [Client Management](clients.md) page supports assigning a client to a team — and **bulk
assignment** of many clients to one team at once — which is the fastest way to set up scoping after
first enabling the feature.

### Removing a client

Removing a client from a team revokes that team's access. If the team was the client's **only**
team, the client is automatically reassigned to the Default Team so it is never orphaned. A client
may belong to multiple teams simultaneously.

## Agent visibility and ownership

Which agents a team can run jobs on is derived from agent ownership and trust.

- **Direct agents** are owned by the team's members (or explicitly assigned to the team). These are
  the team's own compute.
- **System agents** (owned by the system, with no user owner) belong to the **Default Team**. To
  use them, a team must trust the Default Team.
- An agent's effective teams come from its owner's team memberships, unless an administrator has set
  an explicit team override on the agent.

The team detail page's **Agents** tab lists every agent the team can use, tagged **Direct** or
**Trusted**, with status, version, and owner.

!!! note "Agent ownership and the owner field"
    When Multi-Team Mode is on, changing an agent's owner is restricted to administrators, because
    ownership determines team access. See [Agent Management](agents.md).

## Trust relationships

Trust lets one team borrow another team's agents. It is **directional (one-way)**:

> **Team A trusts Team B** ⇒ **Team B's agents may run Team A's jobs.** It does *not* let Team A's
> agents run Team B's jobs.

A team's admins (or a system administrator) manage trust from the team's **Trusted Teams** tab via
**Add Trust** / remove. A team cannot trust itself. Removing trust stops the formerly trusted team's
agents from picking up this team's jobs.

The scheduler grants an agent access to a job when **either**:

1. **Direct match** — the agent is in the same team as the job, **or**
2. **Trust match** — the job's team trusts one of the agent's teams.

## Permissions summary

| Action | Teams off | Member | Team Admin | System Admin |
|--------|-----------|--------|-----------|--------------|
| See a team's clients / hashlists / jobs | All data | ✓ (own teams) | ✓ | ✓ |
| Create team | ✓ | ✓ | ✓ | ✓ |
| Edit team name/description | — | ✗ | ✓ (own) | ✓ |
| Add / remove members, change roles | — | ✗ | ✓ (own) | ✓ |
| Manage trust relationships | — | ✗ | ✓ (own) | ✓ |
| Assign / remove clients to a team | — | ✗ | ✗ | ✓ |
| Delete a team | — | ✗ | ✗ | ✓ |
| Toggle Multi-Team Mode | — | — | — | ✓ |

## API reference

Team endpoints live under the JWT-protected API; team-admin actions are enforced in the service
layer, and client assignment/deletion require a system administrator.

**Authenticated user / team endpoints**

| Method & Path | Description |
|---------------|-------------|
| `GET /api/teams` | List the current user's teams (all teams for admins) |
| `POST /api/teams` | Create a team (creator becomes admin) |
| `GET /api/teams/{id}` | Team details |
| `PUT /api/teams/{id}` | Update a team (team admin or system admin) |
| `GET /api/teams/names` | Lightweight team-name list (for the trust picker) |
| `GET /api/teams/{id}/members` | List members |
| `POST /api/teams/{id}/members` | Add a member |
| `PUT /api/teams/{id}/members/{userId}` | Change a member's role |
| `DELETE /api/teams/{id}/members/{userId}` | Remove a member |
| `GET /api/teams/{id}/clients` | List the team's clients |
| `POST /api/teams/{id}/clients/{clientId}` | Assign a client (system admin) |
| `DELETE /api/teams/{id}/clients/{clientId}` | Remove a client (system admin) |
| `GET /api/teams/{id}/agents` | List accessible agents (direct + trusted) |
| `GET /api/teams/{id}/trust` | List trusted teams |
| `POST /api/teams/{id}/trust/{trustedTeamId}` | Add a trust relationship |
| `DELETE /api/teams/{id}/trust/{trustedTeamId}` | Remove a trust relationship |
| `GET /api/users/search?q=&team_id=` | Search users to add to a team |
| `GET /api/settings/teams_enabled` | Whether Multi-Team Mode is on |

**Admin endpoints**

| Method & Path | Description |
|---------------|-------------|
| `GET /api/admin/teams` | List all teams with counts |
| `POST /api/admin/teams` | Create a team |
| `PUT /api/admin/teams/{id}` | Update a team |
| `DELETE /api/admin/teams/{id}` | Delete a team (with orphan reassignment) |
| `GET /api/admin/settings/teams_enabled` | Read the toggle |
| `PUT /api/admin/settings/teams_enabled` | Enable/disable Multi-Team Mode |

## See also

- [Teams (user guide)](../../user-guide/teams.md) — what teams mean for everyday users
- [Client Management](clients.md) — clients, bulk team assignment, potfiles, and retention
- [User Management](users.md) — user accounts and roles
- [Agent Management](agents.md) — agent ownership and assignment
