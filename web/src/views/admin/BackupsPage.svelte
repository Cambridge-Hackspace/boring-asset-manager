<script lang="ts">
  import { messageOf } from '../../lib/errors';
  import { session } from '../../lib/session.svelte';
  import { formatBytes, type UploadState } from '../../lib/upload';
  import {
    kinds,
    replaceable,
    runJob,
    total,
    type CheckResult,
    type Kind,
    type ReplaceResult,
  } from '../../lib/backups';
  import ArchiveUpload from '../../components/ArchiveUpload.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  // Backing the instance up, and replacing what it holds with a backup
  // (README §2.3): both need users.manage and settings.manage.
  const allowed = $derived(session.can('users.manage') && session.can('settings.manage'));

  let uploaded = $state<UploadState | null>(null);
  let checking = $state(false);
  let checked = $state<CheckResult | null>(null);
  let chosen = $state<Kind[]>([]);
  let confirm = $state('');
  let replacing = $state(false);
  let result = $state<ReplaceResult | null>(null);
  let error = $state('');

  const firstPart = $derived(!checked || checked.header.set.part === 1);
  const canReplace = $derived(
    checked !== null && !replacing && (!firstPart || (chosen.length > 0 && confirm === checked.instance_host && checked.schema_current)),
  );

  async function check(up: UploadState) {
    uploaded = up;
    checked = null;
    result = null;
    error = '';
    checking = true;
    try {
      checked = await runJob<CheckResult>('/backup/check', { upload_id: up.id });
      chosen = replaceable(checked.header);
    } catch (err) {
      error = messageOf(err);
    } finally {
      checking = false;
    }
  }

  async function replace(uploadId: string, which: Kind[], part: number) {
    error = '';
    replacing = true;
    try {
      result = await runJob<ReplaceResult>('/backup/replace', { upload_id: uploadId, kinds: which, confirm, part });
    } catch (err) {
      error = messageOf(err);
    } finally {
      replacing = false;
    }
  }

  function putBack() {
    if (!result?.before_upload_id || !result.summary?.kinds) return;
    void replace(result.before_upload_id, result.summary.kinds, 1);
  }

  const when = (iso: string) => new Date(iso).toLocaleString();
</script>

<PageHeader title="Backups" lede="The whole instance as one archive: to keep somewhere safe, or to replace this instance with." />

{#if !allowed}
  <p class="max-w-2xl text-sm">Backing up and replacing this instance needs both users.manage and settings.manage.</p>
{:else}
  <section class="card mb-6 max-w-2xl space-y-3" aria-labelledby="download-heading">
    <h2 id="download-heading" class="text-lg font-semibold">Download a backup</h2>
    <p class="text-sm">
      A backup holds everything in this instance (assets, the catalog, labels, people’s names and emails, settings, and every
      photo and image) as one file, however large: your browser saves it as it is made. Keep it as carefully as the database.
    </p>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">
      It never holds passwords, sessions or tokens, or notification channels’ passwords. <code>bam backup</code>, on the server,
      makes one that does.
    </p>
    <a class="btn-primary inline-block" href="/api/v1/backup" download data-testid="download-backup">Download a backup</a>
  </section>

  <section class="card mb-6 max-w-2xl space-y-4" aria-labelledby="replace-heading">
    <h2 id="replace-heading" class="text-lg font-semibold">Replace from a backup</h2>
    <p class="text-sm">
      Put back what a backup of this instance holds: all of it, or only some kinds, such as its assets. What you do not choose
      stays as it is. A backup of what is replaced is taken first, so it can be put back.
    </p>

    <ArchiveUpload label="Choose a backup" onuploaded={(up) => check(up)} />

    {#if checking}
      <p class="text-sm" role="status">Checking the backup, every byte of it…</p>
    {/if}

    {#if checked && uploaded}
      <div class="space-y-3" data-testid="checked">
        <p class="text-sm">
          A backup made {when(checked.header.created_at)} by bam {checked.header.bam_version || '(a build from source)'}:
          {total(checked.tables).toLocaleString()} rows and {checked.files.toLocaleString()} files ({formatBytes(checked.file_bytes)}).
          {#if checked.parts > 1}It is part {checked.header.set.part} of {checked.parts}.{/if}
        </p>
        {#each checked.warnings as w (w)}
          <p class="rounded border border-amber-300 bg-amber-50 p-2 text-sm dark:border-amber-700 dark:bg-amber-950" role="note">{w}</p>
        {/each}

        {#if !checked.schema_current}
          <p class="text-sm" role="alert">
            This backup was made by another version of bam. Restore it into a new instance with <code>bam restore</code> instead, then
            back that up.
          </p>
        {:else if firstPart}
          <fieldset class="space-y-2">
            <legend class="text-sm font-medium">What to replace</legend>
            {#each replaceable(checked.header) as k (k)}
              <label class="flex gap-2 text-sm">
                <input type="checkbox" class="mt-1" value={k} bind:group={chosen} />
                <span><span class="font-medium">{kinds[k].label}</span>: {kinds[k].replacing}</span>
              </label>
            {/each}
          </fieldset>
          <p class="text-sm text-zinc-600 dark:text-zinc-400">
            Everything not ticked stays exactly as it is now.
            {#if checked.parts > 1}Its files come in the other parts: upload each after this, in any order.{/if}
          </p>
          <label class="block text-sm">
            Type <code class="font-semibold">{checked.instance_host}</code> to confirm
            <input class="input mt-1" bind:value={confirm} autocomplete="off" spellcheck="false" data-testid="confirm" />
          </label>
          <button type="button" class="btn-danger" disabled={!canReplace} onclick={() => replace(uploaded!.id, chosen, 1)}>
            {replacing ? 'Replacing…' : 'Replace'}
          </button>
        {:else}
          <p class="text-sm">This part adds the files the first part’s rows name.</p>
          <button type="button" class="btn-primary" disabled={!canReplace} onclick={() => replace(uploaded!.id, [], checked!.header.set.part)}>
            {replacing ? 'Adding its files…' : 'Add its files'}
          </button>
        {/if}
      </div>
    {/if}

    {#if result?.summary}
      <div class="space-y-2 rounded border border-green-300 bg-green-50 p-3 text-sm dark:border-green-800 dark:bg-green-950" role="status" data-testid="replaced">
        {#if result.summary.kinds?.length}
          <p class="font-medium">
            Replaced {result.summary.kinds.map((k) => kinds[k].label.toLowerCase()).join(', ')}: {total(result.summary.tables).toLocaleString()}
            rows and {result.summary.files.toLocaleString()} files.
          </p>
          {#if total(result.summary.removed) + total(result.summary.lost) > 0}
            <p>Removed {(total(result.summary.removed) + total(result.summary.lost)).toLocaleString()} rows the backup did not have.</p>
          {/if}
        {:else}
          <p class="font-medium">Added {result.summary.files.toLocaleString()} files ({formatBytes(result.summary.file_bytes)}).</p>
        {/if}
        {#each result.summary.warnings ?? [] as w (w)}<p>{w}</p>{/each}
        {#if result.before_upload_id}
          <p>
            What it replaced was backed up first, and is kept for a day:
            <a class="underline" href={`/api/v1/uploads/${result.before_upload_id}/content`} download>download it</a>, or
            <button type="button" class="underline" onclick={putBack} disabled={replacing}>put it back</button>.
          </p>
        {/if}
      </div>
    {/if}

    <ErrorMessage {error} />
  </section>
{/if}
