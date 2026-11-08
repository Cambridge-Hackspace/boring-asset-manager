<!--
  How purchase orders' numbers are shown (docs/purchase-orders.md#numbers):
  a prefix, and at least how many digits. Only how they are shown changes;
  assets received already keep the order number they were given.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { OrderNumbering } from '../../lib/models';
  import { numberingExample, numberingProblem } from '../../lib/orders';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let prefix = $state('PO-');
  let padding = $state(4);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);
  const problem = $derived(numberingProblem(prefix, Number(padding)));

  onMount(async () => {
    try {
      const n = await request<OrderNumbering>('GET', '/settings/purchase-orders');
      [prefix, padding] = [n.prefix, n.padding];
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    if (problem) {
      error = problem;
      return;
    }
    busy = true;
    try {
      await request('PUT', '/settings/purchase-orders', { prefix: prefix.trim(), padding: Number(padding) });
      status = 'Saved.';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="card mb-6 max-w-3xl space-y-3" aria-labelledby="order-numbers-heading">
  <h2 id="order-numbers-heading" class="text-lg font-semibold">Purchase order numbers</h2>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    How orders’ numbers are shown, on their pages and PDFs, and given to the assets they bring. Changing it changes how every order is shown; assets received already keep the number they were given.
  </p>
  <ErrorMessage {error} />
  <form class="flex flex-wrap items-end gap-3" onsubmit={save}>
    <div>
      <label class="label" for="order-prefix">Prefix</label>
      <input id="order-prefix" class="input font-mono" maxlength="16" aria-describedby="order-prefix-help" bind:value={prefix} />
    </div>
    <div class="w-32">
      <label class="label" for="order-padding">Digits, at least</label>
      <input id="order-padding" class="input" type="number" min="0" max="9" bind:value={padding} />
    </div>
    <button type="submit" class="btn-secondary" disabled={busy}>Save</button>
    {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
  </form>
  <p id="order-prefix-help" class="text-xs text-zinc-600 dark:text-zinc-400">
    Letters, digits, hyphens and underscores, or none. The twelfth order is <span class="font-mono">{numberingExample(prefix.trim(), Number(padding))}</span>.
  </p>
</section>
