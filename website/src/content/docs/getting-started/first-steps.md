---
title: First steps
description: Sign in to a new Lahijan deployment, find your way around the dashboard, create a first instance, zone and bucket, add users and secure your account.
---

This page is for the first hour after [installing Lahijan](/docs/getting-started/installation) (or starting the [Quickstart](/docs/getting-started/quickstart)). You sign in as the bootstrap administrator, create one resource of each kind, bring in other users and protect your account.

## Sign in

Open the dashboard address (for example `https://app.example.com`) and sign in on the **Sign in** page with the bootstrap admin's email and password. If the password was generated, the operator reads it from the server log; see [Sign in as the bootstrap admin](/docs/getting-started/installation#6-sign-in-as-the-bootstrap-admin).

Change the password straight away: go to **Settings > Profile**, fill in **Change password** and click **Update password**.

The bootstrap account belongs to a tenant called **Default Tenant** and holds the **platform administrator** role, so it can use everything, including the **Administration** section at the bottom of the sidebar.

## Find your way around

The sidebar has three parts:

- **Main areas:** **Dashboard**, **Instances** (compute), **DNS**, **Object Storage**, **Billing**, **Agent** and **Audit Log**.
- **Settings:** **Profile**, **Security**, **Access Tokens**, **Identities**, **Sessions** and **AI Provider**, all about your own account.
- **Administration** (platform admins only): **Billing**, **Plugins**, **Marketplace** and **Agent Policy**.

The **Dashboard** page gives a snapshot of the active tenant: counts of instances, DNS zones and buckets, your balance, charts of instances by status and storage usage by bucket, your recent ledger entries and the most recent audit events. Right after installation it is mostly empty.

If you belong to more than one tenant, a tenant switcher appears in the top bar. Everything you see and create applies to the tenant selected there. See [Core concepts](/docs/getting-started/concepts#tenants-and-memberships).

## Check prices and balance

Lahijan is designed to meter running resources per minute and charge them to the balance using the price catalog. Resource metering is not active yet (see [Balance and usage](/docs/billing/overview)), so nothing is charged for now. To set it up, go to **Administration > Billing**:

- The **Price catalog** tab lists a price per resource type. Click **Set price** to add one.
- The **Users** tab lets you top up or refund a user's balance by user id.

See [Billing administration](/docs/admin/billing) before you open the platform to other users.

## Create your first resources

Each of these takes a minute. The linked guides cover every option.

### An instance

1. Go to **Instances** and click **New instance**.
2. On the **Image** step, enter a name (lowercase letters, numbers and hyphens), choose **Container** or **Virtual Machine**, and pick an image such as `ubuntu/24.04`. **Browse** opens the remote image catalog.
3. On the **Size** step, set **vCPUs**, **Memory (MiB)** and **Disk (GiB)**.
4. Check the **Review** step and click **Create instance**.

The new instance is created stopped. Open it from the list and start it; the detail page then gives you the console, snapshots and the rest. See [Instances](/docs/compute/instances).

If the **Instances** page says the feature is disabled, the server cannot reach its compute backend. That is an operator problem; see [Troubleshooting](/docs/operations/troubleshooting).

### A DNS zone

1. Go to **DNS** and click **New zone**.
2. Enter the **Zone name** in canonical form with a trailing dot, for example `example.org.`, and click **Create zone**.
3. Open the zone to add records.

The zone answers queries only after you delegate the domain to your deployment's nameservers at your registrar. See [Zones](/docs/dns/zones) and [DNS overview](/docs/dns/overview).

### A bucket and an access key

1. Go to **Object Storage** and click **New bucket**.
2. Enter a **Slug** (lowercase letters, digits and dashes), optionally a **Label** and quotas, and click **Create bucket**.
3. Open the bucket, go to the **Credentials** tab and click **Mint credential**. Copy the secret key when it appears; the dashboard does not show it again.

The **Connection** tab shows the endpoint and settings to use in an S3 client. See [Buckets](/docs/storage/buckets), [Access keys](/docs/storage/credentials) and [Using S3 clients](/docs/storage/s3-clients).

After these steps, open **Audit Log**: each action you took is listed there.

## Add users

Lahijan does not yet have an administrator screen or API for creating accounts or inviting people into a tenant. The `/api/v1/admin/users/...` endpoints only handle billing (balance, ledger, top-up and refund). People get accounts in one of these ways:

- **Self-registration through the API.** Anyone who can reach the server can create an account with `POST /api/v1/auth/register`. The dashboard has no sign-up form yet, so this is an API call:

  ```sh
  curl -X POST https://app.example.com/api/v1/auth/register \
    -H 'Content-Type: application/json' \
    -d '{"email":"alice@example.com","password":"a-long-passphrase-here","displayName":"Alice"}'
  ```

  The password must meet the minimum length (12 characters by default). Lahijan sends a verification email if outgoing mail is set up.

- **Single sign-on.** If the operator has set up OAuth, OIDC or SAML providers, users can sign in through them. For SAML, new accounts are created on first sign-in only when just-in-time creation (`auth.saml.jit.enabled`) is turned on. See [Sign-in providers and email](/docs/operations/authentication).

With the default setting `auth.signup.personalTenant: true`, every self-registered account gets its own **personal tenant** and the **owner** role in it, so the new user can start creating resources at once. If you turn this off, new accounts have no tenant and cannot create anything.

Adding an existing user to another tenant, or changing a member's role, has no endpoint or screen yet. See [Users and tenants](/docs/admin/users-and-tenants) for what administrators can and cannot do today.

> [!WARNING]
> Registration is open to anyone who can reach `/api/v1/auth/register`, and there is no setting to close it. If your deployment faces the internet, keep that in mind when you set prices and top up balances: a new account has a zero balance until an administrator tops it up.

## Set up two-factor sign-in

Go to **Settings > Security**. Three options are there:

- **Authenticator app:** click **Set up**, scan the QR code with an authenticator app (or enter the secret shown under **Can't scan?**), type the **6-digit code** and click **Confirm**.
- **Passkeys / security keys:** click **Add a passkey**. This only works if the operator has configured WebAuthn for the deployment.
- **Recovery codes:** one-time codes for when you lose your device. They are shown only once; use **Copy all** and store them somewhere safe. **Regenerate codes** replaces them.

> [!CAUTION]
> The dashboard's sign-in form cannot complete a two-factor challenge yet. Once a factor is enabled on an account, signing in on the **Sign in** page stops at "Multi-factor authentication required". Completing the sign-in then takes two API calls: `POST /api/v1/auth/login` returns a `pendingSessionToken`, and `POST /api/v1/auth/mfa/challenge` with that token, `"kind": "totp"` and your `code` returns the session. Do not enable a factor on the only platform administrator account until the dashboard supports it.

See [Sign-in security](/docs/account/security) for details.

## Next steps

- [Core concepts](/docs/getting-started/concepts): how tenants, roles, billing and the audit log fit together.
- [Access tokens](/docs/account/access-tokens): use the REST API from scripts.
- [Installing plugins](/docs/admin/plugins): extend the deployment.
