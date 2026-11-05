<!--
  Someone whose role requires two-factor sign-in, and who has no second
  factor, sets one up before anything else opens
  (docs/access.md#requiring-two-factor-sign-in).
-->
<script lang="ts">
  import { session } from '../lib/session.svelte';
  import TwoFactorSection from './TwoFactorSection.svelte';
</script>

<main id="main" class="mx-auto mt-16 max-w-xl space-y-4 px-4">
  <h1 class="text-2xl font-bold">Set up two-factor sign-in</h1>
  <p class="text-sm">
    A role you hold requires signing in with a second factor. Add a passkey, or set up an authenticator app on your
    phone, to go on; until then, nothing else here opens.
  </p>
  <TwoFactorSection ondone={() => void session.load()} />
  <p class="text-sm">
    Signed in as {session.user?.name || session.user?.email}.
    <button type="button" class="underline" onclick={() => session.signOut()}>Sign out</button>
  </p>
</main>
