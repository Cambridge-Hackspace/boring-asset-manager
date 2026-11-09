# People and permissions

Those who manage users add people on the **Users** page, with their name and email. Everyone signs in with their email; asset pages show their name, never their email.

## People in fields

A **person field** names people: who looks after a machine, whom to ask about it, who trained on it. Typing part of a name offers the people you may choose, and fields follow someone when they are renamed. Names show to those who see who, and to anyone for a field marked **public**; others see that someone is set, but not who. Emails never show.

Someone who has no account yet can still be named. With **Add people**, typing a name that matches no one offers to add them as a **person without an account**: a name, and an email if you know it. They show on the **Users** page as unable to sign in. When they first sign in through an identity provider that links by verified email, they join that person, and their fields are already theirs. **Merge into…** on the **Users** page moves every field naming a person without an account to someone else, and their history says so.

## Wrong passwords

After 5 wrong passwords in 15 minutes, an account waits before its next try, longer after each further wrong one, up to 15 minutes. It is never locked. The **Users** page shows who is waiting, and **Clear sign-in delay** lets them try again at once; setting their password does too. A channel subscribed to *Sign-ins are held back* tells you when it starts, since someone may be guessing.

## Identity providers

Under **Admin › Sign-in**, those who manage settings add the identity providers your space already runs, such as Google Workspace or Microsoft Entra: each is a button on the sign-in page. A provider can make an account for someone the first time they sign in, and its **rules** give roles from what it says about them, such as membership of a group, checked again at every sign-in. Roles you give by hand are never taken away by a rule. On the **Users** page, **Identities** shows whom someone signs in as at each provider, and links or unlinks one by hand. Signing in with a password can be turned off there too; those who manage users can still sign in with one at */signin/local*.

## Invitations and account emails

Making someone's account, **Email them an invitation instead** sends them a link to set their own password, or sign in through a provider; it works once, for seven days, and **Send invitation** sends a new one. Under **Admin › Sign-in**, **Account emails** turns on **Forgot password** on the sign-in page, and removing a lost second factor by email. Each needs email set up.

## Roles

A **role** is a set of permissions, and people hold any number of roles. The **admin** role holds everything. The **public** role is everyone's, signed in or not, and can only look. **See prices** shows what assets cost: without it, a role reads assets with their price, order number and invoice hidden. Every role that read assets was given it where it reads them, so take it away to hide prices.

A role can **require two-factor sign-in**: whoever holds it signs in with a second factor, and someone without one sets one up at their next sign-in before anything else. The admin role requires it, until someone turns that off with **Stop requiring two-factor sign-in**, which is recorded. An identity provider's own second factor counts when it says one was used; each provider says what to do when it does not.

## Scopes

A **scope** is a label on categories, passed down to their subcategories. A role can hold permissions about assets in some scopes only: a keeper of the workshop edits the workshop's tools and nothing else. Assets in no scope are in *default*, and assets in *public* can be seen by anyone the public role allows.

A role can also be given to someone in some scopes only: on the **Users** page, under **Change roles**, tick the scopes under the role. They then hold its permissions about assets in those scopes, and none of its others, such as printing labels or managing users. Ticking none gives the role as it is, in every scope its permissions name. So one *Editor* role serves every bench: "Editor, in *woodshop*" for one person, "Editor, in *metalshop*" for another.

## API tokens

**API tokens** let scripts use BAM! with the roles you give them, each in some scopes only if you like. A token is shown once, when it is made; keep it safe, and revoke it when it is no longer needed.
