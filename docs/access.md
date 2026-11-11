# Access

BAM! decides who may see and change what with roles, permissions and scopes. People sign in with
an email and password, and hold permissions through the roles they are given. Scripts, dashboards
and assistants use API tokens instead of a session. Scopes, labels put on categories, let a role
reach some assets and not others.

This page covers signing in, user accounts, roles and permissions, scopes, the public role, and
API tokens.

## Signing in

People sign in with an email and a password.

- Emails are case-insensitive and unique.
- A new password must be 8 to 72 bytes. 72 is the most bcrypt uses, so a longer password is
  refused rather than silently cut short.
- Anyone can add a second factor: a code from an authenticator app (see
  [Two-factor sign-in](#two-factor-sign-in)).

Signing in starts a session. The session lives in the database, and the browser holds a cookie
that refers to it (see [Sessions](#sessions)).

### Too many wrong passwords

Guessing passwords is slowed down, by account and by address:

- **An account** that has had 5 wrong passwords in 15 minutes waits before its next try: 1 second
  after the 5th, then twice as long after each further wrong one, up to 15 minutes. It is never
  locked, so nobody can lock someone out by guessing badly. The right password clears the count,
  and so does setting their password or clearing their delay on the Users page.
- **An address** that has had 20 wrong passwords in 15 minutes, across any accounts, waits
  15 minutes. This catches one password tried against many emails.
- **While either waits,** signing in answers `429` with `Retry-After` and no password is checked.
  The sign-in page says how long to wait.

A count lapses 15 minutes after the last wrong password, or after the wait it set ends, whichever
is later. Counts are kept in the database, so servers sharing it count together.

An email with no account is counted the same way, and checks the password against a stand-in hash,
so neither the answer nor how long it takes says whether an email has an account.

When an account (one that exists) or an address first has to wait, `auth.throttled` is recorded.
Notification channels can subscribe to it, so an admin hears that someone may be guessing. It is
about no asset or category, so live connections and the events list never show it.

Behind a reverse proxy, every request comes from the proxy's address unless the proxy is named in
`server.trusted_proxies` (see [Configuration](configuration.md#behind-a-reverse-proxy)).

### Two-factor sign-in

Anyone can ask for a second factor as well as their password, so that the password alone is not
enough: a code from an authenticator app, or a [passkey](#passkeys). They are set up under **Your
account**, and are off until someone sets one up. One person can have an app and several passkeys;
any of them finishes a sign-in.

**Setting up an app:**

1. **Set up an authenticator app** shows a QR code, and the same key as text. Any app that takes
   TOTP codes (RFC 6238) works, such as Aegis, Google Authenticator or 1Password. The app shows the
   codes under BAM! and the instance's host.
2. The code the app shows turns it on. Until then, nothing changes.

**Signing in:** after the password, the sign-in page asks for the app's code, or offers **Use a
passkey**. Signing in is two steps:

- **The password** answers `{"factor_required": true, "factors": [...]}`, the kinds that can finish
  it, `totp` and `passkey`, with no session. It sets a cookie for the second step instead, which is
  sent only to the sign-in routes, lasts five minutes, and grants nothing else. After an identity
  provider, `GET /api/v1/auth/login/factor` answers the same `factors`.
- **The second factor** (`POST /api/v1/auth/login/factor`), finishes the sign-in, and starts the
  session: `{"code"}` from the app or a [recovery code](#recovery-codes), or `{"passkey"}`, a
  passkey's answer.

**Codes:**

- Six digits, changing every 30 seconds (SHA-1, as apps use). A code from 30 seconds either side
  is taken too, for a phone's clock a little out.
- Each code is taken once. A code used already, or one older than it, is refused, even when two
  servers are given it at the same moment.
- Wrong codes count with wrong passwords (see [Too many wrong passwords](#too-many-wrong-passwords)).
  The right password does not clear the count while a code is still to come, so knowing the
  password does not allow more guesses at the code.

**Removing a factor** takes the password and a current second factor, a code from the app or a
passkey, under **Your account**. Removing the last one removes the recovery codes too.

#### Passkeys

Passkeys and security keys (WebAuthn): a YubiKey, or the screen lock of a phone or computer. They
cannot be phished: a passkey answers only the site it was made for.

- **Adding one:** **Add a passkey** under **Your account**, with a name for it, such as YubiKey or
  Phone. The browser then asks to touch the key or unlock the device. Each person can add several.
- **Signing in:** after the password, **Use a passkey**. User verification, such as a PIN or a
  fingerprint, is preferred but not required: the password is the other factor.
- **Removing one** takes the password and a current factor, which can be another passkey. It is
  refused when it would leave someone whose roles require two-factor sign-in with none (see
  [Requiring two-factor sign-in](#requiring-two-factor-sign-in)).
- **Bound to `[server] base_url`:** its host is the relying party ID passkeys are made for, and
  they work only there. Passkeys need `base_url` set to the address people use, and are offered
  only to a browser at that host: the default, `localhost`, is no use elsewhere. **Changing the
  host makes every passkey added before unusable.** Their owners sign in with an app or a recovery
  code and add them again. **Admin › Sign-in** shows the host, and how many passkeys were added at
  another.
- **What is kept:** each passkey's credential ID, public key and signature counter, as rows of
  `user_factors` of kind `passkey`, and nothing secret. No attestation is asked for, so no
  device's make or model is checked or kept.
- **Copies:** a passkey whose counter goes backward is refused, as a sign that it was copied.
  Authenticators that keep no counter, as synced passkeys often do, always send zero, and are
  taken.
- **Each answer is to a challenge sent for it,** which lasts five minutes and is taken as it is
  answered, so an answer cannot be used twice. A challenge sent to sign in cannot confirm removing
  a factor, nor the other way about.
- **No secret key needed:** unlike an app's, a passkey's public key is no secret.

Signing in with a passkey alone, in place of the password, is not offered.

#### Recovery codes

A way back in when the phone is lost.

- **Made with the first factor:** setting up the first app or passkey makes ten codes. They are
  shown once, with **Download** (a text file) and **Print**, and never again.
- **Each works once,** in place of a second factor: **Use a recovery code** on the sign-in page.
  Case, spaces and dashes do not matter. A wrong one counts as a wrong code.
- **Stored as hashes,** as sessions are (SHA-256), never in a form that can be read back. They are
  rows of `user_factors` of kind `recovery`, and are not listed as factors of their own.
- **New codes** replace all ten, and take a current second factor: **Make new recovery codes**
  under **Your account**.
- **How many are left** shows under **Your account**, with a warning when fewer than three remain.
- **Using one** records `auth.recovery_code_used` in history, naming whose it was. Notification
  channels can subscribe to it. When email is set up (see [Notifications](notifications.md)), it
  also emails the person, whatever they chose for email to their roles, since it is about their
  own account.

Codes are ten characters of Crockford's base32, such as `4f8kq-29xmd`: 50 random bits each.

**Lost phones:** whoever manages users removes someone's second factors with **Remove second
factors** on the Users page (`DELETE /api/v1/users/{id}/factors`). That signs them out everywhere,
and they sign in with their password alone until they set one up again. On the command line,
`bam admin mfa reset --email …` does the same for anyone, admins included. Either way,
`auth.factors_reset` is recorded, naming whose they were and who removed them, and notification
channels can subscribe to it.

**The secret key:** each app's secret is kept in the database sealed with `[auth] secret_key` from
the configuration (see [Configuration](configuration.md#the-configuration-file)), so a copy of the
database alone cannot make codes. `bam init` writes a key. An instance set up before it has none,
and its people cannot set up an app until one is added: 32 random bytes as base64, such as
`openssl rand -base64 32` makes. Keep it with the configuration, and keep it the same: with another
key, apps already set up cannot be checked. Signing in then says so, rather than counting a wrong
code, and an admin removes the factor for the person to set it up again. This is also what happens
after restoring a backup into an instance with another key (see [Backups](backups.md#authentication)).

API tokens are not affected: a token is a separate credential, and is never asked for a code.

#### Requiring two-factor sign-in

**By role:** each role has a **Requires two-factor sign-in** switch, under **Admin › Roles**.
Whoever holds any role with it on must sign in with a second factor.

- **The admin role has it on,** on a new instance and on one upgraded to it, so an admin without a
  second factor sets one up at their next sign-in. **Stop requiring two-factor sign-in** on the
  admin role turns it off, after a warning that whoever has an admin's password then has every
  permission. It is the one setting of the admin role that can change. On the command line,
  `bam admin mfa require --role admin --off` does the same, and `--off` left out turns it on.
- **Turning it off,** for any role, records `role.two_factor_off` in history, naming the role and
  who turned it off. Notification channels can subscribe to it.
- **Someone required to have a second factor who has none** is asked to set one up at their next
  sign-in, and can do nothing else until they do. Their session reaches only setting up an
  authenticator app, who they are, and signing out; anything else answers `403`. Sessions already
  open when a role starts requiring it go on until they end.
- **Their last second factor cannot be removed** while a role they hold requires one. Whoever
  manages users can still remove someone's factors; they then set one up at their next sign-in.
- **It needs `[auth] secret_key`:** without the key no factor can be set up, so nobody is held to
  setting one up.

**From identity providers:** someone signing in through a provider has already signed in there,
perhaps with a second factor of its own. By default, BAM! trusts the provider:

- **Its sign-in counts as two-factor** when its `amr` holds one of the values the provider's
  settings list (`mfa`, `otp`, `hwk`, `swk` and `fpt` by default), or its `acr`, or SAML's
  `AuthnContextClassRef`, is one they list (none by default).
- **When it does not,** someone whose roles require a second factor is asked for BAM!'s own if the
  provider's **When it does not say so, ask for BAM!'s own second factor** is on, and has to set
  one up if they have none. With it off, the sign-in is refused, saying the provider must require a
  second factor.
- **Someone whose roles do not require one,** but who set one up, is asked for it unless the
  provider's sign-in counts as two-factor.
- **Always ask for BAM!'s own second factor** is a per-provider setting for a provider whose word
  is not trusted: BAM!'s own is asked for whatever the provider says.
- **Asking the provider for a second factor:** an OpenID Connect provider's **Ask for
  (acr_values)** setting sends `acr_values` with the sign-in, such as a level the provider defines
  as requiring a second factor. A SAML provider's **Ask for (AuthnContextClassRef)** sends
  `RequestedAuthnContext` the same way.

**Tokens are unaffected:** API tokens never ask for a code, and an assistant's grant is made from a
session that already passed whatever was required.

### Signing in with an identity provider

People can sign in through an identity provider the space already runs, such as Google Workspace,
Microsoft Entra, Authentik, Keycloak or Okta. Each provider is a button on the sign-in page. They
are set up under **Admin › Sign-in** by someone with `settings.manage`.

Each provider has:

- **A key and a name.** The key is used in its addresses, such as the one the provider sends
  people back to, `{base_url}/api/v1/auth/providers/{key}/callback`.
- **A protocol, and that protocol's settings.** [OpenID Connect](#openid-connect),
  [plain OAuth2](#plain-oauth2) and [SAML 2.0](#saml-20) each add one. A protocol's secrets, such as a client secret, can be written but are
  never shown again. They are kept sealed with `[auth] secret_key`, as second factors' secrets
  are, so a provider with secrets needs the key.
- **Its button:** a label, by default "Sign in with" its name, and an optional icon.
- **An enabled switch, and its place among the buttons.**

**From the configuration:** providers can also be set in `config.toml` as `[[auth.providers]]`, or
in `BAM_AUTH_PROVIDERS` as a JSON list, for instances provisioned by a script (see
[Configuration](configuration.md#identity-providers)). Admin shows them, read-only. Their rules
name roles and scopes by name.

#### OpenID Connect

OpenID Connect is what most identity providers speak: Google, Microsoft Entra, Authentik, Keycloak,
Okta, Zitadel and Authelia among them. Its settings:

- **Issuer:** the provider's issuer URL. BAM! reads its discovery document, the issuer followed by
  `/.well-known/openid-configuration`. It must be `https`, but for a provider on the same machine.
- **Client ID and client secret,** from registering BAM! with the provider. The secret can be left
  blank for a public client.
- **Scopes:** `openid email profile` when blank. Add whichever scope carries groups, such as
  `groups`, for rules on them.
- **The email, name and groups claims:** `email`, `name` and `groups` when blank. The groups claim
  is read as `groups` by rules and by the groups accounts are limited to, whatever it is called.

At the provider, register BAM! as a web application, with the redirect URI Admin shows for the
provider: `{base_url}/api/v1/auth/providers/{key}/callback`.

**How it signs in:** the authorization code flow, with PKCE (`S256`), `state` and a `nonce`. The ID
token is checked fully:

- **Its signature,** by one of the provider's published keys. They are kept for a day, and fetched
  again when a token names a key not among them, at most once a minute.
- **Only asymmetric algorithms** (RSA, RSA-PSS, ECDSA and Ed25519): never `none`, nor `HS256`
  keyed with the client secret, which anyone holding the secret could forge.
- **`iss`, `aud`, `azp`, `exp`, `iat` and `nonce`,** allowing a minute's difference in clocks.

Claims the ID token lacks are read from the provider's userinfo, which must name the same `sub`.
The token's `amr` and `acr`, which say how the person signed in at the provider, are kept on the
sign-in for #192.

**When it fails,** the sign-in page says so without naming a secret or the reason, which goes to the
server's log. Cancelling at the provider says the provider did not sign them in. Someone with no
account linked is told the same whether or not an account has their email, so that a provider
letting anyone choose any email cannot be used to find out who has an account here.

##### Google

1. In the Google Cloud console, under **APIs & Services › Credentials**, create an **OAuth client
   ID** of type **Web application**.
2. Add the redirect URI under **Authorized redirect URIs**.
3. Paste the client ID and secret into BAM!, with the issuer `https://accounts.google.com`.

Google gives no groups, so rules on groups do not apply. Limit accounts to your Google Workspace with
the provider's allowed domains.

##### Microsoft Entra

1. Under **App registrations**, make a **New registration**, with a **Web** redirect URI.
2. Under **Certificates & secrets**, make a client secret, and paste its **Value**, not its ID.
3. The issuer is `https://login.microsoftonline.com/{tenant ID}/v2.0`, with the directory (tenant)
   ID from the registration's overview.
4. Under **Token configuration**, add the optional `email` claim, and a **groups claim** for rules
   on groups. Entra gives groups as their object IDs, so rules name those.

Entra does not say whether an email is verified. Trust the provider's emails only when your
organization manages every email in the tenant.

##### Authentik

1. Under **Applications › Providers**, create an **OAuth2/OpenID Provider** of client type
   **Confidential**, with the redirect URI, and an application for it.
2. Choose a **Signing Key**. Without one, Authentik signs with the client secret (`HS256`), which
   BAM! refuses.
3. The issuer is `https://{authentik}/application/o/{application slug}/`. Authentik's `profile`
   scope carries `groups`.

##### Keycloak

1. In the realm, under **Clients**, create an **OpenID Connect** client with **Client
   authentication** on, and the redirect URI under **Valid redirect URIs**.
2. Paste the secret from the client's **Credentials** tab. The issuer is
   `https://{keycloak}/realms/{realm}`.
3. For groups, add a **Group Membership** mapper to the client's dedicated scope, with the token
   claim name `groups`, **Full group path** off, and **Add to ID token** on.

#### Plain OAuth2

For providers that offer OAuth2 without OpenID Connect, GitHub the best known. With no ID token,
who signed in is read from the provider's user endpoint, by paths into its JSON. Its settings:

- **Preset:** `github` or `discord` fills in everything below but the client ID and secret. Any
  setting given is used in place of the preset's, such as GitHub Enterprise's endpoints. Blank is
  another provider, whose endpoints and ID path are then required.
- **The authorization, token and user endpoints.** They must be `https`, but for a provider on the
  same machine.
- **Client ID and client secret,** from registering BAM! with the provider.
- **Scopes,** separated by spaces.
- **The ID, email, email verified, name and groups paths:** where the user endpoint's JSON has
  each, such as `id`, or `data.user.id` for one nested deeper. Several paths separated by commas
  are tried in order, such as `name,login`. The email and name paths are `email` and `name` when
  blank. Groups are read as `groups` by rules, wherever they were.
- **PKCE:** on unless set to `off`, for a provider that refuses a challenge.

**The ID** is the provider's stable ID for the person, such as GitHub's numeric user ID: never a
login name, which can change. An account linked to it stays linked whatever their login becomes.

**Emails:** an email counts as verified only where the provider says so, at the email verified path.
Without one, no email is verified, so none links an account by itself (see
[Who signs in as whom](#who-signs-in-as-whom)).

**How it signs in:** the authorization code flow, with `state` always and PKCE (`S256`) unless off.
The code is traded for an access token, with the client secret in the form, and the user endpoint is
read with it. The provider says nothing of how someone signed in, so a role requiring two-factor
sign-in asks for BAM!'s own second factor after it (see
[Requiring two-factor sign-in](#requiring-two-factor-sign-in)).

##### GitHub

1. Under **Settings › Developer settings › OAuth Apps**, make a **New OAuth App**, with the
   redirect URI Admin shows as its **Authorization callback URL**.
2. Make a client secret, and paste it and the client ID into BAM!, with the preset `github`.

The preset asks for `read:user user:email read:org`. GitHub's verified emails come from
`/user/emails`: the primary one if verified, else another verified one. Groups are the
organizations someone belongs to, as `hackspace`, and their teams, as `hackspace/woodshop`, for
rules and allowed groups. An organization that restricts OAuth apps shows only once it approves
BAM!. For GitHub Enterprise, set the three endpoints to your server's, such as
`https://github.example.org/login/oauth/authorize` and `https://github.example.org/api/v3/user`.

##### Discord

1. In the Developer Portal, make an application, and under **OAuth2** add the redirect URI.
2. Paste its client ID and secret into BAM!, with the preset `discord`.

The preset asks for `identify email`, reads Discord's `verified` for the email, and the display
name, else the username. Discord gives no groups.

#### SAML 2.0

For organizations whose identity provider speaks only SAML, or prefers it, such as ADFS,
Shibboleth, and some Okta and Entra set-ups. BAM! is the service provider. Its settings:

- **The identity provider's metadata:** its **Metadata URL**, read again daily, or the
  **Metadata XML** pasted. When reading it again fails, the copy BAM! has is used. For metadata
  that lists several identity providers, such as a federation's, **Identity provider entity ID**
  picks one.
- **NameID format:** `persistent` by default. Use `email` only when an email at this provider never
  passes from one person to another, since whoever has the email then signs in as its account. A
  transient NameID says nothing of who signed in, and is refused.
- **The email, name and groups attributes:** blank tries the usual names, such as `mail`,
  `displayName` and `memberOf`, with their OIDs and ADFS and Entra names. Several separated by commas
  are tried in order. Groups are read as `groups` by rules, whichever attribute has them.
- **Ask for (AuthnContextClassRef):** classes to ask the identity provider to sign people in at,
  such as one that requires a second factor. The class it answers with is kept for
  [Requiring two-factor sign-in](#requiring-two-factor-sign-in), as OpenID Connect's `acr` is.
- **Sign-ins started at the provider:** off unless set to `on`. With it on, someone can choose BAM!
  from the identity provider's own page, and is signed in to BAM!'s home page.
- **Service provider key:** BAM! makes one when the provider is saved. Paste a PEM RSA key to use
  your own instead.

**What BAM! publishes:** its metadata for each SAML provider, at
`{base_url}/auth/saml/{key}/metadata`. Give the identity provider that address, or the file it
serves. It holds:

- BAM!'s entity ID, which is that same address;
- where answers come back, `{base_url}/api/v1/auth/providers/{key}/callback`, by HTTP-POST;
- BAM!'s certificate, for the identity provider to check BAM!'s requests and to encrypt assertions
  to BAM!.

**BAM!'s key:** it signs BAM!'s requests, and opens encrypted assertions. It is kept sealed with
`[auth] secret_key`, so without the key no certificate is made. A provider from the configuration
file gets its key from the file's `sp_key`, or goes without. A provider without a key still signs
people in, but its requests are unsigned and it cannot open encrypted assertions. **Make a new key
and certificate** under Admin replaces the key. The identity provider must then read BAM!'s
metadata again, and until it does, it encrypts to the old key, which can no longer open them.

**What is checked:**

- **Signatures:** the response, or the assertion in it, must be signed by a certificate in the
  identity provider's metadata. Everything is read from the element whose signature was checked,
  never from the rest of the document, so a signature cannot be wrapped around something else.
  A response with more than one assertion, or with a DOCTYPE, is refused.
- **Conditions:** the assertion must be for BAM!'s entity ID, its `NotBefore` and `NotOnOrAfter`
  must hold, with three minutes allowed either way for the clocks, and it must be from the
  identity provider. Its bearer confirmation must be for this address, for the sign-in BAM!
  started, and not expired.
- **Replays:** an assertion signs in once. Its ID is kept until it expires.
- **Encrypted assertions** are opened with BAM!'s key: AES-GCM or AES-CBC, with the key sealed by
  RSA-OAEP. RSA PKCS #1 v1.5 key transport is refused.
- **The identity provider's certificate** must be valid at the time, as its metadata gives it.

**Coming back by POST:** the identity provider sends the browser back by posting a form from its own
site, which does not carry BAM!'s sign-in cookie. BAM! answers with a page that posts the same form
again from BAM!'s own site, so the cookie comes with it. A browser without JavaScript shows a
**Continue signing in** button instead.

#### Who signs in as whom

Signing in matches by provider and the provider's stable ID for the person, its *subject*: OIDC's
`sub`, or SAML's persistent NameID. It never matches by email alone, so an email changed at the
provider cannot take over another account. Each match is an *identity*, kept in `user_identities`.
An account may have identities at several providers, and a password or not.

When someone signs in and no identity matches:

1. **Linking by email,** when it is on for the provider, links them to the account with the same
   email, but only if the email is verified. A provider's email counts as verified only when the
   provider says so (OIDC's `email_verified`), or when the admin says its emails can be trusted.
   A person without an account (see [Person fields](data-model.md#person-fields)) with that email
   is joined the same way: the identity is linked to them, they can sign in from then on, and the
   fields naming them are already theirs. Only someone with no password and no identity is joined;
   an unverified email joins no one. They join with no roles but those the provider's rules give.
2. **Otherwise, an account is made,** when that is on for the provider. It gets the email and name
   the provider gives, no password, and the provider's default roles. With none, the account can
   see nothing until someone gives it a role. Accounts can be limited to emails at given domains,
   or to members of given groups. The email must be verified.
3. **Otherwise, signing in is refused,** and the sign-in page says why.

#### Roles and scopes from claims

A provider's rules give roles from what it says. Each names a claim or attribute (such as
`groups`), a value it must hold (such as `makerspace-admins`), and a role, optionally narrowed to
scopes.

- **Re-applied at every sign-in.** Roles a rule gave are added, or taken away, to match the claims.
- **Roles given by hand are left alone.** Each of a user's roles records who gave it: a rule of a
  provider, or a person. A role an admin keeps when saving someone's roles stays the rule's. A role
  given both by hand and by a rule stays given by hand. Deleting a provider leaves the roles its
  rules gave, as given by hand.
- **Anti-lockout still holds.** A sign-in whose claims would take `users.manage` from the last
  person holding it keeps that role, and records `auth.role_kept` in history.

#### Linking accounts

- **By hand:** whoever manages users links an identity under **Identities** on the Users page,
  naming the provider and the subject.
- **By oneself:** someone signed in links a provider under **Your account**. They sign in at the
  provider, and the identity that comes back is linked to their account.
- **Unlinking** is refused when it would leave the account with no way to sign in: no other
  identity, and no password, or passwords off.

#### Turning passwords off

An instance setting under **Admin › Sign-in**. It cannot be turned off while no provider is
enabled.

- **While it is off,** the sign-in page shows only the providers' buttons, a password does not sign
  in, and setting passwords is hidden.
- **Break-glass:** those who hold `users.manage` can still sign in with a password, at a separate
  page, `/signin/local`. Each time records `auth.break_glass`, which notification channels can
  subscribe to.
- **On the command line,** `bam admin password` and `bam admin create` still work, so someone locked
  out, or a provider that is down, never shuts everyone out.

#### Sessions and second factors

A sign-in through a provider starts an ordinary session, so everything after it works as with a
password, the MCP consent page included. Whether BAM!'s own second factor is asked for after the
provider is in [Requiring two-factor sign-in](#requiring-two-factor-sign-in). Deactivating
a user still ends their sessions and tokens, whatever the provider says. Signing out of BAM! does
not sign out at the provider.

The sign-in through a provider is bound to the browser by a cookie, sent only to the providers'
routes, that lasts ten minutes and is used once. A failed sign-in counts against the client's
address, as wrong passwords do (see [Too many wrong passwords](#too-many-wrong-passwords)).

**The test protocol:** with `[auth] test_protocol` on (`BAM_AUTH_TEST_PROTOCOL`), a protocol named
`test` signs in as whoever its provider's settings name, and BAM! serves a test OpenID Connect
provider at `{base_url}/test-oidc`, client `bam` and secret `bam-test-secret`, whose sign-in form
signs in as whoever is typed into it. They are for automated tests alone: never turn them on for an
instance people use.

### Forgotten passwords

Whoever manages users sets a new password for someone who has forgotten theirs. Unless **Forgot
password** is on (see [Account emails](#account-emails)), the sign-in page says whom to ask.
Setting someone's password signs them out everywhere, except in the session making the change
when it is their own.

On the command line, `bam admin password --email … --password …` does the same for anyone, such
as an admin who is locked out. It changes nothing else about the account. (`bam admin create`
would also make them an admin.)

### Account emails

With email set up (see [Notifications](notifications.md#email)) and `[server] base_url` set, so
that links lead back here, BAM! emails people links about their own accounts. Each link works once,
until it expires, and only its hash is stored, as sessions' are. A new link replaces one of the same
kind not yet used. They are sent whatever someone chose for email to their roles, since they are
about their own account.

#### Invitations

Whoever manages users can **email someone an invitation** in place of setting their password, when
making their account: **Email them an invitation instead** on the Users page (`"invite": true`).
The account has no password until the invitation is accepted.

- **The link** leads to a page that sets a password, while passwords are on, and offers the
  identity providers' buttons. Signing in through one links that identity to the invited account.
  Either way, they are then signed in.
- **It works once, for seven days.** One expired or used is refused, and **Send invitation**
  sends a new one, which the old one stops working for.

#### Password reset

**Off unless turned on,** under **Admin › Sign-in** by those who manage settings: **Offer Forgot
password on the sign-in page**.

- **The sign-in page offers Forgot password,** which asks for an email. It always answers the same
  way, whether or not the email has an account, so it cannot be used to find accounts.
- **The link** works once, for 30 minutes.
- **Using it signs them out everywhere,** and emails them that their password changed. They then
  sign in with it, and their second factor if they have one.
- **Each request counts as a failed sign-in** against the address and the account it names (see
  [Too many wrong passwords](#too-many-wrong-passwords)), so it is held back as they are.
- **Never while passwords are off** (see [Turning passwords off](#turning-passwords-off)).

#### Removing a lost second factor by email

**Off unless turned on,** under **Admin › Sign-in**: **Let someone who has lost their second factor
remove it by email**.

- **After their password,** the second step of signing in offers **Email me a link to remove my
  second factor**. The link works once, for 30 minutes, and its page warns what it does.
- **Using it removes their second factors** and signs them out everywhere, recorded as
  `auth.factors_reset` by themselves, and emails them that it was removed.
- **It makes email a way around two-factor sign-in:** whoever can read their email and knows their
  password can sign in. Admin › Sign-in says so.
- **Not for those whose roles require two-factor sign-in** (see
  [Requiring two-factor sign-in](#requiring-two-factor-sign-in)), unless **Also for those whose
  roles require two-factor sign-in** is on. It is checked again when the link is used.

#### Notices

Someone is emailed when their password changes, whoever changes it, and when a second factor is
added to their account or removed from it, and when one of their recovery codes is used. A change
they did not make does not go unseen.

## Your account

### Display name

Each user has a display name of up to 255 characters. Asset pages show people by it: borrowers,
history, the loans list, and report and label bindings. Asset pages, lists, history and the loans
list never show emails.

- A name is required. Admins give it when creating a user and can change it later, and anyone
  signed in can change their own. A blank name is refused (`400`).
- A user from before names were required, or the first admin made by `bam admin create` without
  `--name`, shows as "Unnamed user" until named. The home page's setup checklist asks an admin
  with no name to give one.
- To the signed-in user, what they did themselves reads "you".
- The header and the user list show the email when there is no name.
- The lending form names people by name, adding the email only to tell apart two people with the
  same name.
- History keeps the name as it was when the change was made. Discord messages name the person by
  it.

The name is for display only. Signing in is always by email.

### Theme

Each user chooses whether the web interface is `light` or `dark` for them, or `system`, following
their system's setting. Everyone starts at `system` until they choose.

Wherever the API returns a user (`GET /api/v1/auth/me` among them), it carries the theme as
`theme`, always one of those three. Anyone signed in changes their own. Any other value is
refused (`400`).

The theme is a preference rather than a change to the account, so changing it leaves the user's
`updated_at` as it was. A backup keeps it with the person, and restoring a backup made before
migration `048` puts everyone at `system`. No MCP tool reaches it, since an assistant sees no
interface.

## Roles and permissions

A user holds permissions only through roles. A user given several roles holds the union of what
they grant, each as narrowed to its scopes (see [Narrowing a role](#narrowing-a-role-to-scopes)).

### Permissions

Permissions are `resource.action` keys. Keys were written `resource:action` until migration `019`
renamed them, so that a key can carry a scope (`tools:assets.write`).

| Permission | Allows | Scopable |
|---|---|---|
| `assets.read` | Reading assets. | Yes |
| `assets.write` | Creating and editing assets, their codes, attachments (photos among them) and links, and marking an asset `ok` again. | Yes |
| `assets.delete` | Archiving and restoring assets. | Yes |
| `assets.report` | Reporting an asset as needing maintenance or out of service. | Yes |
| `assets.borrow` | Borrowing assets (see [Stock and lending](stock-and-lending.md)). | Yes |
| `assets.lend` | Lending assets (see [Stock and lending](stock-and-lending.md)). | Yes |
| `assets.scan` | Scanning (see [Scanning](scanning.md)). | Yes |
| `assets.track` | Seeing the people around assets: who borrowed them, who changed them, and the loans list. | Yes |
| `purchases.read` | Seeing what assets cost: their price, currency, order number and invoice, what contracts cost, the prices in vendors' catalogs, and purchase orders' prices (see [Seeing prices](#seeing-prices)). | Yes |
| `assets.confirm` | Confirming assets in categories that use review states, and filing new ones as confirmed (see [Review states](review.md)). | Yes |
| `assets.verify` | Verifying confirmed assets, or returning them to draft with a note saying why (see [Review states](review.md)). | Yes |
| `assets.hook` | Triggering hooks (see [Hooks](hooks.md)). | Yes |
| `events.read` | Receiving events live (see [Hooks](hooks.md)). | Yes |
| `stock.record` | Using, restocking and flagging stock, without being able to edit assets. | Yes |
| `flows.run` | Running flows (see [Flows](flows.md)). | Yes |
| `flows.manage` | Making flows (see [Flows](flows.md)). | No |
| `templates.manage` | Managing label templates (see [Labels](labels.md)). | No |
| `labels.print` | Printing labels (see [Printing](printing.md)). | No |
| `reports.manage` | Managing reports (see [Reports](reports.md)). | No |
| `categories.manage` | Managing categories, including moving them under others, and relation types. | No |
| `vendors.manage` | Creating and editing vendors, their contacts and attachments, their contracts, and purchase orders to them (see [Vendors](vendors.md), [Contracts](contracts.md) and [Purchase orders](purchase-orders.md)). | Yes |
| `comments.write` | Writing comments on what one may read, attaching files to it while writing, and editing and deleting one's own (see [Comments](comments.md)). | Yes |
| `comments.moderate` | Deleting anyone's comments (see [Comments](comments.md)). | Yes |
| `icons.manage` | Changing what every icon is drawn with: the icon library, the palette and the icon settings (see [Icons](icons.md)). | No |
| `settings.manage` | Managing settings. | No |
| `users.manage` | Managing users, roles, scopes and API tokens. It includes `people.add`. | No |
| `people.add` | Adding people without accounts, to name in person fields (see [Person fields](data-model.md#person-fields)). | No |
| `assistant.use` | Seeing the assistant beside the page, and talking with it, which acts as the person with their own permissions, once an admin turns it on (see [The assistant beside the page](assistant.md)). | No |
| `lasers.run` | Starting a laser's job with BAM!'s *Start*, tracing its outline on the workpiece, and resuming it (see [Lasers](printing.md#lasers)). Preparing a job is `labels.print`, and anyone with it may pause or cancel one. An assistant over MCP, a flow or an incoming hook never starts a laser, whatever it holds, and an API token only with `lasers.run`. | No |

### Scopes

A scope is a label put on categories and passed down to their subcategories. Vendors are put in
scopes too, which decide who sees them (see [Vendors](vendors.md#who-sees-a-vendor)). Its name is 1 to 64
lowercase letters, digits, `-` and `_`.

An asset is in the scopes of its categories and of every ancestor of theirs. When there are none,
it is in the reserved `default` scope. So an asset leaves `default` when it joins a category with
a scope, and returns to it when it leaves the last one.

Two scopes are built in:

- `default`, holding every asset in no other scope. It cannot be put on a category (`409`).
- `public`, which labels no categories until someone puts some in it (see
  [The public role](#the-public-role)).

Neither can be renamed or deleted (`409`). Renaming any other scope carries along the roles'
permissions in it and the roles narrowed to it. A scope can be deleted only when no category,
vendor, role or role assignment uses it (`409` otherwise). Deleting it also deletes the narrowing of
archived users' and revoked tokens' roles to it.

Only `users.manage` creates, renames and deletes scopes, and puts them on categories, since scopes
decide who reaches what. Moving categories under others changes their assets' scopes, so
`categories.manage` is trusted with that.

### Scoped permissions

A role holds each permission either in every scope (`assets.write`) or in some
(`tools:assets.write`, `default:assets.write`). Only the asset permissions are scopable:
`assets.read`, `assets.scan`, `assets.hook`, `events.read`, `assets.track`, `assets.write`,
`assets.delete`, `assets.report`, `assets.borrow`, `assets.lend`, `stock.record`, `purchases.read`,
`assets.confirm`, `assets.verify` and `flows.run`, and `vendors.manage`, since vendors are in scopes as assets are (see
[Vendors](vendors.md)), and `comments.write` and `comments.moderate`, in the scopes of what
comments are on (see [Comments](comments.md#who-sees-and-writes-them)).

A permission in any one of an asset's scopes is enough. A permission held in every scope covers
each scope, and a role given it both ways keeps the wider.

### How checks work

Holding a permission in any scope opens its endpoints and pages. Each then checks the asset:

- An asset the requester can neither read nor act on as asked answers `404`, as if it did not
  exist.
- An asset they can read but not act on answers `403`.

Lists, exports, reports, loans and labels hold only the assets the requester can read. An asset's
links and location leave out the assets the requester cannot read.

Some actions check more than one set of scopes:

- Creating an asset, or moving one to other categories, needs `assets.write` in the scopes it ends
  up in as well as the scopes it was in.
- A link needs `assets.write` on both assets.
- An import checks each row and link the same way, and reports what it may not change as
  problems. Assets it cannot read are, to it, unknown.
- Flagging a category running low needs `stock.record` in the scopes its assets get from it.

`GET /api/v1/auth/me` lists scoped keys as they are, so the web app shows each asset only what its
scopes allow.

### Narrowing a role to scopes

Assigning a role to someone may narrow it to some scopes, as "Editor, in `tools`". That way a
role need not be copied for each combination of scopes.

Naming no scopes is the usual case, and works as it always has: the role applies in every scope
its permissions name. Narrowed, a role grants:

- each scopable permission it holds in every scope (`assets.write`) in each scope the assignment
  names (`tools:assets.write`);
- each scopable permission it holds in some scopes (`kitchen:assets.write`) in the scopes both
  name, and not at all if they share none;
- none of its permissions that are not scopable (`users.manage`, `labels.print` and the rest). So
  "Admin, in `tools`" manages no one.

Narrowing limits where a role applies. It never adds to what the role grants.

The union principle holds across assignments. A user holding the same role unrestricted and
narrowed, or several roles, holds what each assignment gives. The same role given twice is one
assignment, over the union of its scopes, or unrestricted if either is.

`default` and `public` can be named like any other scope. The public role, held by everyone,
cannot be narrowed (`400`).

API tokens' roles are narrowed the same way (see [API tokens](#api-tokens)).

### Seeing people

Without `assets.track` for an asset:

- its loan says only that it is lent out, and to the requester if it is theirs;
- its history leaves out who did each thing and who borrowed it, and changes to person fields;
- `{{loan.borrower}}` is blank;
- its person fields say that someone is set, but not who, unless it is the requester or the field
  is public (see [Person fields](data-model.md#person-fields)).

With it, people show by name. Emails never show on asset pages, lists, history, labels or the
loans list.

### Seeing prices

Without `purchases.read` for an asset, in one of its scopes, its price, currency, order number and
invoice are left out of:

- its page and the asset list, which mark it `"prices_hidden": true`;
- its attachments and photos, which leave out the one named as its invoice, and answer `404` for
  it when it is downloaded, renamed or removed (see [Purchases](data-model.md#purchases));
- its history, with any edit that changed nothing else;
- the CSV and Excel exports, and reports, labels and sheets, whose `{{asset.purchase.price}}`,
  `{{asset.purchase.currency}}` and `{{asset.purchase.order_number}}` are blank;
- packages, and MCP, which reads through the API.

Backups are not filtered: one holds everything, as making one needs `users.manage` and
`settings.manage` (see [Backups](backups.md)).

The same permission sees the prices in vendors' catalogs, in one of the vendor's scopes, or anywhere
for a vendor in none (see [Catalogs](vendors.md#catalogs)). A catalog's price is a list price, not
what was paid, but it says what the space spends, so it is kept with what was paid.

Its vendor and dates still show to whoever reads it, as the vendor's page lists the assets bought
from it. Editing it, with `assets.write`, leaves what they cannot see as it is, and an import
giving those details for such an asset is refused (see [Purchases](data-model.md#purchases)).

Migration `070` grants `purchases.read` to every role that reads assets, where it reads them, the
public role among them, so that no one sees less than before. Take it from a role to hide prices.

A contract's cost, currency and billing need `purchases.read` in its vendor's scopes, or in every
scope for a vendor in none, and are left out in the same places, and of the assets it covers (see
[Contracts](contracts.md#who-sees-a-contract)).

A purchase order's prices, totals and currency follow the same rule as a contract's cost, and are
left out of its answers, its PDF, its history and MCP (see
[Purchase orders](purchase-orders.md#who-sees-an-order)). Receiving one needs `stock.record` for
what it restocks and `assets.write` where the assets it makes go, not `vendors.manage`.

### The public role

Everyone holds the `public` role, signed in or not, and API tokens hold it too. It is not given to
users, who all have it already, and `GET /api/v1/roles` marks it `public`.

- It may only look: `assets.read`, `assets.scan` and `purchases.read`, in any scopes.
- It keeps its name and cannot be deleted (`409`).
- It cannot be narrowed to scopes (`400`).

It starts with `public:assets.read`: reading the assets in the `public` scope. That scope labels
no categories until someone puts some in it, so nothing is public until then.

Signed in, the public role's permissions add to the user's own wherever they apply. Signed out,
they apply only to an asset's own page:

- `GET /api/v1/assets/{id}`, the asset's photos and attachments, and its history, without people (see
  [Stock and lending](stock-and-lending.md));
- `GET /api/v1/generators`.

The catalog (the asset list, categories, fields, templates, reports and the rest) answers `401`.
The web app opens only the asset page, with a link to sign in. `GET /api/v1/auth/me` answers
signed-out visitors with `{"user": null}` and the public role's permissions.

A role or scope already named `public` before migration `021` is renamed `public (renamed)` or
`public-renamed`, so that what it allowed never passes to everyone.

### The admin role

Seeding grants every permission to the `admin` role, including permissions added by later
releases. The `admin` role is protected: it cannot be renamed, edited or deleted (`409`), since
seeding recreates it by name and grants it every permission at each start.

### Managing roles

Roles are listed with those made here first, by name regardless of case, then the built-in
`admin` and `public`. Each is listed with its permissions, how many users who are not archived
hold it (`user_count`), and whether it is `protected`.

- Role names are unique and up to 64 characters.
- Permissions must be known keys, each alone or, if scopable, as `scope:key` naming an existing
  scope.
- Deleting a role takes it from everyone who holds it, tokens included.
- Editing and deleting roles follow the anti-lockout rule.

### Anti-lockout

Any deletion, deactivation, role removal, role narrowing or role edit that would leave no active
user holding `users.manage` is rejected.

A role narrowed to scopes grants no `users.manage`, so only users holding it through a role
assigned in every scope count. Tokens do not count.

Each such change runs in one transaction that first takes the `users_manage` row of the
`app_locks` table. Concurrent changes are therefore checked one after another, and two admins
cannot remove each other at the same time.

## Sessions

Sessions are stateful. They are stored in the active database, in the `sessions` table, and
referenced by an `HttpOnly`, `SameSite=Lax`, `Secure` cookie.

The cookie holds a random 256-bit token. The table stores only its SHA-256 hash, so a copy of the
database cannot be used to sign in.

## API tokens

Scripts and dashboards authenticate with `Authorization: Bearer <token>` instead of a session.

A token has:

- a name;
- roles whose permissions it holds, as a user holds theirs, each narrowed to some scopes or not;
- an optional expiry;
- the time it was last used, written at most once a minute.

Its secret is `bam_` followed by 64 hex digits (256 random bits). It is returned once, when the
token is made. As with a session token, only its SHA-256 hash is stored.

### Rules for tokens

- Only a signed-in session makes tokens, so that a token, or an assistant led astray by what it
  read, cannot leave itself a way back in. For the same reason, only a signed-in session makes,
  changes or renews an incoming hook (see [Hooks](hooks.md#who-saves-one)).
- One holding `users.manage` lists and revokes tokens, as it lists live connections.
- No token can use the password or logout endpoints, which answer it `403`. Only a personal token
  can use the rest of the account's endpoints (`/api/v1/auth/me`, `name`, `theme`).
- A revoked, expired or unknown token is answered `401` at once, even with a session cookie
  alongside.
- Revoked tokens stay listed.
- Tokens do not count toward the anti-lockout rule.
- Deleting a role takes it from tokens too.

### History

Changes a token makes are recorded in history under its name (`"actor": {"token": true, ...}`).
Those an incoming hook makes are recorded under its name, as `"incoming": true` (see
[Hooks](hooks.md#incoming-hooks)).

A change made through an MCP tool also records the assistant it came through, as `"via"` on the
actor. The assistant is named as the MCP client names itself (`clientInfo`, its title if it has
one), cleaned as uploaded image names are. It is `MCP` when the client gives no name, as clients
of protocol versions before `2026-07-28` do not on each request. History shows "Ada, via Claude
Desktop", and so do Discord messages and live events.

Only a tool's request can carry `"via"`: nothing a request from outside sends sets it. Labels
printed later by the spooler, from the print queue, are recorded without it.

### Personal tokens

A signed-in user makes tokens for themselves, for an assistant (see [MCP](mcp.md)), under
*Your account › Assistants*. Each user lists, makes and revokes only their own, from a signed-in
session. Anyone else's is `404`.

A personal token holds no roles. It acts as its user, with their permissions as they are at each
request, so a role taken from the user is gone at the token's next request. What it changes is
recorded as theirs.

A personal token can:

- borrow as its user;
- answer `GET /api/v1/auth/me` as them;
- change their name and theme;
- list and revoke their own personal tokens, itself among them.

Like any token, it cannot change the password, sign out, or make tokens (`403`).

Deactivating or archiving the user revokes their personal tokens, as it ends their sessions, so
turning the user back on brings no assistant back.

`GET /api/v1/tokens` lists personal tokens with every other token, with `user_id` and
`user_name`, and `users.manage` revokes any of them there. A personal token's live connections
(see [Hooks](hooks.md)) count against its user.

## API

### Users and accounts

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/auth/me` | Signed in, signed out, or a personal token | The current user, their permissions after narrowing, and `role_scopes`. |
| `PUT /api/v1/auth/name` | Signed in, or a personal token | Change your own name, with `{"name": "..."}`. |
| `PUT /api/v1/auth/theme` | Signed in, or a personal token | Change your own theme, with `{"theme": "dark"}`. Answers `{"user"}`, as a rename does. |
| `PUT /api/v1/auth/email` | Signed in, or a personal token | Turn email sent to your roles off or on, with `{"notifications": false}` (see [Notifications](notifications.md#who-receives-it)). Answers `{"user"}`. |
| `GET /api/v1/auth/me/factors` | Signed in, or a personal token | Your second factors, never their secrets, a passkey's with its `name` and `rp_id`; whether apps are `available`: whether the configuration has a secret key; whether `passkeys` can be added from this browser's host; and `recovery_codes_left`. |
| `POST /api/v1/auth/me/factors/totp` | Signed in | Start setting up an authenticator app: answers its `secret` as text, its `otpauth://` `uri`, and a QR code of it as `qr_svg`. |
| `POST /api/v1/auth/me/factors/totp/confirm` | Signed in | Turn the app on, with `{"code"}` from it. The first factor answers `recovery_codes` too, shown only then. |
| `POST /api/v1/auth/me/factors/passkey` | Signed in | Start adding a passkey: answers `public_key`, the options for `navigator.credentials.create`, binary values base64url. |
| `POST /api/v1/auth/me/factors/passkey/confirm` | Signed in | Add the passkey, with `{"name", "client_data_json", "attestation_object"}` as the browser answered, base64url. The first factor answers `recovery_codes` too. |
| `POST /api/v1/auth/me/factors/passkey/challenge` | Signed in | A challenge for one of your passkeys, to confirm it is you: answers `public_key`, the options for `navigator.credentials.get`. |
| `POST /api/v1/auth/me/factors/recovery` | Signed in | New recovery codes, replacing all of them, with `{"code"}` from an app or `{"passkey"}`. Answers `recovery_codes`. |
| `DELETE /api/v1/auth/me/factors/{id}` | Signed in | Remove a factor, with `{"password"}` and `{"code"}` or `{"passkey"}`. |
| `GET /api/v1/auth/login/factor` | At the second step of signing in | The kinds of second factor that can finish it, as `factors`. |
| `POST /api/v1/auth/login/factor/passkey` | At the second step of signing in | A challenge for your passkeys: answers `public_key`, the options for `navigator.credentials.get`. |
| `GET /api/v1/auth/me/favorites`, `PUT` and `DELETE /api/v1/auth/me/favorites/{kind}/{id}` | Signed in, or a personal token | Your favorites, and starring or unstarring one (see [Favorites](favorites.md#api)). |
| `GET /api/v1/users` | `users.manage` | List users, each with `role_ids` and `roles`, `two_factor`, and `sign_in_delayed_until` while wrong passwords hold their sign-ins back. |
| `POST /api/v1/users` | `users.manage` | Create a user; `name` is required. With `"invite": true` and no `password`, email them an invitation. |
| `POST /api/v1/users/{id}/invite` | `users.manage` | Email someone an invitation again; the one before stops working. |
| `PUT /api/v1/users/{id}/name` | `users.manage` | Change a user's name, with `{"name": "..."}`. |
| `PUT /api/v1/users/{id}/password` | `users.manage` | Set a user's password, with `{"password"}`. Signs them out everywhere, and clears their sign-in delay. |
| `DELETE /api/v1/users/{id}/sign-in-delay` | `users.manage` | Let a user sign in again at once, forgetting their wrong passwords. |
| `DELETE /api/v1/users/{id}/factors` | `users.manage` | Remove a user's second factors, and sign them out everywhere. Answers `{"removed"}`. |
| `GET /api/v1/users/{id}/identities` | `users.manage` | A user's identities at providers, and `role_grants`: which of their roles a provider's rule gave, by role ID. |
| `POST /api/v1/users/{id}/identities` | `users.manage` | Link an identity by hand, with `{"provider_id"}` or `{"provider_key"}`, and `{"subject"}`. |
| `DELETE /api/v1/users/{id}/identities/{identity}` | `users.manage` | Unlink one, unless it is their last way to sign in. |
| `GET /api/v1/auth/me/identities` | Signed in, or a personal token | Your identities, and which of your roles a rule gave. |
| `DELETE /api/v1/auth/me/identities/{identity}` | Signed in | Unlink one of yours, unless it is your last way to sign in. |
| `POST /api/v1/auth/login/local` | Anyone | Sign in with a password at the break-glass page: while passwords are off, only for those holding `users.manage`. |
| `GET /api/v1/auth/providers/public` | Anyone | The sign-in page's buttons (`key`, `label`, `icon`), whether `local_passwords` are on, and whether `password_reset` and `factor_recovery_email` are offered. |
| `GET /api/v1/auth/providers/{key}/start` | Anyone | Send the browser to the provider, then back to `next`. With `link=1`, signed in, link the identity that comes back; with `invite=…`, link it to the invited account and sign in. |
| `GET /api/v1/auth/invite?token=…` | Anyone | Whom an invitation is for, whether passwords are on, and the providers' buttons. |
| `POST /api/v1/auth/invite` | Anyone | Accept an invitation with `{"token", "password"}`, signing in. |
| `POST /api/v1/auth/password-reset` | Anyone | Email a link to set a new password, with `{"email"}`. Answers `202` the same whatever the email. |
| `GET /api/v1/auth/password-reset?token=…` | Anyone | Whose password a link sets. |
| `POST /api/v1/auth/password-reset/confirm` | Anyone | Set a new password with `{"token", "password"}`, signing them out everywhere. |
| `POST /api/v1/auth/login/factor/recover` | At the second step of signing in | Email a link that removes a lost second factor. |
| `GET /api/v1/auth/factor-recovery?token=…` | Anyone | Whose second factor a link removes. |
| `POST /api/v1/auth/factor-recovery` | Anyone | Remove it with `{"token"}`, signing them out everywhere. |
| `GET` or `POST /api/v1/auth/providers/{key}/callback` | Anyone | The provider's answer. One posted without the sign-in's cookie, as from a SAML identity provider's site, is answered with a page that posts it again from here. |
| `GET /auth/saml/{key}/metadata` | Anyone | BAM!'s SAML metadata for a SAML provider, enabled or not, as `application/samlmetadata+xml`. |
| `GET /api/v1/auth/providers` | `settings.manage` | Every provider, with its rules and which secrets are set; the `protocols` and their fields; `local_passwords`; the roles and scopes rules can give; and `passkey_host`, base_url's host, with `passkeys_elsewhere`, how many passkeys were added at another. |
| `POST /api/v1/auth/providers`, `PUT /api/v1/auth/providers/{id}` | `settings.manage` | Save a provider, given whole. A secret left blank keeps the one stored. With `renew_keys`, a SAML provider's key is made again. One from the configuration is refused with `409`. |
| `DELETE /api/v1/auth/providers/{id}` | `settings.manage` | Remove a provider, with the identities at it. |
| `PUT /api/v1/auth/settings` | `settings.manage` | Turn passwords off or on, with `{"local_passwords": false}`. |
| `PUT /api/v1/users/{id}/roles` | `users.manage` | Set a user's roles. |

`PUT /api/v1/users/{id}/roles` takes `{"roles": [{"role_id", "scope_ids": [...]}]}`. An empty or
missing `scope_ids` means every scope. It still takes `{"role_ids": [...]}`, each role in every
scope. It answers with both `role_ids` and `roles`.

`GET /api/v1/auth/me` lists the keys a user holds after narrowing, and in `role_scopes` the names
of the scopes each narrowed role holds, by role name.

### Roles and scopes

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/roles` | `users.manage` | List roles, with `user_count`, `requires_two_factor`, `protected` and `public`. |
| `POST /api/v1/roles` | `users.manage` | Create a role, with `{"name", "description", "permissions", "requires_two_factor"}`. |
| `PUT /api/v1/roles/{id}` | `users.manage` | Change a role, with the same body; `requires_two_factor` left out keeps what it was. |
| `PUT /api/v1/roles/{id}/two-factor` | `users.manage` | Set whether a role requires two-factor sign-in, with `{"required"}`; the admin role's too. |
| `DELETE /api/v1/roles/{id}` | `users.manage` | Delete a role. |
| `GET /api/v1/scopes` | `users.manage` or `categories.manage` | List scopes. |
| `POST /api/v1/scopes` | `users.manage` | Create a scope, with `{"name"}`. |
| `PUT /api/v1/scopes/{id}` | `users.manage` | Rename a scope, with `{"name"}`. |
| `DELETE /api/v1/scopes/{id}` | `users.manage` | Delete an unused scope (`409` otherwise). |
| `GET /api/v1/categories/{id}/scopes` | `users.manage` or `categories.manage` | A category's own scopes. |
| `PUT /api/v1/categories/{id}/scopes` | `users.manage` | Set them, with `{"scope_ids": [...]}`. |

`GET /api/v1/scopes` gives, for each scope:

- how many categories it labels;
- how many roles hold permissions in it (`role_count`);
- how many roles of users who are not archived, and of tokens not revoked, are narrowed to it
  (`assignment_count`).

A scope that incoming hooks' roles are narrowed to cannot be deleted either (see
[Hooks](hooks.md#incoming-hooks)).

### Tokens

| Method and route | Permission | Purpose |
|---|---|---|
| `GET /api/v1/tokens` | `users.manage` | List all tokens, personal ones with `user_id` and `user_name`. |
| `POST /api/v1/tokens` | `users.manage`, from a signed-in session | Make a token. |
| `DELETE /api/v1/tokens/{id}` | `users.manage` | Revoke any token. |
| `GET /api/v1/auth/tokens` | Signed in, or a personal token | List your own personal tokens. |
| `POST /api/v1/auth/tokens` | Signed in, from a session | Make a personal token, with `{"name", "expires_at"}`. |
| `DELETE /api/v1/auth/tokens/{id}` | Signed in, or a personal token | Revoke one of your own personal tokens. |

`POST /api/v1/tokens` takes `{"name", "roles", "expires_at"}`. `roles` is given as a user's are,
each narrowed to some scopes or not; `role_ids` gives roles in every scope instead. `expires_at`
is an RFC 3339 time, or `null` for never.

## Storage

| Tables | Hold |
|---|---|
| `users`, `roles`, `permissions`, `user_roles`, `role_permissions` | Users, roles and their permissions. |
| `scopes`, `category_scopes`, `role_permission_scopes` | Scopes, the categories they label, and the scopes of roles' permissions. |
| `user_role_scopes`, `api_token_role_scopes` | Roles assigned in some scopes only, to users and to tokens. |
| `sessions` | Sessions, by the SHA-256 hash of their token. |
| `sign_in_throttles` | Wrong passwords being counted, by account and by address, and until when each waits. |
| `app_locks` | The `users_manage` row, taken by each change the anti-lockout rule checks, and the `sign_in` row, taken to count a wrong password. |

### Migrations

| Migration | Change |
|---|---|
| `004` | The `app_locks` table, for the anti-lockout rule. |
| `005` | Sessions stored by the SHA-256 hash of their token. |
| `010` | Users' display names. |
| `011`, `013`, `014` | Grant `stock.record`, `assets.report`, `assets.borrow` and `assets.lend` to every role that had `assets.write`, so nobody loses what they could do. |
| `012` | API tokens. |
| `017` | Grants `reports.manage` to every role that had `templates.manage`. |
| `019` | Scopes (`scopes`, `category_scopes`, `role_permission_scopes`); renames permission keys from `resource:action` to `resource.action`. |
| `020` | Grants `assets.track` to every role that reads assets, in the scopes it reads them in, since reading showed people until then. |
| `021` | The `public` role and scope; renames any role or scope already named `public`. |
| `031` | Personal tokens. |
| `035` | Grants `icons.manage` to every role that manages categories, as icons are part of how categories look. |
| `041` | Roles narrowed to scopes (`user_role_scopes`, `api_token_role_scopes`). |
| `043` | Grants `flows.manage` to every role that manages categories, and `flows.run` to every role that writes assets, in the same scopes. |
| `068` | Grants `vendors.manage`, in every scope, to every role that manages categories. |
| `069` | Grants `comments.write` to every role that writes assets, and `comments.moderate` to every role that archives them, in the same scopes. |
| `070` | Grants `purchases.read` to every role that reads assets, in the scopes it reads them in, since reading showed prices until then. |
| `078` | Grants `people.add` to every role that manages users, which includes it. |
| `079` | Grants `assets.confirm` to every role that writes assets, in the scopes it writes them in, so what it adds is taken as true as before until a category uses review states. `assets.verify` goes to the admin role only. |
| `081` | Adds `assistant.use`, which goes to the admin role only. |
| `083` | Adds `lasers.run`, which goes to the admin role only. |
| `048` | Users' themes. |
| `051` | Counting wrong passwords (`sign_in_throttles`). |
| `052` | Whether email sent to a user's roles reaches them (`email_notifications`). |
| `054` | Second factors (`user_factors`) and the second step of signing in (`sign_in_challenges`). |
