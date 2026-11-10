// The assistant beside the page (docs/assistant.md): what the API says of
// it, its conversations and guides, a turn streamed a JSON object a line,
// and the panel's state, shared by the header, the panel and the pages.

import { ApiError, request } from './api';
import { matchRoute, router } from './router.svelte';

/** A route as the panel reads it: its pattern and title. */
export interface TitledRoute {
  pattern: string;
  title: string;
}

export interface AssistantUse {
  messages: number;
  tokens: number;
}

export interface AssistantStatus {
  available: boolean;
  enabled: boolean;
  name: string;
  provider: string;
  model: string;
  limits: { person_messages: number; person_tokens: number; instance_messages: number; instance_tokens: number };
  used: AssistantUse;
  instance_used: AssistantUse;
  resets_at: string;
}

export interface Conversation {
  id: string;
  title: string;
  guide_id?: string;
  progress?: Record<string, string>;
  pending_call?: string;
  created_at: string;
  updated_at: string;
}

export interface Block {
  type: 'text' | 'tool_call' | 'tool_result';
  text?: string;
  id?: string;
  name?: string;
  title?: string;
  input?: unknown;
  content?: string;
  is_error?: boolean;
}

/** Something on the page a message is sent from, which the person can take away. */
export interface ContextItem {
  kind: string;
  id?: string;
  label: string;
  path?: string;
}

export interface MessageView {
  id: string;
  role: 'user' | 'assistant' | 'tool';
  blocks: Block[];
  context?: ContextItem[];
  created_at: string;
}

export interface PendingView {
  id: string;
  name: string;
  title: string;
  input: unknown;
}

export interface NamedRef {
  id: string;
  name: string;
}

export interface Guide {
  id: string;
  slug: string;
  name: string;
  purpose: string;
  enabled: boolean;
  category_ids: string[];
  link_type_ids: string[];
  questions: string[];
  created_at: string;
  updated_at: string;
  url: string;
  categories: NamedRef[];
  link_types: NamedRef[];
}

export interface GuideInput {
  slug: string;
  name: string;
  purpose: string;
  enabled: boolean;
  category_ids: string[];
  link_type_ids: string[];
  questions: string[];
}

export interface AssistantSettings {
  enabled: boolean;
  provider: string;
  endpoint: string;
  model: string;
  key_set: boolean;
  name: string;
  max_tokens: number;
  max_steps: number;
  max_tools: number;
  person_messages: number;
  person_tokens: number;
  instance_messages: number;
  instance_tokens: number;
  keep_days: number;
  acknowledged_by?: string;
  acknowledged_at?: string;
  updated_at?: string;
  /** The settings the configuration sets, which Admin cannot. */
  fixed: Record<string, boolean>;
  /** What turning it on means, said plainly. */
  disclosure: string;
  default_endpoints: Record<string, string>;
  default_models: Record<string, string>;
}

export interface ConversationDetail {
  conversation: Conversation;
  messages: MessageView[];
  pending: PendingView | null;
  guide?: Guide;
}

/** One line of a turn, as the server streams it. */
export type TurnLine =
  | { type: 'message'; message: MessageView }
  | { type: 'changed'; tool: string }
  | { type: 'confirm'; pending: PendingView; description?: string; destructive?: boolean }
  | { type: 'progress'; progress: Record<string, string> }
  | { type: 'limit'; error: string; resets_at: string }
  | { type: 'error'; error: string }
  | { type: 'done'; conversation: Conversation; pending?: PendingView | null };

export const assistantApi = {
  status: () => request<AssistantStatus>('GET', '/assistant'),
  conversations: async () => (await request<{ conversations: Conversation[] }>('GET', '/assistant/conversations')).conversations ?? [],
  conversation: (id: string) => request<ConversationDetail>('GET', `/assistant/conversations/${encodeURIComponent(id)}`),
  start: async (guide?: string) =>
    (await request<{ conversation: Conversation }>('POST', '/assistant/conversations', guide ? { guide } : {})).conversation,
  remove: (id: string) => request<unknown>('DELETE', `/assistant/conversations/${encodeURIComponent(id)}`),
  guides: async () => (await request<{ guides: Guide[] }>('GET', '/assistant/guides')).guides ?? [],
  guide: async (slug: string) => (await request<{ guide: Guide }>('GET', `/assistant/guides/${encodeURIComponent(slug)}`)).guide,
  saveGuide: async (slug: string | null, input: GuideInput) =>
    (await request<{ guide: Guide }>(slug ? 'PUT' : 'POST', slug ? `/assistant/guides/${encodeURIComponent(slug)}` : '/assistant/guides', input)).guide,
  deleteGuide: (slug: string) => request<unknown>('DELETE', `/assistant/guides/${encodeURIComponent(slug)}`),
  settings: () => request<AssistantSettings>('GET', '/settings/assistant'),
  saveSettings: (body: Record<string, unknown>) => request<AssistantSettings>('PUT', '/settings/assistant', body),
};

/** A limit reached, with when it resets. */
export class LimitError extends ApiError {
  constructor(
    message: string,
    readonly resetsAt: string,
  ) {
    super(429, message);
    this.name = 'LimitError';
  }
}

/** Reads lines of JSON from a stream of text, calling online for each. */
export async function readLines(body: ReadableStream<Uint8Array> | null, text: () => Promise<string>, online: (line: TurnLine) => void): Promise<void> {
  const take = (line: string) => {
    const trimmed = line.trim();
    if (!trimmed) return;
    try {
      online(JSON.parse(trimmed) as TurnLine);
    } catch {
      // A line that is not JSON is not one of the turn's.
    }
  };
  if (!body || typeof body.getReader !== 'function') {
    for (const line of (await text()).split('\n')) take(line);
    return;
  }
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let rest = '';
  for (;;) {
    const { done, value } = await reader.read();
    if (value) rest += decoder.decode(value, { stream: !done });
    let at: number;
    while ((at = rest.indexOf('\n')) >= 0) {
      take(rest.slice(0, at));
      rest = rest.slice(at + 1);
    }
    if (done) break;
  }
  take(rest);
}

/**
 * Posts a message or an answer to a change waiting, reading the turn as it
 * goes: each line to online. A refusal is an ApiError; a limit reached
 * before the turn begins, a LimitError.
 */
export async function streamTurn(path: string, body: unknown, online: (line: TurnLine) => void): Promise<void> {
  const res = await fetch('/api/v1' + path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { Accept: 'application/x-ndjson, application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`.trim();
    let data: { error?: string; resets_at?: string } | undefined;
    try {
      data = (await res.json()) as typeof data;
      if (typeof data?.error === 'string') message = data.error;
    } catch {
      // Not JSON; keep the status line.
    }
    if (res.status === 429 && data?.resets_at) throw new LimitError(message, data.resets_at);
    throw new ApiError(res.status, message, [], data);
  }
  await readLines(res.body, () => res.text(), online);
}

/** When a limit resets, in the reader's own time. */
export function resetTime(at: string): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return at;
  return d.toLocaleString(undefined, { weekday: 'long', hour: 'numeric', minute: '2-digit' });
}

/** What a route shows, as a kind the assistant is told of. */
const kinds: [RegExp, string][] = [
  [/^\/assets\/[^/]+(\/(edit|scene))?$/, 'asset'],
  [/^\/assets$/, 'list'],
  [/^\/categories\/[^/]+$/, 'category'],
  [/^\/categories$/, 'list'],
  [/^\/(f|flows)\/[^/]+/, 'flow'],
  [/^\/flows$/, 'list'],
  [/^\/vendors\/[^/]+$/, 'vendor'],
  [/^\/contracts\/[^/]+$/, 'contract'],
  [/^\/orders\/[^/]+/, 'order'],
  [/^\/reports\/[^/]+$/, 'report'],
];

/**
 * The page a message is sent from, by its path alone: what it shows, its
 * ID, and the route's title for its label.
 */
export function contextFor(routes: TitledRoute[], path: string, search = ''): ContextItem | null {
  if (path === '/' || path === '' || /^\/(account|help)(\/|$)/.test(path)) return null;
  for (const route of routes) {
    const params = matchRoute(route.pattern, path);
    if (!params) continue;
    const id = params.id ?? params.slug;
    const found = kinds.find(([re]) => re.test(path))?.[1] ?? 'page';
    // A page naming no one thing, such as /assets/new, is a page.
    const kind = id || found === 'list' ? found : 'page';
    if (id === 'new' || id === 'import' || id === 'graph') return { kind: 'page', label: route.title, path: path + search };
    return { kind, ...(id ? { id } : {}), label: route.title, path: path + search };
  }
  return null;
}

const storeKey = 'bam.assistant';

/** The panel: whether it is open, on which conversation, and the page beside it. */
class Panel {
  open = $state(false);
  conversation = $state<string | null>(null);
  /** Counts the changes the assistant made, so the page beside it reads itself again. */
  version = $state(0);
  status = $state<AssistantStatus | null>(null);
  /** What the page says it shows, more than its path can, such as an asset's name. */
  page = $state<ContextItem | null>(null);
  /** The path the page context was set for. */
  pagePath = $state('');
  /** The app's routes, which App gives it, for naming pages. */
  routes: TitledRoute[] = [];

  constructor() {
    try {
      const saved = JSON.parse(sessionStorage.getItem(storeKey) ?? 'null') as { open?: boolean; conversation?: string | null } | null;
      if (saved) {
        this.open = !!saved.open;
        this.conversation = saved.conversation ?? null;
      }
    } catch {
      // Storage may be unavailable; the panel starts closed.
    }
  }

  get available(): boolean {
    return !!this.status?.available;
  }

  private save(): void {
    try {
      sessionStorage.setItem(storeKey, JSON.stringify({ open: this.open, conversation: this.conversation }));
    } catch {
      // A private window may refuse; nothing is lost but the panel's place.
    }
  }

  async load(): Promise<void> {
    try {
      this.status = await assistantApi.status();
    } catch {
      this.status = null;
    }
  }

  show(conversation?: string | null): void {
    if (conversation !== undefined) this.conversation = conversation;
    this.open = true;
    this.save();
  }

  hide(): void {
    this.open = false;
    this.save();
  }

  choose(conversation: string | null): void {
    this.conversation = conversation;
    this.save();
  }

  changed(): void {
    this.version++;
  }

  /** A page says what it shows, such as an asset by its name. */
  setPage(item: ContextItem | null): void {
    this.page = item;
    this.pagePath = router.path;
  }

  /** What the current page shows: as the page said, or from its path. */
  get context(): ContextItem | null {
    if (this.page && this.pagePath === router.path) return { ...this.page, path: router.path + router.search };
    return contextFor(this.routes, router.path, router.search);
  }
}

export const panel = new Panel();

/** Paths of forms, which are not read again under someone typing in them. */
export function isForm(path: string): boolean {
  return /\/(new|edit)$/.test(path);
}
