# Adding an account

This guide walks through connecting a mailbox to dk-email: what the connect
popup asks for, which login to choose, and what to do when your account has
two-factor authentication (2FA). Per-provider settings are at the end.

- [The connect popup](#the-connect-popup)
- [Choosing a login](#choosing-a-login)
- [Two-factor authentication](#two-factor-authentication)
- [Provider settings](#provider-settings)
- [When the login test fails](#when-the-login-test-fails)

To change or remove an account later, see
[Editing and deleting accounts](accounts.md).

## The connect popup

The popup opens on first run, and whenever you press `A`. It asks for:

| Field | What to enter |
|---|---|
| **Name** | A label for the sidebar, such as `Work` or `Personal`. It's also the key of the account's keychain entry, so it must be unique. |
| **Email** | Your address. Gmail, Outlook, Hotmail, Live and Microsoft 365 addresses fill in the servers for you. |
| **Username** | The IMAP login. Leave it as the email unless your provider says otherwise (Proton Mail Bridge, some self-hosted servers). |
| **IMAP host / Port / TLS** | Your provider's incoming server. Port 993 with implicit TLS is the usual choice; 143 with STARTTLS also works. |
| **Login** | Where the password comes from, or OAuth 2.0. See [Choosing a login](#choosing-a-login). |
| **SMTP host / port / TLS** | Optional. Fill it in to [send mail](../README.md#sending-mail); leave it empty for a read-only account. |
| **Aliases** | Optional. Other addresses this account can send as. See [Sending from aliases](send-as.md). |

`tab`/`↓` and `shift+tab`/`↑` move between fields, `←`/`→` or `space`
change a choice, and `enter` on **Submit** connects.

dk-email then tests the IMAP login, and the SMTP login when an SMTP host is
set. Only when that works is the account added to `config.toml`, with your
comments and formatting left as they are. If the test fails, the error and
a hint show in the popup: fix the field and press `enter` again.

## Choosing a login

| Login | Use it when | Where the secret lives |
|---|---|---|
| **1Password** | You keep the password in 1Password and have the `op` CLI. | 1Password. The config holds only `password_cmd = "op read op://…"`. |
| **Keychain** | You want dk-email to remember the password for you. | The OS keychain (macOS Keychain, or Secret Service on Linux). |
| **OAuth 2.0** | Microsoft 365, Outlook.com, or a Google account that refuses passwords. | A token in the OS keychain. You sign in in the browser; dk-email never sees your password. |

Whichever you pick, **no password is ever written to `config.toml`**: a
`password` key is rejected. If you prefer another password manager, set
`password_cmd` in the config by hand to any command that prints the password
(for example `pass show mail/work` or `security find-generic-password …`).
See [Passwords](../README.md#passwords).

**Which one for my provider?**

| Provider | Login |
|---|---|
| Gmail (personal) | App password (1Password or Keychain), or OAuth 2.0 |
| Google Workspace | App password if your admin allows them, otherwise OAuth 2.0 |
| Microsoft 365 (work/school) | OAuth 2.0 only |
| Outlook.com, Hotmail, Live | OAuth 2.0 only |
| iCloud Mail | App-specific password |
| Fastmail | App password |
| Yahoo, AOL | App password |
| Zoho Mail | App-specific password if 2FA is on, otherwise your password |
| Proton Mail | The Bridge password (via Proton Mail Bridge) |
| Self-hosted (Dovecot, etc.) | Your mail password |

## Two-factor authentication

IMAP and SMTP can only send a username and a secret. They have no way to
ask for a code, a push approval, a passkey or a security key. So once 2FA
is on, **your normal password stops working in a mail client**, and you
need one of these instead:

### App passwords

An *app password* (Apple calls it an *app-specific password*) is a long
random password that your provider generates for one app. It skips the 2FA
prompt but only works for mail clients, so it's safe to store.

1. Turn on 2FA for the account if it isn't already: most providers only
   offer app passwords once 2FA is on.
2. Create an app password in your account's security settings (links in
   [Provider settings](#provider-settings)). Name it `dk-email`.
3. In the connect popup, choose **Keychain** and paste it, or store it in
   1Password and choose **1Password** with its `op://` reference.

Tips:

- Spaces don't matter: Google shows `abcd efgh ijkl mnop`; with or without
  spaces both work.
- The same app password is used for IMAP and SMTP. There's no separate
  sending password.
- Changing your main password often revokes every app password. If logins
  suddenly fail after a password change, create a new one and update it
  with `E` (Login → Keychain or 1Password).
- To cut dk-email off, revoke its app password in your account settings.

### OAuth 2.0

With OAuth, you sign in on the provider's own page in your browser, so any
2FA method works there: codes, push approvals, passkeys, security keys,
conditional access. dk-email only receives a token. Choose **OAuth 2.0** in
the popup; the first time you also need a free app registration (a client
ID). See [Signing in with OAuth 2.0](oauth.md).

Use OAuth when:

- the provider doesn't accept passwords at all (Microsoft 365, Outlook.com);
- your Google Workspace admin has turned app passwords off;
- your Google account is in the Advanced Protection Program, which blocks
  app passwords;
- you'd rather not have a long-lived password on disk.

### Passkeys and security keys

A passkey or security key can't be used over IMAP directly. If your account
signs in only with a passkey, use OAuth 2.0 (where the browser handles the
passkey) or, where the provider still offers them, an app password.

## Provider settings

The servers below are the providers' published defaults. "Implicit" is
`tls = "implicit"` / `smtp_tls = "implicit"` (the default), "STARTTLS" is
`"starttls"`.

### Gmail (personal)

| | Host | Port | TLS |
|---|---|---|---|
| IMAP | `imap.gmail.com` | 993 | Implicit |
| SMTP | `smtp.gmail.com` | 465 | Implicit |

Filled in for you for `@gmail.com` and `@googlemail.com` addresses.

- **Without 2-Step Verification**, Google no longer accepts your password
  over IMAP. Turn 2-Step Verification on, then create an app password at
  <https://myaccount.google.com/apppasswords>.
- If that page says *the setting you are looking for is not available*,
  your account is in Advanced Protection or uses only passkeys, or 2-Step
  Verification is off. Use [OAuth 2.0](oauth.md#google-google-cloud).
- IMAP is always on for personal accounts.
- Gmail files sent mail in Sent itself, so `save_sent` defaults to `false`.

### Google Workspace

Same servers as Gmail. A custom domain isn't filled in automatically:
choose **OAuth 2.0 → Google** and the servers fill in, or type them.

- **App passwords** work only if your admin allows 2-Step Verification
  and hasn't disabled app passwords. Otherwise use
  [OAuth 2.0](oauth.md#google-google-cloud).
- **IMAP must be on for your domain.** If you see *IMAP access is disabled
  for your domain*, ask your admin to enable it
  ([details](oauth.md#imap-access-is-disabled-for-your-domain)). No login
  method gets round this.

### Microsoft 365 (work or school)

| | Host | Port | TLS |
|---|---|---|---|
| IMAP | `outlook.office365.com` | 993 | Implicit |
| SMTP | `smtp.office365.com` | 587 | STARTTLS |

- Exchange Online no longer accepts passwords (basic auth) for IMAP, and
  app passwords don't help. **Use [OAuth 2.0](oauth.md#microsoft-entra-id).**
  MFA and conditional access then happen in the browser as usual.
- Your admin may need to approve the app, and to enable IMAP and
  **Authenticated SMTP** for your mailbox. The OAuth guide lists the
  errors you'll see if they aren't.

### Outlook.com, Hotmail and Live

| | Host | Port | TLS |
|---|---|---|---|
| IMAP | `outlook.office365.com` | 993 | Implicit |
| SMTP | `smtp.office365.com` | 587 | STARTTLS |

Filled in for `@outlook.com`, `@hotmail.com`, `@live.com` and `@msn.com`
addresses. Personal Microsoft accounts need **OAuth 2.0** too: Microsoft
turned off password and app-password logins for them.
`imap-mail.outlook.com` and `smtp-mail.outlook.com` also work.

### iCloud Mail

| | Host | Port | TLS |
|---|---|---|---|
| IMAP | `imap.mail.me.com` | 993 | Implicit |
| SMTP | `smtp.mail.me.com` | 587 | STARTTLS |

- Use an **app-specific password**: sign in at
  <https://account.apple.com>, then **Sign-In and Security → App-Specific
  Passwords**. Your Apple Account needs two-factor authentication, which is
  on for almost all accounts.
- **Username:** your iCloud address (`name@icloud.com`). If you sign in to
  Apple with a non-Apple email, still use the iCloud address here.

### Fastmail

| | Host | Port | TLS |
|---|---|---|---|
| IMAP | `imap.fastmail.com` | 993 | Implicit |
| SMTP | `smtp.fastmail.com` | 465 | Implicit |

Fastmail requires an **app password** for every mail client, with or
without 2FA: **Settings → Privacy & Security → Manage app passwords**.
Give it IMAP and SMTP access.

### Yahoo and AOL

| | IMAP | SMTP |
|---|---|---|
| Yahoo | `imap.mail.yahoo.com` 993, Implicit | `smtp.mail.yahoo.com` 465, Implicit |
| AOL | `imap.aol.com` 993, Implicit | `smtp.aol.com` 465, Implicit |

Both need an **app password**: sign in to your account, then **Account
Security → Generate app password**.

### Zoho Mail

| | Host | Port | TLS |
|---|---|---|---|
| IMAP | `imap.zoho.com` | 993 | Implicit |
| SMTP | `smtp.zoho.com` | 465 | Implicit |

- Use the hosts for your data centre: `zoho.eu`, `zoho.in`, `zoho.com.au`
  and so on, matching the address you use to sign in to Zoho. Organisation
  accounts use `imappro.zoho.*` and `smtppro.zoho.*`.
- Turn IMAP on first: **Settings → Mail Accounts → IMAP Access**.
- With 2FA on, create an **application-specific password** under
  **My Account → Security → App Passwords**.

### Proton Mail

Proton Mail works through **Proton Mail Bridge**, which runs a local IMAP
and SMTP server. Use the password Bridge shows (not your Proton password)
and trust its certificate with `tls_cert`. See
[Proton Mail Bridge](../README.md#proton-mail-bridge).

### Self-hosted and other providers

Use the IMAP and SMTP servers your provider documents, and your mail
password (or an app password, if they have them). dk-email always
encrypts: it needs implicit TLS or STARTTLS and never falls back to
plaintext. For a server with a self-signed certificate, point `tls_cert` at
the certificate.

## When the login test fails

The popup shows the server's message, and a hint when dk-email recognises
the problem. The common ones:

| Message | Meaning |
|---|---|
| *Application-specific password required* | 2FA is on: use an app password, not your normal password. |
| *Invalid credentials* / *AUTHENTICATIONFAILED* | Wrong password or username. For Gmail, Yahoo, iCloud and Fastmail, it must be an app password. |
| *Web login required* | Google wants you to sign in once in a browser to clear a security check. Do that, then retry. |
| *IMAP access is disabled for your domain* | A Workspace admin has to enable IMAP. |
| *the server refused the access token* | OAuth sign-in worked but the mail server said no. See the [OAuth troubleshooting](oauth.md#troubleshooting). |
| *dial tcp … connection refused* / *timeout* | Wrong host or port, or a firewall. Check the provider's settings above. |
| *x509: certificate …* | The server's certificate isn't trusted: wrong host name, or a self-signed certificate that needs `tls_cert`. |
| *no password* | Nothing in the keychain under this account name, or `password_cmd` printed nothing. |

A new account is only added once its test passes. When **editing** an
account whose settings you know are right but can't be tested now (offline,
server down, password manager locked), press `ctrl+s` to **save anyway**. For a full log of each login step, run
`DK_EMAIL_DEBUG=1 dk-email`; the log never contains a password or token.
