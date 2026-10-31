<script lang="ts">
  import { stack } from '../lib/stack';
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { dueDate } from '../lib/assets';
  import { messageOf } from '../lib/errors';
  import type { Loan, Person } from '../lib/models';
  import { choiceNames, personName } from '../lib/people';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  // Lenders see everyone's loans; others see their own, past ones too.
  const canLend = session.can('assets.lend');
  let loans = $state<Loan[]>([]);
  let people = $state<Person[]>([]);
  let show = $state(canLend ? 'open' : '');
  let borrowerId = $state(canLend ? '' : (session.user?.id ?? ''));
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // Shows "Loading" the first time only; reloads keep the table in place.
  async function load() {
    const q = new URLSearchParams();
    if (show) q.set('state', show);
    if (borrowerId) q.set('borrower_id', borrowerId);
    try {
      loans = (await request<{ loans: Loan[] }>('GET', `/loans?${q}`)).loans ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }

  onMount(async () => {
    if (canLend) {
      try {
        people = (await request<{ people: Person[] }>('GET', '/people')).people ?? [];
      } catch (err) {
        error = messageOf(err);
      }
    }
    await load();
  });

  async function checkIn(l: Loan) {
    error = status = '';
    try {
      await request('POST', `/assets/${encodeURIComponent(l.asset_id)}/checkin`);
      status = `Returned ${l.asset_name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<h1 class="mb-4 text-2xl font-bold">{canLend ? 'Loans' : 'Your loans'}</h1>

{#if canLend}
  <div class="mb-4 flex flex-wrap items-end gap-3">
    <div>
      <label class="label" for="loans-state">Show</label>
      <select id="loans-state" class="input" bind:value={show} onchange={load}>
        <option value="open">Lent out now</option>
        <option value="overdue">Overdue</option>
        <option value="">All loans, returned too</option>
      </select>
    </div>
    <div>
      <label class="label" for="loans-person">Borrower</label>
      <select id="loans-person" class="input" bind:value={borrowerId} onchange={load}>
        <option value="">Everyone</option>
        {#each people as p (p.id)}<option value={p.id}>{choiceNames(people).get(p.id)}</option>{/each}
      </select>
    </div>
  </div>
{/if}

<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading loans…</p>
{:else if loans.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    No loans. To borrow or lend something, open it from <a class="underline" href="/assets">Assets</a>, or scan its label, and choose
    Borrow or Lend.
  </p>
{:else}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm" use:stack>
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          <th scope="col" class="py-2 pr-4">Asset</th>
          {#if canLend}<th scope="col" class="py-2 pr-4">Borrower</th>{/if}
          <th scope="col" class="py-2 pr-4">Since</th>
          <th scope="col" class="py-2 pr-4">Due back</th>
          <th scope="col" class="py-2 pr-4">Status</th>
          <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each loans as l (l.id)}
          <tr class="border-b border-zinc-200 dark:border-zinc-800">
            <td class="py-2 pr-4 font-medium"><a class="inline-flex min-h-11 items-center hover:underline" href="/assets/{encodeURIComponent(l.asset_id)}">{l.asset_name}</a></td>
            {#if canLend}<td class="py-2 pr-4">{personName(l.borrower)}{l.borrower.archived ? ' (has left)' : ''}</td>{/if}
            <td class="py-2 pr-4 whitespace-nowrap">{dueDate(l.checked_out_at)}</td>
            <td class="py-2 pr-4 whitespace-nowrap">{l.due_at ? dueDate(l.due_at) : '—'}</td>
            <td class="py-2 pr-4 whitespace-nowrap">
              {#if l.checked_in_at}Returned {dueDate(l.checked_in_at)}{:else if l.overdue}<strong class="text-red-700 dark:text-red-400">Overdue</strong>{:else}Out{/if}
            </td>
            <td class="py-2 text-right">
              {#if !l.checked_in_at}
                <button type="button" class="btn-secondary" aria-label="Return {l.asset_name}" onclick={() => checkIn(l)}>Return</button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
