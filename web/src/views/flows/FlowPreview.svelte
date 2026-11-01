<script lang="ts">
  // A step as a phone shows it, beside the editor (README §6.7): drawn here
  // from its settings, with sample answers where a run would have its own.
  import type { Category } from '../../lib/models';
  import { optionLabel, type Progress, type Step } from '../../lib/flows';
  import RunProgress from '../../components/RunProgress.svelte';
  import { kindLabels } from '../../lib/flowEditor';

  let { step, flowName, categories, progress = null }: { step: Step | null; flowName: string; categories: Category[]; progress?: Progress | null } = $props();

  const offered = $derived(step?.kind === 'choose_category' && step.categories?.length ? categories.filter((c) => step!.categories!.includes(c.id)) : categories);
</script>

<figure class="mx-auto w-[22rem] max-w-full rounded-[2rem] border-8 border-zinc-800 bg-zinc-50 p-4 text-base shadow dark:bg-zinc-950" aria-label="Preview, as a phone shows it">
  {#if !step}
    <p class="text-zinc-600 dark:text-zinc-400">Choose a step to see it.</p>
  {:else}
    <div class="space-y-3" inert>
      <RunProgress flowName={flowName || 'Flow'} {progress} />
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
        {#each step.questions ?? [] as q, i (i)}
          <div data-question={q.type || 'text'}>
            <span class="label">{q.label || 'A question'}{q.required ? ' *' : ''}</span>
            {#if q.type === 'yes_no'}
              <div class="grid grid-cols-2 gap-2"><div class="flow-choice">Yes</div><div class="flow-choice">No</div></div>
            {:else if q.type === 'choice'}
              {#each q.options ?? [] as o, j (j)}<div class="flow-choice">{optionLabel(o) || 'A choice'}</div>{/each}
            {:else if q.type === 'photo'}
              <div class="flow-input text-zinc-500">Take a photo, or choose one</div>
            {:else if q.type === 'long_text'}
              <div class="flow-input h-24"></div>
            {:else}
              <div class="flow-input text-zinc-500">{q.default || (q.type === 'date' ? 'A date' : q.type === 'number' ? 'A number' : '')}</div>
            {/if}
            {#if q.help}<p class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{q.help}</p>{/if}
          </div>
        {/each}
        <div class="flow-button-primary">Next</div>
      {:else if step.kind === 'find'}
        {#if step.find_by?.includes('scan')}<div><span class="label">Its code</span><div class="flow-input"></div></div>{/if}
        {#if step.find_by?.includes('search')}<div class="flow-input text-zinc-500">Search by name</div>{/if}
        {#if step.find_by?.includes('list')}<div class="flow-choice">Something here</div><div class="flow-choice">Something else</div>{/if}
      {:else if step.kind === 'create'}
        {#if step.location?.mode === 'scan'}<div><span class="label">Scan where you put it</span><div class="flow-input"></div></div>{:else}<p class="text-sm text-zinc-600 dark:text-zinc-400">Files it as {step.name || '…'}, asking nothing.</p>{/if}
      {:else if step.kind === 'label' || step.kind === 'change' || step.kind === 'set_fields'}
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
