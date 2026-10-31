<script lang="ts">
  import { flowHref, type FlowLink } from '../lib/flows';
  import { onMount, untrack } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { endOfDay } from '../lib/assets';
  import type { Condition, OfferedAction, ReportResult, ResolvedIcon } from '../lib/models';
  import { scanActionLabel } from '../lib/scan';
  import { session } from '../lib/session.svelte';
  import AddToSpool from '../components/AddToSpool.svelte';
  import ActionButtons from '../components/ActionButtons.svelte';
  import AssetIcon from '../components/AssetIcon.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';
  import ReportProblemForm from '../components/ReportProblemForm.svelte';
  import ScanResult from '../components/ScanResult.svelte';
  import SignInToSee from '../components/SignInToSee.svelte';

  // /scan/:code: what scanning an asset's code shows (README §6.4), with the
  // quick actions its template offers that the viewer may take now. Signed
  // out, it shows what the public role may see.
  let { params }: { params: Record<string, string> } = $props();
  const code = untrack(() => params.code ?? '');

  interface Scan {
    asset: { id: string; name: string; counted: boolean; unit?: string; actions?: OfferedAction[]; icon?: ResolvedIcon; flows?: FlowLink[] };
    template: { id: string; name: string };
    result: ReportResult;
  }

  let scan = $state<Scan | null>(null);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);
  let amount = $state<number | null>(1);
  let reporting = $state(false);
  let due = $state('');
  const today = new Date().toISOString().slice(0, 10);

  const path = $derived(scan ? `/assets/${encodeURIComponent(scan.asset.id)}` : '');

  async function load() {
    try {
      scan = await request<Scan>('GET', `/scan/${encodeURIComponent(code)}`);
      document.title = `${scan.asset.name} · BAM!`;
    } catch (err) {
      scan = null;
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  // Takes an action, says it was done, and shows the asset as it is now.
  async function act(action: () => Promise<unknown>, done: string) {
    error = status = '';
    busy = true;
    try {
      await action();
      status = done;
      await load();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function use(event: SubmitEvent) {
    event.preventDefault();
    if (!amount || amount < 1) return;
    const n = amount;
    void act(() => request('POST', `${path}/quantity`, { adjust: -n }), `Used ${n}${scan?.asset.unit ? ` ${scan.asset.unit}` : ''}.`);
  }

  // The form stays open, with what was typed, if the report fails.
  async function report(condition: Condition, note: string) {
    await request('PUT', `${path}/condition`, { condition, note });
    reporting = false;
    status = 'Thank you; the problem is reported.';
    error = '';
    await load();
  }

  function borrow() {
    const body = due ? { due_at: endOfDay(due) } : undefined;
    void act(() => request('POST', `${path}/checkout`, body), due ? `Borrowed, due back ${new Date(endOfDay(due)).toLocaleDateString()}.` : 'Borrowed.');
    due = '';
  }
</script>

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Looking it up…</p>
{:else if scan}
  <h1 class="mb-4 flex items-center gap-3 text-3xl font-bold"><AssetIcon icon={scan.asset.icon} height={44} />{scan.asset.name}</h1>
  {#if status}<p class="mb-3 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
  <ErrorMessage {error} />
  <ScanResult result={scan.result} linkRows>
    {#snippet actions(list)}
      {#if list.length}
        <div class="flex flex-wrap items-end gap-2">
          {#each list as action (action)}
            {#if action === 'open'}
              <a class="btn-secondary" href={path}>{scanActionLabel(action)}</a>
            {:else if action === 'edit'}
              <a class="btn-secondary" href="{path}/edit">{scanActionLabel(action)}</a>
            {:else if action === 'use'}
              <form class="flex items-end gap-2" onsubmit={use}>
                <div>
                  <label class="label" for="scan-use">How many{scan!.asset.unit ? ` (${scan!.asset.unit})` : ''}</label>
                  <input id="scan-use" class="input w-24" type="number" min="1" step="1" required bind:value={amount} />
                </div>
                <button type="submit" class="btn-primary" disabled={busy}>Use</button>
              </form>
            {:else if action === 'borrow'}
              <div class="flex items-end gap-2">
                <div>
                  <label class="label" for="scan-due">Due back (optional)</label>
                  <input id="scan-due" class="input" type="date" min={today} bind:value={due} />
                </div>
                <button type="button" class="btn-primary" disabled={busy} onclick={borrow}>Borrow</button>
              </div>
            {:else if action === 'return'}
              <button type="button" class="btn-primary" disabled={busy} onclick={() => act(() => request('POST', `${path}/checkin`), 'Returned.')}>Return</button>
            {:else if action === 'actions' && scan!.asset.actions?.length}
              <ActionButtons assetId={scan!.asset.id} actions={scan!.asset.actions} />
            {:else if action === 'add_similar'}
              <a class="btn-secondary" href="/assets/new?like={encodeURIComponent(scan!.asset.id)}">{scanActionLabel(action)}</a>
            {:else if action === 'flows'}
              {#each scan!.asset.flows ?? [] as f (f.slug)}
                <a class="btn-secondary" href={flowHref(f.slug, scan!.asset.id)}>{f.name}</a>
              {/each}
            {:else if action === 'add_inside'}
              <a class="btn-secondary" href="/assets/new?in={encodeURIComponent(scan!.asset.id)}">{scanActionLabel(action)}</a>
            {:else if action === 'report'}
              <button type="button" class="btn-secondary" aria-expanded={reporting} onclick={() => (reporting = !reporting)}>Report a problem</button>
            {/if}
          {/each}
        </div>
        {#if reporting}
          <div class="mt-3"><ReportProblemForm onreport={report} oncancel={() => (reporting = false)} /></div>
        {/if}
      {/if}
    {/snippet}
  </ScanResult>
  {#if session.can('labels.print')}
    {@const assetId = scan.asset.id}
    <p class="mt-4"><AddToSpool what={() => ({ asset_ids: [assetId] })} label="Add its label to the print queue" /></p>
  {/if}
{:else if !session.user}
  <SignInToSee />
{:else}
  <h1 class="mb-4 text-2xl font-bold">Not found</h1>
  <ErrorMessage {error} />
  <p class="mt-3 text-sm"><a class="underline" href="/scan">Look up another code</a></p>
{/if}
