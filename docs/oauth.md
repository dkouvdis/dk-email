# Signing in with OAuth 2.0

Microsoft 365, Outlook.com and many Google Workspace accounts don't accept
a password over IMAP. They want **OAuth 2.0**: you sign in once in your
browser, and the mail client gets a token instead of your password.
dk-email supports this for **Microsoft** and **Google**, for both reading
(IMAP) and sending (SMTP).

You bring your own **app registration** (a "client ID"). It's free, takes a
few minutes, and means no third party sits between you and your mailbox.

- [How it works](#how-it-works)
- [Step 1: register an app](#step-1-register-an-app)
  - [Microsoft (Entra ID)](#microsoft-entra-id)
  - [Google (Google Cloud)](#google-google-cloud)
- [Step 2: add the account in dk-email](#step-2-add-the-account-in-dk-email)
- [Configuration reference](#configuration-reference)
- [Using an external token tool (oama)](#using-an-external-token-tool-oama)
- [Where tokens are kept](#where-tokens-are-kept)
- [Signing in again](#signing-in-again)
- [Troubleshooting](#troubleshooting)

## How it works

1. You pick **OAuth 2.0** in the connect popup and press **Sign in & save**.
2. dk-email opens your browser at Microsoft's or Google's sign-in page. It
   listens on `http://localhost:<random port>/` for the page to send you
   back. If the browser doesn't open, the popup shows the address to open
   yourself.
3. You sign in (with MFA, passkeys, whatever your account uses). The browser
   tab then says *Signed in to dk-email. You can close this tab.*
4. dk-email tests the IMAP login, and the SMTP login if the account sends,
   using the new token (the `XOAUTH2` login method).
5. Only if that works does it store the sign-in in your **OS keychain** and
   add the account to `config.toml`. Nothing secret goes in the config file.

From then on dk-email refreshes the token on its own, a couple of minutes
before it expires, on every connect, reconnect and send. You won't see a
browser again unless the sign-in expires or is revoked.

**Microsoft device code.** Instead of the browser redirect, Microsoft
accounts can sign in with a short code: press `ctrl+d` while the popup
waits for the browser, then go to the address shown (usually
<https://microsoft.com/devicelogin>), enter the code, and sign in there.
This also works from another device, and dk-email switches to it on its
own when it can't open a browser. Google doesn't offer device codes for
mail.

## Step 1: register an app

### Microsoft (Entra ID)

Works for work/school accounts (Microsoft 365, Exchange Online) and
personal accounts (Outlook.com, Hotmail, Live).

1. Open the [Microsoft Entra admin center](https://entra.microsoft.com/)
   → **Identity → Applications → App registrations → New registration**.
   (A personal account can use <https://portal.azure.com> → *App
   registrations*.)
2. **Name:** `dk-email`.
3. **Supported account types:**
   - *Accounts in any organizational directory and personal Microsoft
     accounts* works for everyone, and is the default dk-email assumes
     (tenant `common`).
   - *Accounts in this organizational directory only* is fine for a single
     company. Then set `oauth_tenant` to your tenant ID or domain (see
     [reference](#configuration-reference)).
4. **Redirect URI:** platform **Public client/native (mobile & desktop)**,
   value `http://localhost`. Microsoft matches any port on `localhost`, which
   is what dk-email uses.
5. Click **Register**, then copy the **Application (client) ID** from the
   overview page.
6. **Authentication** → under *Advanced settings*, set **Allow public client
   flows** to **Yes**, and save. This allows the device code sign-in and
   tells Microsoft there's no client secret.
7. **API permissions → Add a permission → Microsoft Graph → Delegated
   permissions**, and add:
   - `IMAP.AccessAsUser.All`
   - `SMTP.Send`
   - `offline_access`

   Some organisations require an admin to approve these. If sign-in later
   says *Need admin approval*, ask your admin to **Grant admin consent**
   on this page.

Don't create a client secret: dk-email is a desktop app, and Entra
rejects secrets from public clients.

**Exchange Online also needs:**

- **IMAP** turned on for your mailbox (it usually is).
- **Authenticated SMTP** turned on, if you want to send. Many tenants turn
  it off. An admin enables it per mailbox in the Microsoft 365 admin
  center: *Users → Active users → (you) → Mail → Manage email apps →
  Authenticated SMTP*.

### Google (Google Cloud)

Works for personal `@gmail.com` accounts and Google Workspace.

1. Open <https://console.cloud.google.com/> and **create a project** (e.g.
   `dk-email`) from the project picker.
2. Go to **APIs & Services → OAuth consent screen** (shown as **Google Auth
   Platform**).
3. **Branding:** app name `dk-email`, your support email and a developer
   contact email.
4. **Audience:**
   - **Internal**: only for Google Workspace, and the best choice there:
     no test users, no warning screen, and sign-ins don't expire weekly.
   - **External**: for personal accounts. It starts in **Testing**:
     - add your own address under **Test users**;
     - in Testing, the sign-in **expires after 7 days** and you'll have to
       sign in again weekly. To avoid that, click **Publish app**. You don't
       need Google's verification for your own use (up to 100 users), but
       sign-in then shows *Google hasn't verified this app*. Click
       **Advanced → Go to dk-email** to continue.
5. **Data Access → Add or remove scopes** → add `https://mail.google.com/`
   (type it under *Manually add scopes* if it isn't listed). It's the only
   scope that allows IMAP and SMTP.
6. **Clients → Create client**:
   - **Application type: Desktop app**. A *Web application* client won't
     accept dk-email's `http://localhost` redirect.
   - Name it (e.g. `dk-email desktop`) and **Create**.
7. Copy the **Client ID** (`…apps.googleusercontent.com`) and the **Client
   secret**. Google still requires the secret from desktop apps, even though
   it can't be kept truly secret. Treat it like a password anyway.

**Google Workspace also needs:** IMAP allowed by your admin. If it's off,
the login fails with *IMAP access is disabled for your domain*; see
[troubleshooting](#imap-access-is-disabled-for-your-domain).

**No admin, no project?** Personal Gmail accounts with 2-Step Verification
can use an [app password](https://myaccount.google.com/apppasswords) with
`password_cmd` or the keychain instead of OAuth. Most Workspace accounts
don't allow app passwords.

## Step 2: add the account in dk-email

1. Press `A` (or start dk-email with no accounts) to open **Connect
   account**.
2. Fill in **Name** and **Email**. An Outlook/Hotmail/Live/Microsoft 365 or
   Gmail address fills in the servers for you:

   | Provider | IMAP | SMTP |
   |---|---|---|
   | Microsoft | `outlook.office365.com:993`, implicit TLS | `smtp.office365.com:587`, STARTTLS |
   | Google | `imap.gmail.com:993`, implicit TLS | `smtp.gmail.com:465`, implicit TLS |

   For a company domain on Microsoft 365 or Google Workspace, choose OAuth
   first and pick the **Provider**: the servers follow it. Servers you typed
   yourself are never overwritten.
3. Set **Login** to **OAuth 2.0**. An *OAuth 2.0* box opens with:
   - **Provider**: Microsoft or Google;
   - **Client ID**: from step 1;
   - **Tenant** (Microsoft only): leave empty for `common`;
   - **Secret** (Google only): the client secret from step 1.
4. Press **Sign in & save** and sign in in the browser. `esc` cancels at any
   point.

To switch an existing account to OAuth, press `E` on it, set **Login** to
**OAuth 2.0** and do the same. See [Editing an
account](accounts.md#editing-an-account).

## Configuration reference

The popup writes these keys for you. By hand they look like this:

```toml
[[account]]
name            = "Work"
email           = "me@contoso.com"
imap_host       = "outlook.office365.com"
smtp_host       = "smtp.office365.com"
smtp_port       = 587
smtp_tls        = "starttls"
auth            = "oauth2"
oauth_provider  = "microsoft"
oauth_client_id = "00000000-0000-0000-0000-000000000000"
# oauth_tenant  = "contoso.onmicrosoft.com"   # optional, default "common"

[[account]]
name                    = "Gmail"
email                   = "me@gmail.com"
imap_host               = "imap.gmail.com"
smtp_host               = "smtp.gmail.com"
auth                    = "oauth2"
oauth_provider          = "google"
oauth_client_id         = "1234567890-abc123.apps.googleusercontent.com"
# oauth_client_secret_cmd = "op read 'op://Private/dk-email Google/client secret'"
```

| Key | Meaning |
|---|---|
| `auth` | `"oauth2"` to sign in with OAuth. `"password"` (or leaving it out) keeps the usual password login. |
| `oauth_provider` | `"microsoft"` or `"google"`. Required with `auth = "oauth2"` unless `password_cmd` supplies the token. |
| `oauth_client_id` | Your app's client ID. Required with `oauth_provider`. |
| `oauth_tenant` | Microsoft only. Defaults to `common` (any account). Set your tenant ID or domain for a single-tenant app. |
| `oauth_client_secret_cmd` | Google only, optional. A command that prints the client secret, run like `password_cmd`. Without it, the secret comes from the keychain (the popup stores it there). |

Rules dk-email enforces when loading the config:

- `oauth_client_secret` is rejected: secrets never go in the file. Use
  `oauth_client_secret_cmd` or the keychain.
- `oauth_*` keys without `auth = "oauth2"` are errors, as is an
  `oauth_tenant` on Google or an `oauth_client_secret_cmd` on Microsoft.
- `auth = "oauth2"` takes its token from **either** `password_cmd` **or** the
  `oauth_*` keys, not both.
- The account's `username` (default: `email`) is the user name sent with
  the token, so it must be the mailbox's address.

**`save_sent`.** Exchange Online, Outlook.com and Gmail already file mail
sent through their SMTP servers in Sent, so dk-email doesn't append a second
copy there by default. That applies when the IMAP host is
`outlook.office365.com`, `imap-mail.outlook.com` or `imap.gmail.com`, or the
SMTP host is `smtp.office365.com` or `smtp-mail.outlook.com`. Set
`save_sent = true` to force a copy.

## Using an external token tool (oama)

If you already manage tokens with a tool such as
[oama](https://github.com/pdobsan/oama) or `mutt_oauth2.py`, point
`password_cmd` at it instead of using the `oauth_*` keys:

```toml
[[account]]
name         = "Work"
email        = "me@contoso.com"
imap_host    = "outlook.office365.com"
auth         = "oauth2"
password_cmd = "oama access me@contoso.com"
```

With `auth = "oauth2"`, the first line the command prints is used as an
**access token** for `XOAUTH2`, not as a password. dk-email runs it for every
connection, reconnect and send, so the tool's own refresh keeps it fresh.
dk-email stores nothing in the keychain for such an account.

## Where tokens are kept

Everything lives in the OS keychain (macOS Keychain, or the Secret Service
on Linux), under the service `dk-email`, next to any passwords:

| Keychain account | Holds |
|---|---|
| `oauth:<name>` | the sign-in: access token, refresh token and expiry, as JSON |
| `oauth-client-secret:<name>` | the Google client secret typed in the popup |

`<name>` is the account's `name` in `config.toml`. On macOS you can check an
entry exists with:

```sh
security find-generic-password -s dk-email -a "oauth:Work"
```

- **Refreshing.** When the access token is within two minutes of expiring,
  dk-email asks for a new one. Microsoft hands out a new refresh token each
  time, and dk-email saves it to the keychain before using the new access
  token, so the sign-in survives a crash.
- **Renaming** an account in the edit popup moves both entries to the new
  name.
- **Deleting** an account asks whether to remove its sign-in too (`y`), or
  keep it (`k`).
- **Errors never contain tokens or secrets.** They're masked as `***` or
  `[redacted]`, so error messages and the debug log are safe to share.

## Signing in again

A sign-in can end: you changed your password, an admin revoked it, the
Google app is still in Testing (7 days), or you haven't used the account for
a long time. The account then shows:

> Work needs you to sign in again: press E, choose OAuth 2.0 and Sign in & save

Press `E` on the account, set **Login** to **OAuth 2.0** (the provider and
client ID are already filled in), and press **Sign in & save**. For Google,
leave **Secret** empty to keep the saved one.

## Troubleshooting

### The browser didn't open

The popup shows the full sign-in address. Copy it into a browser on the same
machine: the page has to be able to reach `http://localhost` when it sends
you back. For Microsoft, press `ctrl+d` to sign in with a code instead, from
any device.

### "IMAP access is disabled for your domain"

Your Google Workspace admin has turned IMAP off. The sign-in worked, but
Gmail refuses IMAP logins for the whole domain. The admin can enable it in
the [Google Admin console](https://admin.google.com) under **Apps → Google
Workspace → Gmail → End User Access → POP and IMAP access**:

- tick **Enable IMAP access for all users** (or for your organisational
  unit), and
- under **IMAP access**, choose **Allow any mail client**, or **Restrict
  which mail clients users can use** and add your OAuth client ID.

The change can take a while to apply. Then sign in again.

### "the server refused the access token"

The mail server didn't accept the token, although the sign-in worked.
Usually:

- **Microsoft:** the app lacks the `IMAP.AccessAsUser.All` or `SMTP.Send`
  permission, or IMAP/Authenticated SMTP is off for the mailbox (see [Exchange
  Online also needs](#microsoft-entra-id)).
- **Google:** the `https://mail.google.com/` scope wasn't added to the app.
- **Both:** `username` isn't the mailbox's address.

Fix that, then press `E` and **Sign in & save** again.

### Microsoft sign-in errors

| Error | Fix |
|---|---|
| `AADSTS50011` (redirect URI mismatch) | Add the **Public client/native** platform with redirect URI `http://localhost` (step 4). |
| `AADSTS7000218` (requires `client_assertion` or `client_secret`) | Set **Allow public client flows** to **Yes** (step 6). |
| `AADSTS700016` (application not found in the directory) | The client ID is wrong, or the app is single-tenant: set `oauth_tenant` to your tenant. |
| `AADSTS50020` / `AADSTS50194` (account not allowed for this app) | The app's *Supported account types* doesn't include your account. Use the multi-tenant + personal option, or set `oauth_tenant`. |
| `AADSTS65001` / *Need admin approval* | An admin must grant consent for the app's permissions (step 7). |
| `535 5.7.139 … SmtpClientAuthentication is disabled` | Authenticated SMTP is off for the mailbox: ask an admin to enable it. |

### Google sign-in errors

| Error | Fix |
|---|---|
| `redirect_uri_mismatch` | The client must be a **Desktop app**, not a Web application (step 6). |
| `access_denied` / *has not completed the Google verification process* | The app is in Testing and your address isn't a **test user**. Add it, or publish the app. |
| *Google hasn't verified this app* | Expected for an unverified published app: **Advanced → Go to dk-email**. |
| `invalid_client` | Wrong client ID or secret. Check both, and retype the secret in the popup. |
| `admin_policy_enforced` | Your Workspace admin blocks third-party apps. Ask them to trust your client ID under **Security → API controls**. |
| *sign-in expired or was revoked* after about a week | The app is in Testing. Publish it, or use **Internal** on Workspace, then sign in again. |

### Something else

Run dk-email with `DK_EMAIL_DEBUG=1` to write a debug log to
`~/.cache/dk-email/debug.log`. It records each login step and how long it
took, never a token or password.
