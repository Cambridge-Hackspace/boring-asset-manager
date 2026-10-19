<script lang="ts">
  import { restack, type Restack, type TemplateElement } from '../../lib/template';

  // Moves the selected elements over or under the others. A button is
  // disabled when it would change nothing.
  let { elements, selected, onrestack }: { elements: TemplateElement[]; selected: number[]; onrestack: (how: Restack) => void } = $props();

  const moves: [Restack, string][] = [
    ['back', 'Send to back'],
    ['backward', 'Send backward'],
    ['forward', 'Bring forward'],
    ['front', 'Bring to front'],
  ];
  const unchanged = (how: Restack) => restack(elements, selected, how).selected.every((k, n) => k === [...selected].sort((a, b) => a - b)[n]);
</script>

<div class="flex flex-wrap gap-1" role="group" aria-label="Stacking order">
  {#each moves as [how, label] (how)}
    <button type="button" class="btn-secondary" onclick={() => onrestack(how)} disabled={unchanged(how)}>{label}</button>
  {/each}
</div>
