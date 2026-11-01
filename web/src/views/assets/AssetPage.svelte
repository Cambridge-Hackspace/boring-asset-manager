<script lang="ts">
  import { flowHref, type FlowLink } from '../../lib/flows';
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { amount, conditionLabel, contentsOf, describeChange, describeEvent, isWebURL, keptValues, placeLink } from '../../lib/assets';
  import { messageOf } from '../../lib/errors';
  import { actorName } from '../../lib/people';
  import type { AssetEvent, AssetWithDetails, Condition, Field, GeneratorInfo, GeneratorKey, RelationType } from '../../lib/models';
  import { DEFAULT_SCOPE } from '../../lib/permissions';
  import { navigate } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import AssetIcon from '../../components/AssetIcon.svelte';
  import { paramsNote, plainIcon, stateNote, textsNote } from '../../lib/iconStates';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import ConditionBadge from '../../components/ConditionBadge.svelte';
  import ConflictMark from '../../components/ConflictMark.svelte';
  import LabelActions from '../../components/LabelActions.svelte';
  import ReportProblemForm from '../../components/ReportProblemForm.svelte';
  import SignInToSee from '../../components/SignInToSee.svelte';
  import HookButtons from '../../components/HookButtons.svelte';
  import LinksPanel from '../../components/LinksPanel.svelte';
  import LocationPicker from '../../components/LocationPicker.svelte';
  import PhotosPanel from '../../components/PhotosPanel.svelte';
  import LoanPanel from '../../components/LoanPanel.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const id = untrack(() => params.id ?? '');

  let asset = $state<AssetWithDetails | null>(null);
  let generators = $state<GeneratorInfo[]>([]);
  let history = $state<AssetEvent[]>([]);
  let olderHistory = $state(false);
  let error = $state('');
  let saved = $state('');
  let loading = $state(true);
  let busy = $state(false);
  let howMany = $state<number | null>(1);
  // Where it is: the link types that make locations, and moving it.
  let relationTypes = $state<RelationType[]>([]);
  // The flows that start from it, such as Collect it (README §6.7).
  let offeredFlows = $state<FlowLink[]>([]);
  let moving = $state(false);
  // Every field, to name values kept from former categories by their labels.
  let allFields = $state<Field[]>([]);

  const path = `/assets/${encodeURIComponent(id)}`;

  onMount(async () => {
    try {
      [asset, generators] = await Promise.all([
        request<{ asset: AssetWithDetails }>('GET', path).then((r) => r.asset),
        request<{ generators: GeneratorInfo[] }>('GET', '/generators').then((r) => r.generators ?? []),
        loadHistory(),
      ]);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
    if (asset && keptValues(asset.field_values, asset.effective_fields).length) {
      // Without them, kept values are named by their keys.
      allFields = await request<{ fields: Field[] }>('GET', '/fields').then((r) => r.fields ?? [], () => []);
    }
    // Signed out, the catalog is closed (README §3.3): where it is shows,
    // but not what it holds.
    if (!session.user) return;
    try {
      relationTypes = (await request<{ relation_types: RelationType[] }>('GET', '/relation-types')).relation_types ?? [];
    } catch {
      // Without them, where it is shows, but not what it holds.
    }
    if (session.can('flows.run')) {
      offeredFlows = await request<{ flows: FlowLink[] }>('GET', `${path}/flows`).then((r) => r.flows ?? [], () => []);
    }
  });

  const contents = $derived(asset ? contentsOf(asset, relationTypes) : []);
  const kept = $derived(asset ? keptValues(asset.field_values, asset.effective_fields, allFields) : []);
  const locationType = $derived(relationTypes.find((t) => t.is_location));

  // Moves it into a place, or out of the one it is in.
  async function move(place: { id: string; name: string } | null) {
    if (!asset) return;
    error = '';
    try {
      let r: { asset: AssetWithDetails };
      if (place && locationType) {
        r = await request('POST', `${path}/links`, { type_id: locationType.id, asset_id: place.id, outgoing: false });
      } else {
        const link = placeLink(asset, relationTypes);
        if (!link) return;
        r = await request('DELETE', `${path}/links/${encodeURIComponent(link.id)}`);
      }
      moving = false;
      assetChanged(r.asset, place ? `Moved to ${place.name}.` : 'No longer in anything.');
    } catch (err) {
      error = messageOf(err);
    }
  }

  // Loads the newest page of history, or with before, the page older than it.
  async function loadHistory(before?: string) {
    const query = before ? `?before=${encodeURIComponent(before)}` : '';
    const page = await request<{ events: AssetEvent[]; more: boolean }>('GET', `${path}/history${query}`);
    history = before ? [...history, ...(page.events ?? [])] : (page.events ?? []);
    olderHistory = page.more;
  }

  async function showOlder() {
    error = '';
    try {
      await loadHistory(history[history.length - 1]?.id);
    } catch (err) {
      error = messageOf(err);
    }
  }

  const archived = $derived(!!asset?.asset.archived_at);

  const generatorName = (type: string) => generators.find((g) => g.type === type)?.name ?? type;

  // Runs a stock action, then shows the asset and history as they are now.
  async function stockAction(action: () => Promise<{ asset: AssetWithDetails }>, done: string) {
    error = '';
    saved = '';
    busy = true;
    try {
      asset = (await action()).asset;
      saved = done;
      await loadHistory();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function adjust(sign: 1 | -1) {
    const n = howMany ?? 0;
    if (!Number.isInteger(n) || n < 1) {
      error = 'Enter a whole number of at least 1.';
      return;
    }
    const unit = asset?.asset.stock?.unit;
    void stockAction(
      () => request('POST', `${path}/quantity`, { adjust: sign * n }),
      sign < 0 ? `Used ${amount(n, unit)}.` : `Added ${amount(n, unit)}.`,
    );
  }

  function setRunningLow(low: boolean) {
    void stockAction(
      () => request('PUT', `${path}/running-low`, { running_low: low }),
      low ? 'Marked as running low.' : 'Marked as restocked.',
    );
  }

  const when = (iso: string) => new Date(iso).toLocaleString();

  // Reporting a problem, or clearing one.
  let reporting = $state(false);

  async function report(condition: Condition, note: string) {
    const r = await request<{ asset: AssetWithDetails }>('PUT', `${path}/condition`, { condition, note });
    asset = r.asset;
    reporting = false;
    saved = `Reported: ${conditionLabel(condition).toLowerCase()}.`;
    await loadHistory();
  }

  // After a loan or a link changes: show the asset as it is now.
  function assetChanged(a: AssetWithDetails, done: string) {
    asset = a;
    saved = done;
    error = '';
    void loadHistory();
  }

  function markWorking() {
    void stockAction(() => request('PUT', `${path}/condition`, { condition: 'ok' }), 'Marked as working.');
  }

  async function restore() {
    error = '';
    saved = '';
    busy = true;
    try {
      const r = await request<{ asset: AssetWithDetails; skipped_codes: GeneratorKey[] }>('POST', `${path}/restore`);
      asset = r.asset;
      const skipped = (r.skipped_codes ?? []).map((k) => `${generatorName(k.generator_type)} ${k.assigned_code}`);
      saved = skipped.length ? `Restored. Other assets have taken ${skipped.join(', ')}, so it did not get them back.` : 'Restored.';
      await loadHistory();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function archive() {
    try {
      await request('DELETE', `/assets/${encodeURIComponent(id)}`);
      navigate('/assets');
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm"><a class="underline" href="/assets">Assets</a></nav>
{#if asset || session.user}<ErrorMessage {error} />{/if}
{#if saved}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{saved}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if asset}
  <div class="mb-4 flex flex-wrap items-end justify-between gap-4">
    <div>
      <h1 class="flex items-center gap-3 text-3xl font-bold"><AssetIcon icon={asset.resolved_icon} height={44} />{asset.asset.name}</h1>
      {#if asset.generator_keys.length}
        <p class="mt-2 flex flex-wrap gap-2" aria-label="Its codes">
          {#each asset.generator_keys as k, i (k.generator_type)}<span class={i === 0 ? 'chip-yellow' : 'chip'} title={generatorName(k.generator_type)}>{k.assigned_code}</span>{/each}
        </p>
      {/if}
    </div>
    <div class="flex flex-wrap gap-2">
      {#if archived}
        {#if session.canOn(asset.scopes, 'assets.delete')}
          <button type="button" class="btn-primary" disabled={busy} onclick={restore}>Restore</button>
        {/if}
      {:else}
        {#if asset.holds}<a class="btn-secondary" href="/assets/{encodeURIComponent(id)}/scene">See inside</a>{/if}
        {#if session.canOn(asset.scopes, 'assets.write')}
          <a class="btn-secondary" href="/assets/{encodeURIComponent(id)}/edit">Edit</a>
        {/if}
        {#if session.canAnyOn(asset.scopes, 'assets.report', 'assets.write') && !reporting}
          <button type="button" class="btn-secondary" onclick={() => (reporting = true)}>{asset.asset.condition === 'ok' ? 'Report a problem' : 'Update the problem'}</button>
        {/if}
        {#if asset.asset.condition !== 'ok' && session.canOn(asset.scopes, 'assets.write')}
          <button type="button" class="btn-secondary" disabled={busy} onclick={markWorking}>Mark as working</button>
        {/if}
      {/if}
    </div>
  </div>
  {#if asset.location.length}
    <nav aria-label="Location" class="-mt-2 mb-4 text-sm text-zinc-600 dark:text-zinc-400">
      In
      {#each asset.location as place, i (place.id)}{#if i > 0}{' › '}{/if}<a class="underline" href="/assets/{encodeURIComponent(place.id)}">{place.name}</a>{/each}
    </nav>
  {/if}
  {#if asset.asset.archived_at}
    <p class="mb-4 rounded border border-zinc-300 bg-zinc-100 p-3 text-sm dark:border-zinc-700 dark:bg-zinc-800">
      Archived {when(asset.asset.archived_at)}. Its codes were released for other assets; restoring it gives back those no other asset has taken.
    </p>
  {/if}
  {#if asset.asset.condition !== 'ok'}
    <p class="mb-4 flex flex-wrap items-center gap-2 text-sm">
      <ConditionBadge condition={asset.asset.condition} />
      {#if asset.asset.condition_note}<span>{asset.asset.condition_note}</span>{/if}
    </p>
  {/if}
  {#if reporting}
    <div class="mb-4">
      <ReportProblemForm condition={asset.asset.condition} note={asset.asset.condition_note ?? ''} onreport={report} oncancel={() => (reporting = false)} />
    </div>
  {/if}

  <div class="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
    <!-- The main column is plain sections; the side column, cards. -->
    <div class="space-y-10">
      <PhotosPanel {asset} onchange={assetChanged} />
      <section aria-labelledby="details-heading">
        <h2 id="details-heading" class="section-title mb-3">Details</h2>
        <dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-3 text-sm">
          <dt class="text-zinc-600 dark:text-zinc-400">Categories</dt>
          <dd class="flex flex-wrap gap-1">
            {#each asset.categories as c (c.id)}<CategoryBadge category={c} href="/categories/{encodeURIComponent(c.id)}" />{/each}
          </dd>
          {#if asset.resolved_icon}
            {@const icon = asset.resolved_icon}
            <dt class="text-zinc-600 dark:text-zinc-400">Icon</dt>
            <dd>
              {#if icon.from === 'asset'}
                Its own, <span class="font-mono">{plainIcon(icon)}</span>{#if icon.missing}: no icon is drawn by that name any more, so it shows as unknown{/if}.
              {:else}
                From <a class="underline" href="/categories/{encodeURIComponent(icon.source_category_id ?? '')}?tab=icon">{icon.source_category_name}</a>.
              {/if}
              {#if paramsNote(icon)}<span data-icon-params>{paramsNote(icon)}</span>{/if}
              {#if textsNote(icon)}<span data-icon-texts>{textsNote(icon)}</span>{/if}
              {#if stateNote(icon)}<span data-icon-state>{stateNote(icon)}</span>{/if}
            </dd>
          {/if}
          {#if asset.scopes && !(asset.scopes.length === 1 && asset.scopes[0] === DEFAULT_SCOPE)}
            <dt class="text-zinc-600 dark:text-zinc-400">Scopes</dt>
            <dd>{asset.scopes.join(', ')}</dd>
          {/if}
          {#if session.canOn(asset.scopes, 'assets.scan')}
            <dt class="text-zinc-600 dark:text-zinc-400">Scan page</dt>
            <dd>
              <a class="inline-flex min-h-11 items-center underline" href="/scan/{encodeURIComponent(asset.asset.id)}">What scanning it shows</a>
              {#if asset.scan_template?.conflict}
                <span class="block"><ConflictMark /> Its template was chosen by a tie between categories as close to it as each other.
                <a class="underline" href="/categories">Name a template on one of them</a> to settle it.</span>
              {/if}
            </dd>
          {/if}
          {#if asset.label_template?.conflict && session.can('labels.print')}
            <dt class="text-zinc-600 dark:text-zinc-400">Label template</dt>
            <dd>
              <ConflictMark label="Label template chosen by a tie between categories" /> Chosen by a tie between categories as close to it as
              each other. <a class="underline" href="/categories">Name a template on one of them</a> to settle it.
            </dd>
          {/if}
          {#each asset.effective_fields as e (e.field.id)}
            <dt class="text-zinc-600 dark:text-zinc-400">{e.field.label}</dt>
            <dd>
              {#if asset.field_values[e.field.key]}
                {@const value = asset.field_values[e.field.key]!}
                {#if e.field.validation_type === 'url' && isWebURL(value)}
                  <a class="underline" href={value} rel="noopener noreferrer" target="_blank">{value}</a>
                {:else if e.field.validation_type === 'select'}
                  {e.field.options?.choices.find((c) => c.value === value)?.label ?? value}
                {:else}
                  {value}
                {/if}
              {:else}
                <span class="text-zinc-600 dark:text-zinc-400">{e.is_required ? 'Missing' : '—'}</span>
              {/if}
            </dd>
          {/each}
          {#each asset.generator_keys as k (k.generator_type)}
            <dt class="text-zinc-600 dark:text-zinc-400">{generatorName(k.generator_type)}</dt>
            <dd class="font-mono">{k.assigned_code}</dd>
          {/each}
        </dl>
        {#if kept.length}
          <details class="mt-4 text-sm">
            <summary class="cursor-pointer">Values kept from former categories</summary>
            <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
              {#each kept as k (k.key)}
                <dt class={k.label ? '' : 'font-mono'}>{k.label ?? k.key}</dt>
                <dd>{k.value}</dd>
              {/each}
            </dl>
          </details>
        {/if}
      </section>
      <LinksPanel {asset} onchange={assetChanged} />
      {#if contents.length}
        <section aria-labelledby="contents-heading">
          <h2 id="contents-heading" class="section-title mb-3">Contents</h2>
          <ul class="space-y-1 text-sm">
            {#each contents as l (l.id)}
              <li><a class="underline" href="/assets/{encodeURIComponent(l.asset.id)}">{l.asset.name}</a>{#if l.asset.archived}<span class="text-xs text-zinc-600 dark:text-zinc-400"> (archived)</span>{/if}</li>
            {/each}
          </ul>
        </section>
      {/if}
      {#if history.length}
        <section aria-labelledby="history-heading">
          <h2 id="history-heading" class="section-title mb-3">History</h2>
          <ul class="space-y-2 text-sm">
            {#each history as e (e.id)}
              <li>
                <span class="font-medium">{describeEvent(e)}</span>
                {#if e.payload.changes?.length}
                  <ul class="list-disc pl-5">
                    {#each e.payload.changes as c, i (i)}<li>{describeChange(c, e.kind)}</li>{/each}
                  </ul>
                {/if}
                <span class="block text-xs text-zinc-600 dark:text-zinc-400">
                  {when(e.created_at)}{e.payload.actor ? ` · ${actorName(e.payload.actor, session.user?.id)}` : ''}
                </span>
              </li>
            {/each}
          </ul>
          {#if olderHistory}
            <button type="button" class="btn-secondary mt-3" onclick={showOlder}>Show older</button>
          {/if}
        </section>
      {/if}
    </div>
    <aside class="space-y-6" aria-label="About it">
      <section class="card" aria-labelledby="where-heading">
        <h2 id="where-heading" class="mb-2 text-lg font-semibold">Where it is</h2>
        <p class="flex flex-wrap items-center gap-x-1 text-sm">
          {#if asset.location.length}
            {#each asset.location as place, i (place.id)}{#if i > 0}<span aria-hidden="true">›</span>{/if}<a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(place.id)}">{place.name}</a>{/each}
          {:else}
            <span class="text-zinc-600 dark:text-zinc-400">Not set</span>
          {/if}
        </p>
        <div class="mt-3 flex flex-wrap gap-2">
          {#if asset.location.length}<a class="btn-secondary" href="/assets/{encodeURIComponent(asset.location.at(-1)!.id)}/scene?show={encodeURIComponent(id)}">Show in place</a>{/if}
          {#if !archived && locationType && session.canOn(asset.scopes, 'assets.write')}
            <button type="button" class="btn-secondary" aria-expanded={moving} onclick={() => (moving = !moving)}>{asset.location.length ? 'Move' : 'Set where it is'}</button>
          {/if}
        </div>
        {#if moving}
          <div class="mt-3">
            <LocationPicker id="move" value={asset.location.at(-1) ?? null} exclude={asset.asset.id} onchange={move} />
          </div>
        {/if}
      </section>
      {#if asset.asset.stock}
        {@const stock = asset.asset.stock}
        <section class="card" aria-labelledby="stock-heading">
          <h2 id="stock-heading" class="mb-1 text-lg font-semibold">Stock</h2>
          <p class="text-sm">
            <strong>{stock.is_running_low ? 'Running low.' : 'In stock.'}</strong>
            {amount(stock.quantity, stock.unit)} left{stock.low_stock_threshold
              ? `; flagged automatically below ${amount(stock.low_stock_threshold, stock.unit)}`
              : ''}.
          </p>
          {#if session.canOn(asset.scopes, 'stock.record') && !archived}
            <div class="mt-3 flex flex-wrap items-end gap-2">
              <div>
                <label class="label" for="stock-amount">How many{stock.unit ? ` (${stock.unit})` : ''}</label>
                <input id="stock-amount" class="input w-28" type="number" min="1" step="1" bind:value={howMany} />
              </div>
              <button type="button" class="btn-secondary" disabled={busy} onclick={() => adjust(-1)}>Use</button>
              <button type="button" class="btn-secondary" disabled={busy} onclick={() => adjust(1)}>Add</button>
              {#if stock.is_running_low}
                <button type="button" class="btn-secondary" disabled={busy} onclick={() => setRunningLow(false)}>Mark restocked</button>
              {:else}
                <button type="button" class="btn-secondary" disabled={busy} onclick={() => setRunningLow(true)}>Mark running low</button>
              {/if}
            </div>
          {/if}
        </section>
      {/if}
      {#if (asset.lendable || asset.loan) && !archived}
        <LoanPanel {asset} onchange={assetChanged} />
      {/if}
      {#if !archived && session.user}<LabelActions assetIds={[id]} spool />{/if}
      {#if !archived && asset.hooks?.length && session.canOn(asset.scopes, 'assets.hook')}
        <section class="card" aria-labelledby="hooks-heading">
          <h2 id="hooks-heading" class="mb-3 text-lg font-semibold">Hooks</h2>
          <HookButtons assetId={id} hooks={asset.hooks} />
        </section>
      {/if}
      {#if !archived && offeredFlows.length}
        <section class="card" aria-labelledby="flows-heading">
          <h2 id="flows-heading" class="mb-3 text-lg font-semibold">Flows</h2>
          <div class="flex flex-wrap gap-2">
            {#each offeredFlows as f (f.slug)}<a class="btn-secondary" href={flowHref(f.slug, id)}>{f.name}</a>{/each}
          </div>
        </section>
      {/if}
      {#if !archived && (session.canOn(asset.scopes, 'assets.write') || session.canOn(asset.scopes, 'assets.delete'))}
        <section class="card" aria-labelledby="more-heading">
          <h2 id="more-heading" class="mb-3 text-lg font-semibold">More</h2>
          <div class="flex flex-wrap gap-2">
            {#if session.canOn(asset.scopes, 'assets.write')}
              <a class="btn-secondary" href="/assets/new?like={encodeURIComponent(id)}" title="A new asset like this one, to change before it is created">Duplicate…</a>
              {#if locationType}<a class="btn-secondary" href="/assets/new?in={encodeURIComponent(id)}" title="A new asset, in this one">Add something inside…</a>{/if}
            {/if}
            {#if session.canOn(asset.scopes, 'assets.delete')}
              <ConfirmButton label="Archive" question="Archive this asset? Its codes become free for reuse." onconfirm={archive} />
            {/if}
          </div>
        </section>
      {/if}
    </aside>
  </div>
{:else if !session.user}
  <SignInToSee />
{/if}
