<script lang="ts">
  import { router } from '../lib/router.svelte';
  import { signInPath } from '../lib/session.svelte';

  // For a visitor who is not signed in, when what they opened is not public
  // or does not exist: the server does not say which (docs/access.md).
  let { what = 'this asset' }: { what?: string } = $props();
</script>

<h1 class="mb-2 text-2xl font-bold">Sign in to see {what}</h1>
<p class="mb-4 text-sm text-zinc-700 dark:text-zinc-300">It is not shown to visitors who are not signed in, or the code or address is wrong.</p>
<a class="btn-primary" href={signInPath(router.path + router.search)}>Sign in</a>
