<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Vendor } from '../../lib/models';
  import { navigate, router } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import { contactSummary, mainContact, siteName, telHref, websiteHref } from '../../lib/vendors';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // /vendors: the suppliers someone may see (docs/vendors.md), by name, and
  // a search through them.
  let { params: _params }: { params: Record<string, string> } = $props();

  let vendors = $state<Vendor[]>([]);
  let loading = $state(true);
  let error = $state('');
  let search = $state(router.query.get('q') ?? '');
  // The search the list shows.
  let applied = $state(router.query.get('q') ?? '');

  onMount(() => void load());

  // Each load is numbered, so a slow answer to an older one cannot replace
  // the list a newer one asked for.
  let loads = 0;

  async function load() {
    const mine = ++loads;
    loading = true;
    error = '';
    const q = search.trim();
    applied = q;
    try {
      const found = (await request<{ vendors: Vendor[] }>('GET', q ? `/vendors?q=${encodeURIComponent(q)}` : '/vendors')).vendors ?? [];
      if (mine !== loads) return;
      vendors = found;
    } catch (err) {
      if (mine === loads) error = messageOf(err);
    } finally {
      if (mine === loads) loading = false;
    }
  }

  // Keep the search in the address, so it survives reloads and links.
  function applySearch(event: SubmitEvent) {
    event.preventDefault();
    const q = search.trim();
    navigate('/vendors' + (q ? `?q=${encodeURIComponent(q)}` : ''), true);
    void load();
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <div>
    <h1 class="text-2xl font-bold">Vendors</h1>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">The suppliers things are bought from, and how to reach them.</p>
  </div>
  {#if session.can('vendors.manage')}
    <a class="btn-primary" href="/vendors/new">New vendor</a>
  {/if}
</div>

<form class="mb-4 flex flex-wrap items-end gap-3" role="search" onsubmit={applySearch}>
  <div class="min-w-48 flex-1">
    <label class="label" for="vendor-search">Search</label>
    <input id="vendor-search" class="input" type="search" placeholder="Name, account number, contact or notes" bind:value={search} />
  </div>
  <button type="submit" class="btn-secondary">Search</button>
</form>

<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading vendors…</p>
{:else if vendors.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">{applied ? 'No vendors match.' : 'No vendors yet.'}</p>
{:else}
  <ul class="space-y-2" aria-label="Vendors">
    {#each vendors as v (v.id)}
      {@const site = websiteHref(v.website)}
      {@const contact = mainContact(v)}
      <li class="card flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <a class="inline-flex min-h-11 items-center font-medium hover:underline" href="/vendors/{encodeURIComponent(v.id)}">{v.name}</a>
        <span class="flex flex-wrap items-center gap-x-4 text-sm text-zinc-600 dark:text-zinc-400">
          {#if site}<a class="inline-flex min-h-11 items-center underline" href={site} rel="noopener noreferrer" target="_blank">{siteName(v.website)}</a>{/if}
          {#if contact}
            <span>
              {contactSummary(contact)}{#if contact.email}{' · '}<a class="underline" href="mailto:{contact.email}">{contact.email}</a>{/if}{#if contact.phone}{' · '}<a class="underline" href={telHref(contact.phone)}>{contact.phone}</a>{/if}
            </span>
          {/if}
        </span>
      </li>
    {/each}
  </ul>
{/if}
