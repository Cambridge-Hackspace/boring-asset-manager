<script lang="ts">
  // A step as a phone shows it, beside the editor (README §6.7): drawn here
  // from its settings, with sample answers where a run would have its own.
  import type { Category, Field } from '../../lib/models';
  import type { Step } from '../../lib/flows';
  import { kindLabels } from '../../lib/flowEditor';

  let { step, flowName, categories, fields }: { step: Step | null; flowName: string; categories: Category[]; fields: Field[] } = $props();

  // The fields a form asks, as its settings order and name them.
  const asked = $derived.by(() => {
    if (!step || step.kind !== 'form') return [];
    const set = step.fields ?? [];
    const order = [...set.map((s) => s.key), ...fields.map((f) => f.key).filter((k) => !set.some((s) => s.key === k))];
    return order
      .map((k) => ({ f: fields.find((f) => f.key === k), s: set.find((s) => s.key === k) }))
      .filter((x) => x.f && !x.s?.hidden)
      .map((x) => ({ key: x.f!.key, label: x.s?.label || x.f!.label, help: x.s?.help, required: !!x.s?.required }));
  });
  const offered = $derived(step?.kind === 'choose_category' && step.categories?.length ? categories.filter((c) => step!.categories!.includes(c.id)) : categories);
</script>

<figure class="mx-auto w-[22rem] max-w-full rounded-[2rem] border-8 border-zinc-800 bg-zinc-50 p-4 text-base shadow dark:bg-zinc-950" aria-label="Preview, as a phone shows it">
  {#if !step}
    <p class="text-zinc-600 dark:text-zinc-400">Choose a step to see it.</p>
  {:else}
    <div class="space-y-3" inert>
      <p class="text-xs font-medium tracking-wide text-zinc-600 uppercase dark:text-zinc-400">{flowName || 'Flow'}</p>
      <p class="text-2xl font-bold">{step.title || (step.kind === 'done' ? 'Done' : step.kind === 'confirm' ? 'Is this right?' : flowName || kindLabels[step.kind])}</p>
      {#if step.text}<p>{step.text}</p>{/if}
      {#if step.kind === 'choose_category'}
        {#if step.fixed}
          <p class="text-sm text-zinc-600 dark:text-zinc-400">Always {categories.find((c) => c.id === step!.fixed)?.name}: nothing is asked.</p>
        {:else}
          {#each offered as c (c.id)}
            <div class="flow-choice"><span class="h-5 w-5 rounded-full" style="background-color: {c.color_hex || '#71717a'}"></span>{c.name}</div>
          {/each}
        {/if}
      {:else if step.kind === 'form'}
        {#if step.ask_name}
          <div><span class="label">{step.name_label || 'Its name'}</span><div class="flow-input"></div></div>
        {/if}
        {#each asked as f (f.key)}
          <div>
            <span class="label">{f.label}{f.required ? ' *' : ''}</span>
            <div class="flow-input"></div>
            {#if f.help}<p class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{f.help}</p>{/if}
          </div>
        {/each}
        {#each step.questions ?? [] as q, i (i)}
          <div><span class="label">{q.label || 'A question'}{q.required ? ' *' : ''}</span><div class="flow-input"></div></div>
        {/each}
        {#if step.ask_photo}<div><span class="label">A photo</span><div class="flow-input"></div></div>{/if}
        <div class="flow-button-primary">Next</div>
      {:else if step.kind === 'find'}
        {#if step.find_by?.includes('scan')}<div><span class="label">Its code</span><div class="flow-input"></div></div>{/if}
        {#if step.find_by?.includes('search')}<div class="flow-input text-zinc-500">Search by name</div>{/if}
        {#if step.find_by?.includes('list')}<div class="flow-choice">Something here</div><div class="flow-choice">Something else</div>{/if}
      {:else if step.kind === 'create'}
        {#if step.location?.mode === 'scan'}<div><span class="label">Scan where you put it</span><div class="flow-input"></div></div>{:else}<p class="text-sm text-zinc-600 dark:text-zinc-400">Files it as {step.name || '…'}, asking nothing.</p>{/if}
      {:else if step.kind === 'label' || step.kind === 'change'}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">Done as soon as it is reached; nothing is asked.</p>
      {:else if step.kind === 'confirm'}
        <ul class="list-disc pl-5 text-sm"><li>What the next steps will do</li></ul>
        <div class="flow-button-primary">Yes, go ahead</div>
      {:else if step.kind === 'done'}
        <ul class="text-sm"><li>What the flow did</li></ul>
        <div class="flow-button-primary">Start again</div>
      {/if}
    </div>
  {/if}
</figure>
