<script lang="ts">
  import { messageOf } from '../lib/errors';
  import { addedMessage, addToSpool, type SpoolAddRequest } from '../lib/spool';

  // Adds labels to the print queue (README §6.2), and says what that did. Only
  // for those with labels.print; the caller checks.
  let { what, label = 'Add to the print queue' }: { what: () => SpoolAddRequest; label?: string } = $props();

  let busy = $state(false);
  let status = $state('');
  let error = $state('');

  async function add() {
    busy = true;
    status = error = '';
    try {
      status = addedMessage(await addToSpool(what()));
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<span class="inline-flex flex-wrap items-center gap-2">
  <button type="button" class="btn-secondary" disabled={busy} onclick={add}>{label}</button>
  <span class="text-sm" aria-live="polite">
    {#if status}<span class="text-green-700 dark:text-green-400">{status}</span> <a class="underline" href="/spool">Open the print queue</a>{/if}
    {#if error}<span class="text-red-700 dark:text-red-400">{error}</span>{/if}
  </span>
</span>
