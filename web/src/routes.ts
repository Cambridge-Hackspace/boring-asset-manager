// The app's pages, in navigation order. Entries with a nav label appear in
// the main navigation for users holding one of their permissions.

import type { Component } from 'svelte';
import type { Permission } from './lib/models';
import Home from './views/Home.svelte';
import CategoriesPage from './views/categories/CategoriesPage.svelte';
import CategoryPage from './views/categories/CategoryPage.svelte';
import FieldsPage from './views/fields/FieldsPage.svelte';

export interface Route {
  pattern: string;
  component: Component<{ params: Record<string, string> }>;
  title: string;
  /** Any one of these lets the user open the page; none means any user. */
  permissions?: Permission[];
  /** The label in the main navigation, if the page is listed there. */
  nav?: string;
}

export const routes: Route[] = [
  { pattern: '/', component: Home, title: 'Home' },
  { pattern: '/categories', component: CategoriesPage, title: 'Categories', permissions: ['assets:read'], nav: 'Categories' },
  { pattern: '/categories/new', component: CategoryPage, title: 'New category', permissions: ['categories:manage'] },
  { pattern: '/categories/:id', component: CategoryPage, title: 'Category', permissions: ['assets:read'] },
  { pattern: '/fields', component: FieldsPage, title: 'Fields', permissions: ['categories:manage'], nav: 'Fields' },
];
