<script lang="ts">
  // Roles to hold, each in every scope or narrowed to some (README §3.3):
  // ticking scopes under a role narrows it to them, and says what that leaves
  // out. Ticking none holds it in every scope its permissions name.
  import type { Role, RoleAssignment, Scope } from '../lib/models';
  import { narrowedOut, permissionLabel } from '../lib/permissions';

  let {
    roles,
    scopes,
    value = $bindable(),
    id,
    legend = 'Roles',
  }: { roles: Role[]; scopes: Scope[]; value: RoleAssignment[]; id: string; legend?: string } = $props();

  const held = (roleId: string) => value.find((a) => a.role_id === roleId);

  function toggle(roleId: string, on: boolean) {
    value = on ? [...value, { role_id: roleId, scope_ids: [] }] : value.filter((a) => a.role_id !== roleId);
  }

  function toggleScope(roleId: string, scopeId: string, on: boolean) {
    value = value.map((a) =>
      a.role_id !== roleId ? a : { ...a, scope_ids: on ? [...a.scope_ids, scopeId] : a.scope_ids.filter((s) => s !== scopeId) },
    );
  }

  function leftOut(r: Role, a: RoleAssignment): string {
    const names = a.scope_ids.map((sid) => scopes.find((s) => s.id === sid)?.name ?? sid);
    return narrowedOut(r.permissions, names).map(permissionLabel).join(', ');
  }
</script>

<fieldset class="space-y-1">
  <legend class="label">{legend}</legend>
  {#each roles as r (r.id)}
    {@const a = held(r.id)}
    <div>
      <label class="inline-flex items-center gap-2 text-sm">
        <input type="checkbox" checked={!!a} onchange={(e) => toggle(r.id, e.currentTarget.checked)} />
        {r.name}
      </label>
      {#if a && scopes.length}
        <fieldset class="mb-2 ml-6" aria-describedby="{id}-{r.id}-where">
          <legend class="text-xs text-zinc-600 dark:text-zinc-400">Only in these scopes</legend>
          <div class="flex flex-wrap gap-x-4">
            {#each scopes as s (s.id)}
              <label class="inline-flex items-center gap-1 text-sm">
                <input type="checkbox" checked={a.scope_ids.includes(s.id)} onchange={(e) => toggleScope(r.id, s.id, e.currentTarget.checked)} />
                <span class="font-mono">{s.name}</span>
              </label>
            {/each}
          </div>
          <p id="{id}-{r.id}-where" class="text-xs text-zinc-600 dark:text-zinc-400">
            {#if a.scope_ids.length === 0}
              None ticked: in every scope its permissions name.
            {:else if leftOut(r, a)}
              Leaves out: {leftOut(r, a)}.
            {:else}
              Its asset permissions, in these scopes only.
            {/if}
          </p>
        </fieldset>
      {/if}
    </div>
  {/each}
</fieldset>
