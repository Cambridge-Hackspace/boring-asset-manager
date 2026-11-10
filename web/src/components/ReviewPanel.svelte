<script lang="ts">
  // An asset's review (docs/review.md): its state, when it is next due,
  // who last confirmed it, and the moves the viewer may make; and changes
  // an assistant made, for the viewer to accept or reject.
  import { request, type Method } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { AssetWithDetails, AssetReview } from '../lib/models';
  import { actorName } from '../lib/people';
  import { dueText, reviewChip, reviewLabel } from '../lib/review';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  let {
    asset,
    review,
    onchange,
  }: {
    asset: AssetWithDetails;
    review: AssetReview;
    /** Shows the asset as it is after a move or a decision, with what was done. */
    onchange: (a: AssetWithDetails, done: string) => void;
  } = $props();

  const path = $derived(`/assets/${encodeURIComponent(asset.asset.id)}`);
  const archived = $derived(!!asset.asset.archived_at);
  const proposal = $derived(review.proposal);
  const mayConfirm = $derived(!archived && session.canOn(asset.scopes, 'assets.confirm'));
  const mayVerify = $derived(!archived && session.canOn(asset.scopes, 'assets.verify'));
  const mayEdit = $derived(!archived && session.canOn(asset.scopes, 'assets.write'));
  // Someone cannot verify what they confirmed themselves, unless its categories allow it.
  const ownConfirmation = $derived(!!session.user && review.confirmed_by?.id === session.user.id && !review.rules.self_verify);

  let busy = $state(false);
  let error = $state('');
  // Saying why it goes back to draft, or why changes are rejected.
  let writing = $state<'' | 'return' | 'reject'>('');
  let note = $state('');

  async function act(method: Method, url: string, body: unknown, done: string) {
    busy = true;
    error = '';
    try {
      const r = await request<{ asset: AssetWithDetails }>(method, url, body);
      writing = '';
      note = '';
      onchange(r.asset, done);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  const move = (m: 'confirm' | 'verify' | 'return', done: string) => act('POST', `${path}/review`, { move: m, note }, done);

  function submit(event: SubmitEvent) {
    event.preventDefault();
    if (writing === 'return') void move('return', 'Returned to draft.');
    else void act('POST', `${path}/proposal/reject`, { note }, proposal?.new ? 'Rejected; the asset is archived.' : 'Rejected; it is back as it was.');
  }

  const when = (iso: string) => iso.slice(0, 10);
</script>

<section class="mb-4 space-y-3" aria-label="Review">
  {#if proposal}
    <div class="notice-strong" role="region" aria-label="Assistant changes">
      <p class="text-sm">
        {#if proposal.new}Added{:else}Changed{/if} through {proposal.via || 'an assistant'}{#if proposal.by}{' '}by {actorName(proposal.by, session.user?.id)}{/if}, {when(proposal.updated_at)}.
        {#if !proposal.undoable}These changes can only be accepted.{:else if proposal.new}Rejecting archives it.{:else}Rejecting puts it back as it was.{/if}
      </p>
      {#if mayEdit}
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn-primary" disabled={busy} onclick={() => act('POST', `${path}/proposal/accept`, {}, 'Accepted.')}>Accept</button>
          {#if mayConfirm}
            <button type="button" class="btn-secondary" disabled={busy} onclick={() => act('POST', `${path}/proposal/accept`, { confirm: true }, 'Accepted and confirmed.')}>Accept and confirm</button>
          {/if}
          <a class="btn-secondary" href="{path}/edit">Edit</a>
          {#if proposal.undoable && (!proposal.new || session.canOn(asset.scopes, 'assets.delete'))}
            <button type="button" class="btn-secondary" disabled={busy} onclick={() => ((writing = 'reject'), (note = ''))}>Reject…</button>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
  <div class="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
    <span class={reviewChip(review.state)} data-review-state={review.state}>{#if review.state === 'verified'}<span aria-hidden="true">✓</span>{/if}{reviewLabel(review.state)}</span>
    {#if review.due_at}
      <span class={review.overdue ? 'font-medium text-red-700 dark:text-red-400' : review.due ? 'font-medium' : 'text-zinc-600 dark:text-zinc-400'}>{dueText(review)}</span>
    {/if}
    {#if review.confirmed_at}
      <span class="text-zinc-600 dark:text-zinc-400">Confirmed {when(review.confirmed_at)}{#if review.confirmed_by}{' '}by {actorName(review.confirmed_by, session.user?.id)}{/if}</span>
    {/if}
    {#if !proposal}
      <span class="flex flex-wrap gap-2">
        {#if mayConfirm}
          <button type="button" class="btn-secondary" disabled={busy} onclick={() => move('confirm', review.state === 'confirmed' ? 'Confirmed again.' : 'Confirmed.')}>
            {review.state === 'draft' ? 'Confirm' : 'Confirm again'}
          </button>
        {/if}
        {#if mayVerify && review.state !== 'draft'}
          <button type="button" class="btn-secondary" disabled={busy || ownConfirmation} title={ownConfirmation ? 'Someone else verifies what you confirmed' : undefined} onclick={() => move('verify', 'Verified.')}>
            {review.state === 'verified' ? 'Verify again' : 'Verify'}
          </button>
          <button type="button" class="btn-secondary" disabled={busy} onclick={() => ((writing = 'return'), (note = ''))}>Return to draft…</button>
        {/if}
      </span>
    {/if}
  </div>
  {#if ownConfirmation && mayVerify && review.state !== 'draft' && !proposal}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">You confirmed it, so someone else verifies it.</p>
  {/if}
  <ErrorMessage {error} />
  {#if writing}
    <form class="card max-w-xl space-y-3" onsubmit={submit} aria-labelledby="review-note-heading">
      <h2 id="review-note-heading" class="text-lg font-semibold">{writing === 'return' ? 'Return to draft' : 'Reject the changes'}</h2>
      <div>
        <label class="label" for="review-note">{writing === 'return' ? 'What is wrong with it?' : 'Why? (optional)'}</label>
        <input id="review-note" class="input" maxlength="1000" required={writing === 'return'} bind:value={note} />
      </div>
      <div class="flex gap-2">
        <button type="submit" class="btn-primary" disabled={busy}>{writing === 'return' ? 'Return to draft' : 'Reject'}</button>
        <button type="button" class="btn-secondary" onclick={() => (writing = '')}>Cancel</button>
      </div>
    </form>
  {/if}
</section>
