// The app's pages, in navigation order. Entries with a nav label appear in
// the main navigation for users holding one of their permissions.

import type { Component } from 'svelte';
import type { Permission } from './lib/models';
import Home from './views/Home.svelte';
import AssetsPage from './views/assets/AssetsPage.svelte';
import AssetPage from './views/assets/AssetPage.svelte';
import AssetForm from './views/assets/AssetForm.svelte';
import ImportPage from './views/assets/ImportPage.svelte';
import CategoriesPage from './views/categories/CategoriesPage.svelte';
import CategoryPage from './views/categories/CategoryPage.svelte';
import FieldsPage from './views/fields/FieldsPage.svelte';
import TemplatesPage from './views/templates/TemplatesPage.svelte';
import TemplateEditor from './views/templates/TemplateEditor.svelte';
import ImagesPage from './views/admin/ImagesPage.svelte';
import PrintersPage from './views/admin/PrintersPage.svelte';
import UsersPage from './views/admin/UsersPage.svelte';
import NotificationsPage from './views/admin/NotificationsPage.svelte';
import AccountPage from './views/AccountPage.svelte';

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
  { pattern: '/assets', component: AssetsPage, title: 'Assets', permissions: ['assets:read'], nav: 'Assets' },
  { pattern: '/assets/new', component: AssetForm, title: 'New asset', permissions: ['assets:write'] },
  { pattern: '/assets/import', component: ImportPage, title: 'Import assets', permissions: ['assets:write'] },
  { pattern: '/assets/:id', component: AssetPage, title: 'Asset', permissions: ['assets:read'] },
  { pattern: '/assets/:id/edit', component: AssetForm, title: 'Edit asset', permissions: ['assets:write'] },
  { pattern: '/categories', component: CategoriesPage, title: 'Categories', permissions: ['assets:read'], nav: 'Categories' },
  { pattern: '/categories/new', component: CategoryPage, title: 'New category', permissions: ['categories:manage'] },
  { pattern: '/categories/:id', component: CategoryPage, title: 'Category', permissions: ['assets:read'] },
  { pattern: '/fields', component: FieldsPage, title: 'Fields', permissions: ['categories:manage'], nav: 'Fields' },
  { pattern: '/templates', component: TemplatesPage, title: 'Templates', permissions: ['templates:manage'], nav: 'Templates' },
  { pattern: '/templates/new', component: TemplateEditor, title: 'New template', permissions: ['templates:manage'] },
  { pattern: '/templates/:id/edit', component: TemplateEditor, title: 'Edit template', permissions: ['templates:manage'] },
  { pattern: '/images', component: ImagesPage, title: 'Images', permissions: ['templates:manage'], nav: 'Images' },
  { pattern: '/printers', component: PrintersPage, title: 'Printers', permissions: ['settings:manage', 'labels:print'], nav: 'Printers' },
  { pattern: '/notifications', component: NotificationsPage, title: 'Notifications', permissions: ['settings:manage'], nav: 'Notifications' },
  { pattern: '/users', component: UsersPage, title: 'Users', permissions: ['users:manage'], nav: 'Users' },
  { pattern: '/account', component: AccountPage, title: 'Your account' },
];
