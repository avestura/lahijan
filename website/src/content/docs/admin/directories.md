---
title: Directories (LDAP and SAML)
description: Connect an LDAP server to import users and groups, or a SAML identity provider for single sign-on, from the dashboard.
---

A directory connection ties Lahijan to the place where your organisation already keeps its people. There are two kinds:

- **LDAP** (including Active Directory): people sign in to Lahijan with their directory email and password, and Lahijan can import users and groups from the server with **Sync now**.
- **SAML**: an identity provider (IdP) such as Microsoft Entra ID, Okta or Keycloak signs your users in. Lahijan records which connection each user came from and which groups the IdP says they belong to.

Connections are managed by platform administrators at **Administration > Directories** (`/admin/directory`). They belong to the whole platform, not to one tenant, because users are global. The page needs the `platform.directory.manage` permission, which only `platform.admin` holds. Every create, change, test and sync is written to the [audit log](/docs/audit/overview).

## Add an LDAP connection

1. Open **Administration > Directories** and select **New connection**.
2. Choose **LDAP** and give the connection a **Name**: lowercase letters, digits, `-` and `_`.
3. Fill in the **Server** section:

   | Field                                 | Meaning                                                                                                          |
   | ------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
   | **Server URL**                        | `ldaps://host:636`, or `ldap://host:389` together with **Upgrade with STARTTLS**.                                |
   | **Bind DN** and **Bind password**     | A read-only service account used to search. Leave both empty to search anonymously.                              |
   | **Skip TLS certificate verification** | Turns off certificate checks. Use it only on a test network: anyone on the path can then impersonate the server. |

4. Fill in **Users**: the **User base DN** to search, a **User filter** (default `(objectClass=person)`), and the attributes that hold the **Email** (default `mail`) and **Display name** (default `displayName`, falling back to `cn`).
5. Optionally fill in **Groups**: a **Group base DN**, a **Group filter** (default `(objectClass=groupOfNames)`), the **Group name attribute** (default `cn`) and the **Group member attribute** (default `member`). Leave the group base DN empty to skip groups.
6. Under **Sign-in**, choose whether to **Create an account on first sign-in** (on by default); see [Sign in with LDAP](#sign-in-with-ldap).
7. Select **Test connection**. Lahijan connects, signs in and runs both searches, then reports how many users and groups it found (counting up to 50) or why it failed. Fix any problem before you save.
8. Select **Save**.

The bind password is encrypted with `auth.secrets.encryptionKey` before it is stored and is never shown again. When you edit a connection, leave the password empty to keep the stored one. Outside the `dev` environment, saving a password fails if that key is not configured. See [Configuration](/docs/reference/configuration).

### Sync users and groups

Select **Sync now** on a connection (it must be enabled). Lahijan then:

- **Creates** a Lahijan user for every entry that has an email address. New users have a verified email and no password, and, when `auth.signup.personalTenant` is on, a personal tenant, exactly like a self-registered account.
- **Links** an entry to an existing user when the email matches. The existing account's password and settings are not changed.
- **Skips** entries without an email address and counts them in the summary.
- **Imports groups** with their members. A group member is matched to a user by its distinguished name, ignoring case and spaces; members that are not among the imported users are ignored. Groups that disappeared from the directory are removed.

A sync never disables or deletes users. If someone leaves your organisation, disable them in [user management](/docs/admin/users-and-tenants#manage-users-in-the-dashboard). Running a sync again is safe: it updates the same users and groups instead of duplicating them.

The connection row shows the time and outcome of the last sync, how many users and groups it covered and, after a failure, the error. Select **Groups** to see the imported groups and their member counts. Imported users appear in **Administration > Users** with the connection's name in the **Source** column.

## Sign in with LDAP

While an LDAP connection is **enabled**, people sign in on the normal sign-in page with their directory **email address** and **password**. Nothing else needs to be set up, and you do not have to run a sync first.

When someone signs in, Lahijan:

1. Checks the **local password** first, if the account has one. A correct local password signs the user in without contacting the directory.
2. Otherwise tries each enabled LDAP connection, oldest first. It searches the **User base DN** (with the **User filter**) for the one entry whose email attribute equals the address, then proves the password by binding to the directory as that entry. The bind password of the connection's service account is used only for the search. If zero or more than one entry matches, the sign-in is refused.
3. Finds the Lahijan account. An existing account with that email is **linked** to the directory entry and its local password is left untouched. If there is none, an account is **created** (verified email, no password, personal tenant when `auth.signup.personalTenant` is on), unless you turned off **Create an account on first sign-in** on the connection, in which case only existing accounts can sign in.
4. Applies the usual account checks: a disabled account is refused, and a second factor is still asked for when the account or tenant requires one.

A wrong password, an unknown user and an unreachable directory all produce the same "invalid credentials" answer, so the sign-in page reveals nothing about the directory. Empty passwords are never sent to the directory (an empty password would be an unauthenticated bind that succeeds).

Disable the connection to switch directory sign-in off. A person who changes their password in the directory uses the new one immediately; Lahijan never stores it.

> [!NOTE]
> Directory sign-in trusts the directory: anyone who can authenticate there with an email address can sign in to the Lahijan account with that email address. Connect only directories whose email attribute you control. Creating accounts on sign-in does not need [registration](/docs/admin/users-and-tenants#turn-registration-off) to be on, because an administrator enabled the connection.

> [!NOTE]
> Users who only sign in through the directory have no local password. They can still use a password reset to set one, or you can set one when you edit the user.

> [!NOTE]
> Groups are shown for information; they are not mapped to tenant roles yet. Set roles in [user management](/docs/admin/users-and-tenants#manage-users-in-the-dashboard).

## Add a SAML connection

1. Select **New connection**, choose **SAML** and enter a **Name**. The name becomes part of the sign-in address, `/api/v1/auth/saml/<name>/start`, so pick something stable.
2. Under **Identity provider**, give the **IdP metadata URL**, or paste the **IdP metadata XML** (pasted metadata is used instead of the URL).
3. Optionally set the **Email attribute**, **Display name attribute** and **Groups attribute**: the SAML attribute names your IdP uses. Empty email and name attributes use the standard claim names that Entra ID, Okta and most IdPs send. Leave **Groups attribute** empty if you do not need groups.
4. The form shows the **ACS URL** and **Metadata URL** of this Lahijan server. Register them in your IdP.
5. Select **Test connection**. Lahijan fetches and parses the metadata and shows the IdP's entity ID.
6. Select **Save**.

An enabled SAML connection becomes active as soon as it is saved, without a restart. If it cannot be activated, the connection is still saved and marked **Not active**, and a message says why. The usual causes are unreachable or invalid metadata, and a missing service-provider signing key: outside `dev`, set `auth.saml.spSigningKey` and `auth.saml.spSigningCert` (see [Configuration](/docs/reference/configuration)). The same keys sign requests for every SAML provider on the server, so the published metadata is identical for all of them.

A name that is already used by a provider defined in the configuration file cannot be reused.

### Who can sign in with SAML

SAML sign-in recognises people by the identity the IdP asserts, not by email address:

- A person who signed in with this connection before is signed in to the same account.
- Someone with no link yet is accepted only when `auth.saml.jit.enabled` is `true`, which creates a new account from the asserted attributes. With it off, an account has to be linked first: the person signs in another way and links the IdP at **Settings > Identities** (see [Linked identities](/docs/account/identities)).

After each successful sign-in, Lahijan records the user's source and refreshes the groups found in the **Groups attribute**, so the group list on the connection fills in as people sign in.

## Edit, disable and delete

- **Edit** changes any field except the type. A SAML change takes effect immediately.
- Untick **Enabled** to switch a connection off without losing its settings. A disabled SAML connection stops signing people in; a disabled LDAP connection cannot be synced.
- **Delete** removes the connection and its imported groups. The users it created stay and keep working; manage them under **Users**.

## Use the API

All of these need `platform.directory.manage`. The bind password is accepted on create and update and is never returned; responses only say whether a secret is stored.

| Method and path                                                   | Purpose                                                                                      |
| ----------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `GET /api/v1/admin/directory/connections`                         | List connections.                                                                            |
| `POST /api/v1/admin/directory/connections`                        | Create one.                                                                                  |
| `GET`, `PATCH`, `DELETE /api/v1/admin/directory/connections/{id}` | Read, update or delete one.                                                                  |
| `POST /api/v1/admin/directory/test`                               | Test a saved connection (`connectionId`) or an unsaved draft (`connection`).                 |
| `POST /api/v1/admin/directory/connections/{id}/sync`              | Sync an LDAP connection. Answers `409` for SAML and `502` when the server cannot be reached. |
| `GET /api/v1/admin/directory/connections/{id}/groups`             | List imported groups with member counts (paginated).                                         |

```sh
curl -X POST https://cloud.example.com/api/v1/admin/directory/connections \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "kind": "ldap",
        "name": "corp-ldap",
        "bindPassword": "service-account-password",
        "config": {
          "url": "ldaps://ldap.example.com:636",
          "bindDn": "cn=svc-lahijan,dc=example,dc=com",
          "userBaseDn": "ou=people,dc=example,dc=com",
          "groupBaseDn": "ou=groups,dc=example,dc=com",
          "createUsersOnLogin": true
        }
      }'
```

## Limits

- Directory sign-in matches users by email address. Signing in with another attribute, such as a user name, is not supported.
- Mapping groups to roles, and disabling users that left the directory, are not part of this release.
- Sync reads the whole directory under the base DNs on every run; narrow the base DNs and filters on large directories.
- A SAML connection fetches the IdP metadata when it is saved or activated, and once at startup in the background. If the IdP was down at startup, save the connection again (or restart) once it is back.
