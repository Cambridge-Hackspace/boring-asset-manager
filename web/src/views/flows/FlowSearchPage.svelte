<script lang="ts">
  // A flow that only searches (docs/flows.md#search-search, #397): a search
  // box over the flow's categories, with the results the asset list shows,
  // each opened here, on its scan page's layout, rather than elsewhere. On a
  // kiosk it clears itself after the time it is set to, for the next person.
  import { onMount, tick, untrack } from 'svelte';
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { FlowSearchView } from '../../lib/flows';
  import type { AssetWithDetails, ReportResult } from '../../lib/models';
  import { queryProblemOf, type QueryProblem as Problem } from '../../lib/query';
  import { navigate, router } from '../../lib/router.svelte';
  import { searchDirect } from '../../lib/search';
  import { session, signInPath } from '../../lib/session.svelte';
  import AssetResults from '../../components/AssetResults.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import QueryProblem from '../../components/QueryProblem.svelte';
  import ScanResult from '../../components/ScanResult.svelte';
  import SearchSuggestions from '../../components/SearchSuggestions.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const slug = untrack(() => params.slug ?? '');
  const base = `/f/${encodeURIComponent(slug)}`;

  let view = $state<FlowSearchView | null>(null);
  let assets = $state<AssetWithDetails[]>([]);
  let matched = $state<Record<string, string>>({});
  let suggestions = $state<string[]>([]);
  // What is typed, and the search the results are for.
  let text = $state(untrack(() => router.query.get('search') ?? ''));
  let shown = $state<string | null>(null);
  let loading = $state(false);
  let error = $state('');
  let signIn = $state(false);
  let problem = $state<Problem | null>(null);
  let problemQuery = $state('');
  // The result opened, laid out by its scan template where it may be scanned.
  let opened = $state<{ id: string; result: ReportResult | null } | null>(null);
  let loads = 0;
  // A search just made goes straight to its one result, where the person
  // (or, signed out, the instance) goes straight there; one come back to
  // from history doesn't.
  let straight = false;
  let field: HTMLInputElement | undefined = $state();
  let heading: HTMLHeadingElement | undefined = $state();

  /** The address of a search here, and of one result opened. */
  function hrefOf(search: string, open = ''): string {
    const q = new URLSearchParams();
    if (search) q.set('search', search);
    if (open) q.set('open', open);
    const s = q.toString();
    return base + (s ? `?${s}` : '');
  }

  // The address says what shows: a search, and a result opened. Going
  // back and forth in history moves between them, within the flow.
  $effect(() => {
    const search = router.query.get('search') ?? '';
    const open = router.query.get('open') ?? '';
    untrack(() => void follow(search, open));
  });

  async function follow(search: string, open: string) {
    const going = straight;
    straight = false;
    if (search !== shown || !view) {
      if (!(await load(search))) return;
      if (going && search && !open && assets.length === 1 && (await searchDirect())) {
        navigate(hrefOf(search, assets[0]!.asset.id), true);
        return;
      }
    }
    if (open) await openOne(open);
    else opened = null;
  }

  async function load(search: string): Promise<boolean> {
    const mine = ++loads;
    loading = true;
    error = '';
    problem = null;
    try {
      const res = await request<{ flow: FlowSearchView; assets: AssetWithDetails[]; matched?: Record<string, string>; suggestions?: string[] }>(
        'GET',
        `/flows/${encodeURIComponent(slug)}/search${search ? `?search=${encodeURIComponent(search)}` : ''}`,
      );
      if (mine !== loads) return false;
      view = res.flow;
      assets = res.assets ?? [];
      matched = res.matched ?? {};
      suggestions = res.suggestions ?? [];
      shown = search;
      text = search;
      return true;
    } catch (err) {
      if (mine !== loads) return false;
      assets = [];
      suggestions = [];
      shown = search;
      if (err instanceof ApiError && err.status === 401) {
        signIn = true;
        return false;
      }
      problem = err instanceof ApiError ? queryProblemOf(err.body) : null;
      if (problem) problemQuery = search;
      else error = messageOf(err);
      return false;
    } finally {
      if (mine === loads) loading = false;
    }
  }

  async function openOne(id: string) {
    opened = { id, result: null };
    await tick();
    heading?.focus();
    try {
      const scan = await request<{ result: ReportResult }>('GET', `/scan/${encodeURIComponent(id)}`);
      if (opened?.id === id) opened = { id, result: scan.result };
    } catch {
      // Without a scan page to see, it shows as its result does.
    }
  }

  function submit(event: SubmitEvent) {
    event.preventDefault();
    straight = true;
    navigate(hrefOf(text.trim()));
  }

  function suggest(term: string) {
    text = term;
    straight = true;
    navigate(hrefOf(term));
  }

  // Unused for as long as the flow says, it starts over, for the next
  // person: the search cleared, nothing opened.
  let timer: ReturnType<typeof setTimeout> | undefined;
  function poke() {
    clearTimeout(timer);
    const wait = view?.reset_after ?? 0;
    if (wait > 0) timer = setTimeout(startOver, wait * 1000);
  }
  function startOver() {
    text = '';
    if (router.search) navigate(base, true);
    else opened = null;
    field?.focus();
  }
  onMount(() => {
    const events = ['pointerdown', 'keydown', 'input', 'scroll', 'touchstart'] as const;
    for (const e of events) window.addEventListener(e, poke, { passive: true });
    return () => {
      for (const e of events) window.removeEventListener(e, poke);
      clearTimeout(timer);
    };
  });
  $effect(() => {
    if (view?.reset_after) untrack(poke);
  });

  const openedAsset = $derived(opened ? assets.find((a) => a.asset.id === opened!.id) : undefined);
</script>

{#if signIn}
  <h1 class="text-2xl font-bold">Sign in to search here</h1>
  <p class="mt-2 text-sm">This page is for people signed in.</p>
  <p class="mt-4"><a class="btn-primary" href={signInPath(router.path + router.search)}>Sign in</a></p>
{:else if opened}
  <p class="mb-3"><a class="inline-flex min-h-11 items-center underline underline-offset-4" href={hrefOf(shown ?? '')}>Back to the results</a></p>
  <h1 class="mb-4 text-2xl font-bold outline-none" tabindex="-1" bind:this={heading}>{openedAsset?.asset.name ?? 'Found'}</h1>
  {#if opened.result}
    <ScanResult result={opened.result}>
      <!-- Nothing here leads away from the page, nor changes anything. -->
      {#snippet actions()}{/snippet}
    </ScanResult>
  {:else if openedAsset}
    <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
      {#if openedAsset.location.length}<dt class="font-medium">Where</dt><dd>{openedAsset.location.map((p) => p.name).join(' › ')}</dd>{/if}
      {#if openedAsset.categories.length}<dt class="font-medium">Categories</dt><dd>{openedAsset.categories.map((c) => c.name).join(', ')}</dd>{/if}
      {#if openedAsset.generator_keys.length}<dt class="font-medium">Codes</dt><dd class="font-mono">{openedAsset.generator_keys.map((k) => k.assigned_code).join(', ')}</dd>{/if}
    </dl>
  {/if}
{:else}
  <h1 class="mb-2 text-3xl font-bold">{view?.title || view?.name || 'Search'}</h1>
  {#if view?.text}<p class="mb-4 text-lg">{view.text}</p>{/if}
  <form class="mb-4 flex flex-wrap items-start gap-2" role="search" aria-label="Search" onsubmit={submit}>
    <div class="min-w-0 flex-1">
      <label class="sr-only" for="flow-search-field">Search</label>
      <input
        id="flow-search-field"
        type="search"
        class="input min-h-14 text-lg"
        autocomplete="off"
        bind:this={field}
        bind:value={text}
        aria-invalid={problem ? 'true' : undefined}
        aria-describedby={problem ? 'flow-search-problem' : undefined}
      />
      {#if problem}<QueryProblem id="flow-search-problem" query={problemQuery} {problem} />{/if}
    </div>
    <button type="submit" class="btn-primary min-h-14 px-6 text-lg">Search</button>
  </form>
  <ErrorMessage {error} />
  {#if loading}
    <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Searching…</p>
  {:else if view && shown}
    <SearchSuggestions {suggestions} onpick={suggest} />
    {#if assets.length === 0}
      {#if !problem}<p class="text-sm text-zinc-600 dark:text-zinc-400" role="status">Nothing here matches.</p>{/if}
    {:else}
      <p class="sr-only" role="status">{assets.length} found.</p>
      <AssetResults {assets} {matched} columns={view.columns} hrefFor={(a) => hrefOf(shown ?? '', a.asset.id)} linkPlaces={false} star={false} />
    {/if}
  {/if}
  {#if session.user && view?.public}
    <p class="mt-6 text-xs text-zinc-600 dark:text-zinc-400">Signed in, you search with your own permissions; signed out, this page finds what the public role may read.</p>
  {/if}
{/if}
