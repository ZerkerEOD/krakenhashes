# Teams

Teams let an organization partition who can see which clients, hashlists, jobs, and cracked
data. When your administrator turns on **Multi-Team Mode**, KrakenHashes stops showing every
user everything and instead scopes what you see to the teams you belong to.

This page explains what teams mean for you as a day-to-day user. If you administer the system
(enabling the feature, creating teams, assigning clients), see
[Team Management](../admin-guide/operations/team-management.md) in the Admin Guide.

## When teams are active

Teams are controlled by a single system-wide setting, **Multi-Team Mode**, that an administrator
enables. You can tell which mode you are in:

- **Multi-Team Mode off (default):** Every user can see all clients, hashlists, and jobs. There
  is no **Teams** entry in the sidebar and no team filter. This is the original, "everyone shares
  everything" behavior.
- **Multi-Team Mode on:** A **Teams** item appears in the navigation sidebar, a **Team** filter
  dropdown appears at the top of pages like Jobs, and what you can access is limited to your
  team's data.

!!! info "You don't toggle this yourself"
    Multi-Team Mode is a system-wide administrator setting, not a per-user preference. If you
    expect to see teams but don't, ask an administrator whether the feature is enabled.

## What a team gives you access to

Access flows down a chain. A team is granted access to one or more **clients**; everything that
hangs off those clients follows:

```
Team  →  Client  →  Hashlist  →  Job & cracked passwords
```

If you are a member of a team, and that team is assigned a client, then you can see and work with
that client's hashlists, the jobs run against them, and the passwords cracked from them. If a
client is **not** assigned to any team you belong to, it is invisible to you.

!!! note "Legacy hashlists without a client"
    A hashlist that was uploaded without being linked to a client is **private to the user who
    uploaded it**, even when Multi-Team Mode is on. Administrators can always access everything
    regardless of teams.

## Your role in a team

Every membership has one of two roles:

| Role | What you can do |
|------|-----------------|
| **Member** | Access the team's clients, hashlists, jobs, and cracked data. Use the team's (and trusted teams') agents to run jobs. |
| **Admin** (Team Manager) | Everything a member can do, **plus** manage the team: add/remove members, change member roles, edit the team's name and description, and manage trust relationships with other teams. |

A few rules worth knowing:

- **Any user can create a team**, and the creator automatically becomes that team's admin.
- You can belong to **multiple teams** at once; your access is the union of all of them.
- A team must always keep at least one admin — the system prevents removing or demoting the last
  admin of a team.
- **Assigning clients to teams** and **deleting teams** are reserved for system administrators
  (not team admins).

## Finding and using your teams

### My Teams

Open **Teams** in the sidebar to see the teams you belong to. Each team shows quick counts of its
members, clients, hashlists, and agents, and an **Admin** badge on any team you manage. Click
**View Details** to open a team.

### The team detail page

A team's detail page has four tabs:

- **Members** — who is on the team and their roles. Team admins can add members (search by
  username or email), change a member's role between Member and Admin, or remove members.
- **Clients** — the clients this team can access. Only system administrators can assign or remove
  clients here; members and team admins can view them and jump to a client's page.
- **Trusted Teams** — other teams whose agents are allowed to run this team's jobs (see
  [Sharing agents with trust](#sharing-agents-with-trust) below).
- **Agents** — every agent this team can run jobs on, labeled **Direct** (owned by a team member)
  or **Trusted** (shared from a team you trust), with each agent's status, version, and owner.

### The Team filter

When Multi-Team Mode is on and you belong to more than one team, a **Team** dropdown appears at
the top of list pages (such as Jobs). Choose **All Teams** to see everything you have access to,
or pick a single team to narrow the view to just that team's work.

### Choosing a team when uploading

If you belong to more than one team, the hashlist upload form shows a **Team** selector so you can
decide which team's context a new hashlist belongs to. If you are on only one team, this is handled
for you automatically.

## The Default Team

KrakenHashes always keeps a built-in **Default Team** as a safety net. When Multi-Team Mode is
turned on, any client that wasn't assigned to a team is placed in the Default Team so it never
becomes orphaned (invisible to everyone). By default only administrators have access to the Default
Team. It cannot be deleted.

## Sharing agents with trust

Each team runs jobs on its own agents — agents owned by its members. Sometimes one team needs to
borrow another team's compute. **Trust** makes that possible, and it is deliberately **one-way**:

> If **Team A trusts Team B**, then **Team B's agents may run Team A's jobs**. It does **not** give
> Team A's agents access to Team B's jobs.

Team admins manage trust from the **Trusted Teams** tab. System-owned agents belong to the Default
Team, so to use them a team trusts the Default Team. For the full agent-ownership and trust model,
see [Team Management — Agents and trust](../admin-guide/operations/team-management.md#agent-visibility-and-ownership).

## See also

- [Team Management](../admin-guide/operations/team-management.md) — administrator guide to enabling
  the feature, creating teams, and assigning clients
- [Core Concepts](core-concepts.md) — fundamental KrakenHashes terminology
- [Managing Hashlists](hashlists.md) — uploading and organizing hashes
- [Client Management](../admin-guide/operations/clients.md) — how clients organize work
