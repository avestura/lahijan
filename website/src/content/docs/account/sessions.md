---
title: Sessions
description: How long a dashboard sign-in lasts, how signing out works and what the Sessions page can and cannot do today.
---

A session is created each time you sign in to the dashboard. This page explains how long a session lasts, how to end it and what is not available yet.

## How a session works

When you sign in, Lahijan sets two cookies in your browser:

| Cookie  | Default name      | Purpose                                   | Default lifetime                                |
| ------- | ----------------- | ----------------------------------------- | ----------------------------------------------- |
| Session | `lahijan_session` | Identifies your session on every request. | 24 hours (`auth.session.lifetimeSeconds`)       |
| Refresh | `lahijan_refresh` | Lets the dashboard renew its credentials. | 30 days (`auth.session.refreshLifetimeSeconds`) |

Both cookies are `HttpOnly`, so scripts in the page cannot read them. Your operator sets the names, lifetimes and cookie flags; the values above are the defaults.

A session ends 24 hours (by default) after you sign in. When the dashboard gets a `401`, it calls `POST /api/v1/auth/refresh` once. Refreshing replaces the refresh token with a new one, but it does not move the session's end time, so after the session lifetime you sign in again.

Lahijan protects the refresh token against theft: if an old, already-replaced refresh token is presented again, Lahijan treats it as stolen, ends that session and revokes all its refresh tokens. You then need to sign in again.

Each session records the browser's user agent, your IP address and when it was last used.

## Sign out

Select **Sign out** in the dashboard. This calls `POST /api/v1/auth/logout`, which ends the current session, revokes its refresh tokens and clears the cookies.

With the API, send the refresh token in the body (or let the browser send the refresh cookie):

```sh
curl -X POST https://cloud.example.com/api/v1/auth/logout \
  -H "Content-Type: application/json" \
  -d '{"refreshToken": "<refresh token>"}'
```

Sign-out always answers success, even if there was nothing to revoke.

## The Sessions page

**Settings > Sessions** (titled **Active sessions**) is a placeholder in this release. It shows the notice "Per-session listing lands with a follow-up auth API." There is no API to list your sessions or to end a session other than the one you are using.

What this means in practice:

- You cannot see which other devices are signed in.
- You cannot sign out a lost or stolen device from another device. Its session stays valid until its lifetime runs out (24 hours by default).
- Changing or resetting your password does not end other sessions either.

> [!TIP]
> If you think someone else has your session, change your password so they cannot sign in again, and ask your operator to revoke the session on the server. Sessions last at most `auth.session.lifetimeSeconds`, so shortening that setting limits how long a stolen session can be used.

## Sessions and access tokens

Access tokens are separate from sessions. They do not expire when you sign out, and signing out does not affect them. Manage them on [Access tokens](/docs/account/access-tokens).
