// The app's pages. Each sub-phase adds its own; entries with a nav label
// appear in the main navigation for users holding one of their permissions.

import type { Component } from 'svelte';
import type { Permission } from './lib/models';
import Home from './views/Home.svelte';

export interface Route {
  pattern: string;
  component: Component<{ params: Record<string, string> }>;
  title: string;
  /** Any one of these lets the user open the page; none means any user. */
  permissions?: Permission[];
  /** The label in the main navigation, if the page is listed there. */
  nav?: string;
}

export const routes: Route[] = [{ pattern: '/', component: Home, title: 'Home' }];
