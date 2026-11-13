<script lang="ts">
  import { onMount } from 'svelte';
  import { ApiError, request } from '../lib/api';
  import { isOffline } from '../lib/install/app.svelte';
  import { listenForTags, nfcAllowed, nfcAvailable, nfcMessage, ownTagPath, type TagListener, type TagRead } from '../lib/nfc';
  import { findByCode } from '../lib/offline/copy';
  import { offline } from '../lib/offline/offline.svelte';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  // Reads NFC tags on the Scan page (docs/scanning.md#nfc-tags), in Chrome on
  // Android: while the page is open and shown, once the person has allowed
  // it. A tag holding this instance's scan link, or whose serial number is
  // an asset's code, opens that asset. Elsewhere it shows nothing.
  let { onfound }: { onfound: (path: string) => void } = $props();

  const available = nfcAvailable();
  let listener: TagListener | null = null;
  let listening = $state(false);
  let note = $state('');
  let error = $state('');
  let bases = [window.location.origin];

  onMount(() => {
    if (available) {
      void nfcAllowed().then((ok) => {
        if (ok) void listen();
      });
    }
    return () => listener?.stop();
  });

  async function listen() {
    error = note = '';
    try {
      const base = (await request<{ base_url?: string }>('GET', '/info')).base_url;
      if (base) bases = [base, window.location.origin];
    } catch {
      // This page's address will do.
    }
    try {
      listener = listenForTags(found, failed);
      listening = true;
    } catch (err) {
      error = nfcMessage(err);
    }
  }

  function failed(err: unknown) {
    const name = (err as { name?: string } | null)?.name;
    if (name === 'NotAllowedError' || name === 'NotSupportedError') {
      listener?.stop();
      listener = null;
      listening = false;
    }
    error = nfcMessage(err);
  }

  /** Whether a serial number is an asset's code here, or in the copy kept offline. */
  async function known(serial: string): Promise<boolean> {
    const inCopy = async () => {
      await offline.load();
      return !!offline.copy && !!findByCode(offline.copy, serial);
    };
    if (session.offline) return inCopy();
    try {
      await request('GET', `/scan/${encodeURIComponent(serial)}`);
      return true;
    } catch (err) {
      if (err instanceof ApiError) return err.status !== 404;
      return isOffline(err) ? inCopy() : false;
    }
  }

  async function found(tag: TagRead) {
    error = note = '';
    for (const link of tag.links) {
      const path = ownTagPath(link, bases);
      if (path) return onfound(path);
    }
    if (tag.serial && (await known(tag.serial))) return onfound(`/scan/${encodeURIComponent(tag.serial)}`);
    const serial = tag.serial ? ', and its serial number isn’t any asset’s code' : '';
    note = tag.links.length ? `That tag links to somewhere other than this BAM!${serial}.` : `That tag holds no link to an asset here${serial}.`;
  }
</script>

{#if available}
  <section aria-label="NFC tags" class="space-y-2">
    {#if listening}
      <p role="status" class="text-sm">Listening for NFC tags: hold one to the back of the phone.</p>
    {:else}
      <button type="button" class="btn-secondary" onclick={listen}>Read NFC tags</button>
    {/if}
    {#if note}<p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">{note}</p>{/if}
    <ErrorMessage {error} />
  </section>
{/if}
