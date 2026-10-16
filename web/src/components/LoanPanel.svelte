<script lang="ts">
  import { request } from '../lib/api';
  import { dueDate, endOfDay } from '../lib/assets';
  import { messageOf } from '../lib/errors';
  import type { AssetWithDetails, Person } from '../lib/models';
  import { displayName } from '../lib/people';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  // Who has a lendable asset, and checking it out and back in.
  let { asset, onchange }: { asset: AssetWithDetails; onchange: (a: AssetWithDetails, done: string) => void } = $props();

  let people = $state<Person[]>([]);
  let borrowerId = $state('');
  let due = $state('');
  // For a weighed asset coming back: how much is left, if it was measured.
  let left = $state<number | null>(null);
  let busy = $state(false);
  let error = $state('');

  const path = $derived(`/assets/${encodeURIComponent(asset.asset.id)}`);
  const canLend = $derived(session.canOn(asset.scopes, 'assets.lend'));
  const canBorrow = $derived(session.canOn(asset.scopes, 'assets.borrow'));
  const loan = $derived(asset.loan);
  const mine = $derived(!!loan && loan.borrower.id === session.user?.id);
  const today = new Date().toISOString().slice(0, 10);
  // Why it cannot be lent, if it cannot.
  const blocked = $derived(
    asset.asset.stock && !asset.weighed
      ? 'It is counted by quantity, so it cannot be lent; label each item to lend it.'
      : asset.asset.condition === 'out_of_service'
        ? 'It is out of service, so it cannot be lent until it is working again.'
        : '',
  );

  $effect(() => {
    if (canLend && !loan && people.length === 0) {
      request<{ people: Person[] }>('GET', '/people')
        .then((r) => (people = r.people ?? []))
        .catch((err) => (error = messageOf(err)));
    }
  });

  async function act(action: () => Promise<{ asset: AssetWithDetails }>, done: string) {
    error = '';
    busy = true;
    try {
      onchange((await action()).asset, done);
      due = '';
      borrowerId = '';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function checkOut(to?: string) {
    const body: Record<string, string> = {};
    if (to) body.borrower_id = to;
    if (due) body.due_at = endOfDay(due);
    const who = to ? displayName(people.find((p) => p.id === to) ?? { email: 'them' }) : 'you';
    void act(() => request('POST', `${path}/checkout`, body), `Lent to ${who}.`);
  }

  function checkIn() {
    const body = asset.asset.stock && left !== null ? { left } : undefined;
    void act(() => request('POST', `${path}/checkin`, body), 'Checked in.');
    left = null;
  }
</script>

<section class="card" aria-labelledby="loan-heading">
  <h2 id="loan-heading" class="mb-1 text-lg font-semibold">Loan</h2>
  <ErrorMessage {error} />
  {#if loan}
    <p class="text-sm">
      Lent to <strong>{mine ? 'you' : displayName(loan.borrower)}</strong>{loan.borrower.archived ? ' (who has left)' : ''} since
      {dueDate(loan.checked_out_at)}{loan.due_at ? `, due back ${dueDate(loan.due_at)}` : ''}.
      {#if loan.overdue}<span class="ml-1 rounded bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-900 dark:bg-red-900 dark:text-red-100">Overdue</span>{/if}
    </p>
    {#if canLend || (canBorrow && mine)}
      <div class="mt-3 flex flex-wrap items-end gap-2">
        {#if asset.asset.stock}
          <div>
            <label class="label" for="loan-left">Left now{asset.asset.stock.unit ? ` (${asset.asset.stock.unit})` : ''}, if weighed</label>
            <input id="loan-left" class="input w-32" type="number" min="0" step="1" bind:value={left} />
          </div>
        {/if}
        <button type="button" class="btn-secondary" disabled={busy} onclick={checkIn}>Check in</button>
      </div>
    {/if}
  {:else if blocked}
    <p class="text-sm">Available to borrow, but not now. {blocked}</p>
  {:else}
    <p class="text-sm">Available to borrow.</p>
    {#if canLend || canBorrow}
      <div class="mt-3 flex flex-wrap items-end gap-2">
        <div>
          <label class="label" for="loan-due">Due back (optional)</label>
          <input id="loan-due" class="input" type="date" min={today} bind:value={due} />
        </div>
        {#if canBorrow}
          <button type="button" class="btn-secondary" disabled={busy} onclick={() => checkOut()}>Borrow it myself</button>
        {/if}
        {#if canLend}
          <div>
            <label class="label" for="loan-borrower">Lend to</label>
            <select id="loan-borrower" class="input" bind:value={borrowerId}>
              <option value="">Choose someone…</option>
              {#each people as p (p.id)}<option value={p.id}>{displayName(p)}{p.name ? ` (${p.email})` : ''}</option>{/each}
            </select>
          </div>
          <button type="button" class="btn-secondary" disabled={busy || !borrowerId} onclick={() => checkOut(borrowerId)}>Lend</button>
        {/if}
      </div>
    {/if}
  {/if}
</section>
