// The icons pages draw (docs/icons.md): the build's icons, the palette they
// paint with, which icon each category is drawn with, and the unknown
// icon, drawn for a name the build does not draw. Read once, when a page
// first draws an icon, again when someone else signs in, and on
// refresh() after an icon changes. With no icons, nothing is drawn, and
// pages look as they would without them.

import { request } from './api';
import type { IrDocument, IrIcon } from './isolex/ir';
import type { IconColor } from './isolex/palette';
import type { ResolvedIcon } from './models';
import { session } from './session.svelte';
import { isWrapper } from './icons';

interface Overview {
  category_icons?: Record<string, string>;
  rendition_icons?: { label?: Record<string, string> };
  unknown?: IrIcon;
}

class Iconset {
  palette = $state<IconColor[]>([]);
  /** The icon each category is drawn with, its own or its nearest ancestor's, by name. */
  categories = $state<Record<string, string>>({});
  /** The icon each category is drawn with on labels, as categories; its screen icon when it has no label icon. */
  labelCategories = $state<Record<string, string>>({});
  #icons = $state<Map<string, IrIcon>>(new Map());
  #unknown = $state<IrIcon | null>(null);
  #reading: Promise<void> | null = null;
  #for: string | null = null;

  /** Reads the icons, unless they have been for whoever is signed in. */
  load(): Promise<void> {
    const user = session.user?.id ?? null;
    // the icons are not public: asking without a session would end one
    if (!user || !session.can('assets.read')) return Promise.resolve();
    if (this.#reading && this.#for === user) return this.#reading;
    this.#for = user;
    return (this.#reading = this.#read());
  }

  /** Reads the icons again, after one changed. */
  refresh(): Promise<void> {
    this.#reading = null;
    return this.load();
  }

  async #read(): Promise<void> {
    try {
      const [overview, ir, palette] = await Promise.all([
        request<Overview>('GET', '/icons'),
        request<IrDocument>('GET', '/icons/ir'),
        request<{ palette: IconColor[] }>('GET', '/icons/palette'),
      ]);
      this.#icons = new Map((ir.icons ?? []).map((i) => [i.name, i]));
      this.categories = overview.category_icons ?? {};
      this.labelCategories = overview.rendition_icons?.label ?? this.categories;
      this.#unknown = overview.unknown ?? null;
      this.palette = palette.palette ?? [];
    } catch {
      // without them, pages draw no icons; the next load tries again
      this.#reading = null;
    }
  }

  /** The icon by a name, or the unknown icon when the build draws none by it. */
  named(name: string): IrIcon | undefined {
    return this.#icons.get(name) ?? this.#unknown ?? undefined;
  }

  /** The icon a category is drawn with, if any. */
  forCategory(id: string): IrIcon | undefined {
    const name = this.categories[id];
    return name ? this.named(name) : undefined;
  }

  /** The icon an asset is drawn with, as the server resolved it, if any: drawn with its own texts, when it has them. */
  forAsset(icon: ResolvedIcon | null | undefined): IrIcon | undefined {
    if (!icon) return undefined;
    return icon.drawn?.[icon.name] ?? this.named(icon.name);
  }

  /** The icon drawn for a name the build does not draw. */
  get unknown(): IrIcon | null {
    return this.#unknown;
  }

  /** Every icon the build draws, by name, but the wrappers drawing assets' params. */
  get all(): IrIcon[] {
    return [...this.#icons.values()].filter((i) => !isWrapper(i.name)).sort((a, b) => a.name.localeCompare(b.name));
  }
}

export const iconset = new Iconset();
