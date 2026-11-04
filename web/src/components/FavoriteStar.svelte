<script lang="ts">
  // A star that keeps something among the signed-in person's favorites
  // (#225, docs/favorites.md): pressed when it is one.
  import { onMount } from 'svelte';
  import { messageOf } from '../lib/errors';
  import { favorites } from '../lib/favorites.svelte';
  import type { FavoriteKind } from '../lib/models';
  import { session } from '../lib/session.svelte';

  let { kind, id, name }: { kind: FavoriteKind; id: string; name: string } = $props();

  const starred = $derived(favorites.has(kind, id));
  let busy = $state(false);
  let error = $state('');

  onMount(() => {
    favorites.load().catch(() => {});
  });

  async function toggle() {
    busy = true;
    error = '';
    try {
      await favorites.set(kind, id, !starred);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

{#if session.user}
  <button
    type="button"
    class="inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-md {starred ? 'text-amber-600 dark:text-amber-400' : 'text-zinc-500 dark:text-zinc-400'} hover:bg-zinc-100 dark:hover:bg-zinc-800 print:hidden"
    aria-pressed={starred}
    aria-label="Favorite {name}"
    title={starred ? 'A favorite: press to unstar' : 'Star as a favorite'}
    disabled={busy}
    onclick={toggle}
  >
    <svg viewBox="0 0 24 24" class="h-6 w-6" aria-hidden="true" fill={starred ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.8" stroke-linejoin="round">
      <path d="M12 3.5l2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.9l-5.2 2.7 1-5.8-4.3-4.1 5.9-.9z" />
    </svg>
  </button>
  {#if error}<span class="text-xs text-red-700 dark:text-red-400" role="alert">{error}</span>{/if}
{/if}
