<script lang="ts">
  import { untrack } from 'svelte';
  import { messageOf } from '../lib/errors';
  import type { Condition } from '../lib/models';
  import ErrorMessage from './ErrorMessage.svelte';

  // Reporting a problem with an asset, on its page and its scan page alike:
  // how bad it is, and what is wrong. What was typed is kept until the
  // report is made, so a failure loses nothing.
  let {
    condition: initialCondition = 'needs_maintenance',
    note: initialNote = '',
    onreport,
    oncancel,
  }: {
    condition?: Condition;
    note?: string;
    /** Makes the report; a rejection is shown, and the form stays open. */
    onreport: (condition: Condition, note: string) => Promise<void>;
    oncancel: () => void;
  } = $props();

  let condition = $state<Condition>(untrack(() => (initialCondition === 'out_of_service' ? 'out_of_service' : 'needs_maintenance')));
  let note = $state(untrack(() => initialNote));
  let busy = $state(false);
  let error = $state('');

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = '';
    try {
      await onreport(condition, note);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<form class="card max-w-xl space-y-3" onsubmit={submit} aria-labelledby="report-heading">
  <h2 id="report-heading" class="text-lg font-semibold">Report a problem</h2>
  <fieldset>
    <legend class="label">How bad is it?</legend>
    <label class="mr-4 inline-flex items-center gap-2 text-sm">
      <input type="radio" name="condition" value="needs_maintenance" bind:group={condition} /> Needs maintenance, still usable
    </label>
    <label class="inline-flex items-center gap-2 text-sm">
      <input type="radio" name="condition" value="out_of_service" bind:group={condition} /> Out of service, do not use
    </label>
  </fieldset>
  <div>
    <label class="label" for="condition-note">What is wrong?</label>
    <input id="condition-note" class="input" maxlength="255" placeholder="The chuck slips" bind:value={note} />
  </div>
  <ErrorMessage {error} />
  <div class="flex gap-2">
    <button type="submit" class="btn-primary" disabled={busy}>Report</button>
    <button type="button" class="btn-secondary" onclick={oncancel}>Cancel</button>
  </div>
</form>
