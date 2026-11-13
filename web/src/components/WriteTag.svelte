<script lang="ts">
  import { onDestroy } from 'svelte';
  import { request } from '../lib/api';
  import { lockTag, nfcMessage, scanLink, webNFC, writeLink, type NDEFWriter } from '../lib/nfc';
  import ConfirmButton from './ConfirmButton.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  // Writes an asset's scan link to an NFC tag (docs/scanning.md#nfc-tags):
  // through Web NFC where the browser has it, and elsewhere shows the link
  // to copy into an app that writes tags.
  let { assetId }: { assetId: string } = $props();

  const NFC = webNFC();
  let open = $state(false);
  let link = $state('');
  /** What the tag is waiting on, or has had done to it. */
  let step = $state<'' | 'writing' | 'written' | 'locking' | 'locked'>('');
  let error = $state('');
  let copied = $state(false);
  let reader: NDEFWriter | null = null;
  let stop: AbortController | null = null;

  onDestroy(() => stop?.abort());

  async function linkFor(): Promise<string> {
    let base: string | undefined;
    try {
      base = (await request<{ base_url?: string }>('GET', '/info')).base_url;
    } catch {
      // This page's address will do.
    }
    return scanLink(base, assetId, window.location.origin);
  }

  async function start() {
    error = '';
    copied = false;
    open = true;
    link = await linkFor();
    if (NFC) await write();
  }

  async function attempt(waiting: typeof step, done: typeof step, run: (r: NDEFWriter, signal: AbortSignal) => Promise<void>) {
    error = '';
    stop?.abort();
    stop = new AbortController();
    const before = step;
    step = waiting;
    try {
      reader ??= new NFC!();
      await run(reader, stop.signal);
      step = done;
    } catch (err) {
      step = before;
      error = nfcMessage(err);
    }
  }

  const write = () => attempt('writing', 'written', (r, signal) => writeLink(r, link, signal));
  const lock = () => attempt('locking', 'locked', (r, signal) => lockTag(r, signal));

  function cancel() {
    stop?.abort();
  }

  function close() {
    stop?.abort();
    open = false;
    step = error = '';
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(link);
      copied = true;
    } catch {
      error = 'Couldn’t copy it. Select the link and copy it instead.';
    }
  }
</script>

{#if !open}
  <button type="button" class="btn-secondary" onclick={start}>Write tag</button>
{:else}
  <section class="card" aria-labelledby="write-tag-heading">
    <h2 id="write-tag-heading" class="mb-2 text-lg font-semibold">Write an NFC tag</h2>
    {#if NFC}
      {#if step === 'writing'}
        <p role="status" class="mb-2 text-sm">Hold a blank tag against the back of the phone…</p>
        <button type="button" class="btn-secondary" onclick={cancel}>Stop</button>
      {:else if step === 'locking'}
        <p role="status" class="mb-2 text-sm">Hold the tag against the phone again to lock it…</p>
        <button type="button" class="btn-secondary" onclick={cancel}>Stop</button>
      {:else if step === 'written' || step === 'locked'}
        <p role="status" class="mb-2 text-sm text-green-700 dark:text-green-400">
          {step === 'locked' ? 'Locked. The tag holds' : 'Written. The tag holds'} <span class="break-all">{link}</span>, and a phone held to it opens this asset’s scan page.
        </p>
        {#if step === 'written'}
          <p class="mb-2 text-sm">Lock it so that it can’t be written over. Locking can’t be undone: the tag can never be written again.</p>
          <div class="flex flex-wrap gap-2">
            <ConfirmButton label="Lock the tag" question="Lock it for good? It can never be written again." onconfirm={lock} />
            <button type="button" class="btn-secondary" onclick={write}>Write another</button>
          </div>
        {/if}
      {:else}
        <button type="button" class="btn-primary" onclick={write}>Try again</button>
      {/if}
    {:else}
      <p class="mb-2 text-sm">This browser can’t write NFC tags; Chrome on Android can. Copy the link below into an app that writes tags, such as NFC Tools, as a link (URL) record.</p>
      <div class="flex flex-wrap items-center gap-2">
        <input class="input min-w-0 flex-1" readonly value={link} aria-label="The link to write" onfocus={(e) => e.currentTarget.select()} />
        <button type="button" class="btn-secondary" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
      </div>
    {/if}
    <ErrorMessage {error} />
    <p class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">
      The tag holds this server’s address, as its QR codes do: if the address changes, write the tag again.
      <a class="underline" href="/help/nfc">About NFC tags</a>
    </p>
    <p class="mt-2"><button type="button" class="btn-secondary" onclick={close}>Done</button></p>
  </section>
{/if}
