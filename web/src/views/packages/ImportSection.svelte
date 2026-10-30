<script lang="ts">
  import { messageOf } from '../../lib/errors';
  import { formatBytes, type UploadState } from '../../lib/upload';
  import {
    actionSaid,
    choiceLabels,
    clashes,
    importJob,
    importSummary,
    itemKey,
    kindNames,
    mergeable,
    type Choice,
    type ImportPlan,
    type ImportRequest,
    type MergeKind,
    type TreeNode,
  } from '../../lib/packages';
  import ArchiveUpload from '../../components/ArchiveUpload.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { tree, onimported }: { tree: TreeNode[]; onimported?: () => void } = $props();

  // Importing a package, or kinds of a backup (README §2.3): checked first,
  // saying what it would do; what clashes with something here is chosen
  // for; then imported as checked.
  let uploaded = $state<UploadState | null>(null);
  let anchor = $state('');
  let choices = $state<Record<string, Choice>>({});
  let kinds = $state<MergeKind[]>([]);
  let plan = $state<ImportPlan | null>(null);
  let checkedFor = $state('');
  let checking = $state(false);
  let importing = $state(false);
  let done = $state<ImportPlan | null>(null);
  let error = $state('');

  const isBackup = $derived(plan?.header.type === 'backup');
  const offered = $derived(mergeable.filter((k) => plan?.header.kinds.includes(k)));
  const request = $derived<ImportRequest>({
    upload_id: uploaded?.id ?? '',
    anchor,
    choices: { ...choices },
    kinds: isBackup ? [...kinds] : undefined,
  });
  // the plan shown is what is imported: changing what is asked checks again
  const current = $derived(plan !== null && checkedFor === JSON.stringify(request));
  const blocked = $derived(!plan || plan.problems.length > 0 || plan.missing.length > 0);
  const canImport = $derived(current && !blocked && !importing && !checking && (!isBackup || kinds.length > 0));

  // categories, each under its parent, to import under
  const anchors = $derived.by(() => {
    const out: { id: string; label: string }[] = [];
    const walk = (nodes: TreeNode[], depth: number) => {
      for (const n of nodes) {
        out.push({ id: n.id, label: `${'— '.repeat(depth)}${n.name}` });
        walk(n.children, depth + 1);
      }
    };
    walk(tree, 0);
    return out;
  });

  async function check() {
    if (!uploaded) return;
    error = '';
    done = null;
    checking = true;
    const asked = $state.snapshot(request) as ImportRequest;
    try {
      const out = await importJob('/import/check', asked);
      plan = out.plan;
      checkedFor = JSON.stringify(asked);
      error = out.error;
      if (plan && plan.header.type === 'backup' && asked.kinds === undefined) {
        // first check of a backup: every kind it holds that can be merged
        kinds = mergeable.filter((k) => plan!.kinds.includes(k));
        checkedFor = JSON.stringify({ ...asked, kinds: [...kinds] });
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      checking = false;
    }
  }

  function uploadedOne(up: UploadState) {
    uploaded = up;
    plan = null;
    choices = {};
    kinds = [];
    void check();
  }

  async function runImport() {
    if (!plan) return;
    error = '';
    importing = true;
    try {
      const out = await importJob('/import', { ...($state.snapshot(request) as ImportRequest), fingerprint: plan.fingerprint });
      if (out.error) {
        error = out.error;
        if (out.plan) plan = out.plan;
      } else {
        done = out.plan;
        onimported?.();
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      importing = false;
    }
  }

  function choose(key: string, c: Choice) {
    choices = { ...choices, [key]: c };
  }

  const what = (p: ImportPlan) =>
    p.header.package ? `the package ${p.header.package.name}${p.header.package.version ? ` ${p.header.package.version}` : ''}` : 'a backup';
</script>

<section class="card mb-6 max-w-3xl space-y-4" aria-labelledby="import-heading">
  <h2 id="import-heading" class="text-lg font-semibold">Import a package</h2>
  <p class="text-sm">
    Bring a package, or parts of a backup, into this instance. Nothing changes until you import it: it is checked first, and
    what it would create, use, copy or skip is shown. Everything comes in new, so a package can be imported twice, under
    different categories. People, settings, passwords and sessions never come in.
  </p>

  <ArchiveUpload label="Choose a package or backup" onuploaded={(up) => uploadedOne(up)} />

  {#if uploaded}
    <label class="block max-w-md text-sm">
      Put its top categories under
      <select class="input mt-1" bind:value={anchor} data-testid="anchor">
        <option value="">The top level</option>
        {#each anchors as a (a.id)}<option value={a.id}>{a.label}</option>{/each}
      </select>
    </label>
  {/if}

  {#if checking}
    <p class="text-sm" role="status">Checking it, every byte of it…</p>
  {/if}

  {#if plan && !done}
    <div class="space-y-3" data-testid="import-plan">
      <p class="text-sm">
        Importing {what(plan)}{plan.header.package?.author ? `, by ${plan.header.package.author}` : ''}:
        {importSummary(plan)}
        {#if plan.files > 0}Its {plan.files.toLocaleString()} files are {formatBytes(plan.file_bytes)}.{/if}
      </p>
      {#if plan.header.package?.description}<p class="text-sm italic">{plan.header.package.description}</p>{/if}

      {#if isBackup}
        <fieldset class="space-y-1">
          <legend class="text-sm font-medium">What to merge from the backup</legend>
          {#each offered as k (k)}
            <label class="flex gap-2 text-sm"><input type="checkbox" value={k} bind:group={kinds} /> {k}</label>
          {/each}
        </fieldset>
      {/if}

      {#each plan.warnings as w (w)}
        <p class="rounded border border-amber-300 bg-amber-50 p-2 text-sm dark:border-amber-700 dark:bg-amber-950" role="note">{w}</p>
      {/each}
      {#if plan.missing.length > 0}
        <p class="text-sm" role="alert" data-testid="import-missing">
          It needs permissions you do not have: {plan.missing.join('; ')}.
        </p>
      {/if}
      {#if plan.problems.length > 0}
        <div class="text-sm" role="alert" data-testid="import-problems">
          <p class="font-medium">It cannot be imported as it is:</p>
          <ul class="list-disc pl-5">{#each plan.problems as p (p)}<li>{p}</li>{/each}</ul>
        </div>
      {/if}

      {#if clashes(plan).length > 0}
        <fieldset class="space-y-2" data-testid="clashes">
          <legend class="text-sm font-medium">What is here already</legend>
          {#each clashes(plan) as it (itemKey(it))}
            {@const key = itemKey(it)}
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
              <span>
                <span class="font-medium">{it.name}</span>, a {kindNames[it.kind]},
                {it.identical ? 'is the same as' : 'clashes with'} {it.existing?.name ? `${it.existing.name} here` : 'one here'}
              </span>
              <select
                class="input w-auto"
                aria-label={`What to do with the ${kindNames[it.kind]} ${it.name}`}
                value={choices[key] ?? it.action}
                onchange={(e) => choose(key, e.currentTarget.value as Choice)}
              >
                {#each it.choices ?? [] as c (c)}<option value={c}>{choiceLabels[c]}</option>{/each}
              </select>
            </div>
          {/each}
        </fieldset>
      {/if}

      <details class="text-sm">
        <summary class="cursor-pointer">Everything it brings ({plan.items.length})</summary>
        <ul class="mt-2 space-y-1" data-testid="import-items">
          {#each plan.items as it (itemKey(it))}
            <li>
              <span class="font-medium">{it.name}</span>, a {kindNames[it.kind]}: {actionSaid(it)}.{it.note ? ` ${it.note}` : ''}
            </li>
          {/each}
        </ul>
      </details>

      {#if plan.permissions.length > 0}
        <p class="text-xs text-zinc-600 dark:text-zinc-400">It needs {plan.permissions.join('; ')}.</p>
      {/if}

      {#if current}
        <button type="button" class="btn-primary" disabled={!canImport} onclick={runImport} data-testid="import">
          {importing ? 'Importing…' : 'Import'}
        </button>
      {:else}
        <button type="button" class="btn-primary" disabled={checking} onclick={check} data-testid="check-again">
          Check it with these choices
        </button>
      {/if}
    </div>
  {/if}

  {#if done}
    <div class="space-y-1 rounded border border-green-300 bg-green-50 p-3 text-sm dark:border-green-800 dark:bg-green-950" role="status" data-testid="imported">
      <p class="font-medium">Imported {what(done)}{done.anchor ? ` under ${done.anchor.name}` : ''}.</p>
      <p>{importSummary(done)}</p>
    </div>
  {/if}

  <ErrorMessage {error} />
</section>
