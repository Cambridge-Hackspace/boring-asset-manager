# People and permissions

Those who manage users add people on the **Users** page, with their name and email. Everyone signs in with their email; asset pages show their name, never their email.

## Roles

A **role** is a set of permissions, and people hold any number of roles. The **admin** role holds everything. The **public** role is everyone's, signed in or not, and can only look.

## Scopes

A **scope** is a label on categories, passed down to their subcategories. A role can hold permissions about assets in some scopes only: a keeper of the workshop edits the workshop's tools and nothing else. Assets in no scope are in *default*, and assets in *public* can be seen by anyone the public role allows.

## API tokens

**API tokens** let scripts use BAM! with the roles you give them. A token is shown once, when it is made; keep it safe, and revoke it when it is no longer needed.
