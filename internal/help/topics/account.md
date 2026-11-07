# Your account

On **Your account** you can change the name others see, and your password. Changing your password signs you out everywhere else.

What you can do depends on your roles, listed on the same page with what each permission lets you do. A page you may not open says what it needs. If you forget your password, choose **Forgot password** on the sign-in page, where this BAM! offers it, and follow the link emailed to you; otherwise whoever manages users can set a new one for you. After several wrong passwords, signing in makes you wait a little before trying again, and says how long. Under **Email**, you can stop email that notification channels send to your roles from reaching you, stop reminders of what you borrow, and stop emails when a comment mentions you.

## Two-factor sign-in

Under **Two-factor sign-in**, you can ask for a second factor as well as your password, so your password alone is not enough: a passkey, or a code from an authenticator app.

- **A passkey** is a security key, such as a YubiKey, or your phone's or computer's screen lock. Choose **Add a passkey**, give it a name, and touch the key or unlock the device when your browser asks. You can add several. Signing in then offers **Use a passkey** after your password. A passkey works only at the address it was added at: if this BAM! moves to another, add yours again.
- **An app** on your phone, such as Aegis, Google Authenticator or 1Password: choose **Set up an authenticator app**, scan the QR code with the app, and enter the code it shows. Signing in then asks for the app's code after your password. Each code works once.

Setting up the first shows ten **recovery codes**, once. Download or print them, and keep them somewhere safe, apart from your phone and keys. If you lose your second factor, choose **Use a recovery code** when signing in and enter one: each works once. The page shows how many you have left and warns when you are running low; **Make new recovery codes** replaces them all, given a current second factor. Using one is recorded, and emails you when this BAM! sends email.

To remove a factor, choose **Remove…** beside it and give your password and a current second factor: a code from the app, or a passkey. If you lose your second factors and your recovery codes, ask whoever manages users to remove them: you can then sign in with your password and set one up again. Where this BAM! offers it, the second step of signing in can instead email you a link that removes them.

You are emailed when your password changes, and when a second factor is added or removed, so that a change you did not make does not go unseen.

Some roles require two-factor sign-in, the **admin** role among them. If you hold one and have no second factor, signing in asks you to set one up before anything else opens, and your last one cannot be removed.

## Signing in elsewhere

If BAM! has sign-in buttons for an identity provider your space runs, such as your organization's Google or Microsoft account, **Signing in elsewhere** lists the ones linked to your account. **Link** one to sign in through it from then on: you sign in there, and come back here. **Unlink** stops it, as long as you keep another way to sign in.

## Assistants

An assistant, such as Claude, can work with BAM! for you over MCP: find things, add them, lend and return them, print labels. Under **Assistants** on Your account, choose **New assistant**, name it, and copy what it needs to connect: the token is shown only once. The assistant acts as you, with whatever you may do at the time, and what it changes is recorded as yours. It cannot change your password or make tokens.

Revoke an assistant there when you stop using it. [Assistants](/help/assistants) says more. Whoever manages users can revoke it too, on [API tokens](/tokens), and an assistant stops when your account is turned off.

An assistant that can sign in, such as Claude, needs no token copied: give it the server's address and it asks you to sign in to BAM! and approve it, on a page that says what it will be able to do. It is then listed under **Assistants** like any other, renewing itself while you use it, until you revoke it.
