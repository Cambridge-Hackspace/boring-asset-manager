<script lang="ts">
  import { permissionLabel } from '../lib/permissions';
  import type { Route } from '../routes';

  // Shown when the user lacks every permission a route needs: which page,
  // and what it needs.
  let { route }: { route?: Route } = $props();

  const needs = $derived((route?.permissions ?? []).map(permissionLabel));
</script>

<h1 class="text-2xl font-bold">Not allowed</h1>
<p class="mt-2">
  {#if route && needs.length}
    {route.title} needs {needs.length === 1 ? '' : 'one of '}<strong>{needs.join(' or ')}</strong>, which your account does not have. Ask whoever manages users here.
  {:else}
    Your account does not have permission to open this page.
  {/if}
  <a class="underline" href="/">Go home</a>.
</p>
