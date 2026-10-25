<!--
  The icon compiler's diagnostics, each said with where it is: a link to
  the category whose icon it is in, or the library file's path.
-->
<script lang="ts">
  import type { Category, CategoryIcon, IconDiag } from '../lib/models';
  import { iconFile } from '../lib/icons';

  let { diags, categories, icons }: { diags: IconDiag[]; categories: Category[]; icons: CategoryIcon[] } = $props();

  function owner(file: string): Category | undefined {
    const icon = icons.find((i) => iconFile(i.icon_name) === file);
    return icon ? categories.find((c) => c.id === icon.category_id) : undefined;
  }
</script>

<ul class="mt-1 list-disc space-y-0.5 pl-5">
  {#each diags as d, k (k)}
    {@const cat = owner(d.file)}
    <li>
      {#if cat}<a class="underline" href="/categories/{encodeURIComponent(cat.id)}">{cat.name}</a>{:else}<span class="font-mono">{d.file.replace(/^library\//, '')}</span>{/if}, line {d.line}: {d.message}
    </li>
  {/each}
</ul>
