<!--
  Loan reminders by email (docs/stock-and-lending.md#loan-reminders):
  whether they go, how many days before a loan is due, and how many times
  once it is overdue. They need email set up, with the server's address.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  interface Settings {
    on: boolean;
    days_before: number;
    overdue_times: number;
    can_email: boolean;
  }

  let on = $state(false);
  let daysBefore = $state(1);
  let overdueTimes = $state(4);
  let canEmail = $state(true);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);
  // Nothing can be changed until the settings are read, so a change is not
  // overwritten as they arrive.
  let loaded = $state(false);

  function show(s: Settings) {
    [on, daysBefore, overdueTimes, canEmail] = [s.on, s.days_before, s.overdue_times, s.can_email];
    loaded = true;
  }

  onMount(async () => {
    try {
      show(await request<Settings>('GET', '/settings/loan-reminders'));
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    busy = true;
    try {
      show(await request<Settings>('PUT', '/settings/loan-reminders', { on, days_before: Number(daysBefore), overdue_times: Number(overdueTimes) }));
      status = 'Saved.';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="card mb-6 max-w-3xl space-y-3" aria-labelledby="reminders-heading">
  <h2 id="reminders-heading" class="text-lg font-semibold">Loan reminders</h2>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    Email borrowers before what they borrowed is due, the day it becomes overdue, and weekly after. Each can turn them off under Your account.
  </p>
  {#if !canEmail}
    <p class="text-sm text-amber-700 dark:text-amber-400">Email is not set up, or the server's address is not, so reminders cannot be sent.</p>
  {/if}
  <ErrorMessage {error} />
  <form class="space-y-3" onsubmit={save}>
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" bind:checked={on} disabled={!loaded || (!canEmail && !on)} /> Send loan reminders
    </label>
    <div class="flex flex-wrap items-end gap-3">
      <div>
        <label class="label" for="reminder-days">Days before it is due</label>
        <input id="reminder-days" class="input w-24" type="number" min="0" max="30" required disabled={!loaded} bind:value={daysBefore} />
      </div>
      <div>
        <label class="label" for="reminder-overdue">Reminders once overdue</label>
        <input id="reminder-overdue" class="input w-24" type="number" min="0" max="52" required disabled={!loaded} bind:value={overdueTimes} />
      </div>
      <button type="submit" class="btn-secondary" disabled={busy || !loaded}>Save</button>
      {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
    </div>
  </form>
</section>
