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
import RolesPage from './views/admin/RolesPage.svelte';
import TokensPage from './views/admin/TokensPage.svelte';
import ReportsPage from './views/reports/ReportsPage.svelte';
import ReportView from './views/reports/ReportView.svelte';
import ReportEditor from './views/reports/ReportEditor.svelte';
import ScanTemplateEditor from './views/reports/ScanTemplateEditor.svelte';
import NotificationsPage from './views/admin/NotificationsPage.svelte';
import AccountPage from './views/AccountPage.svelte';
import LoansPage from './views/LoansPage.svelte';
import SpoolPage from './views/SpoolPage.svelte';
import ScanPage from './views/ScanPage.svelte';
import HelpPage from './views/HelpPage.svelte';
import RelationsPage from './views/RelationsPage.svelte';
import ActionsPage from './views/ActionsPage.svelte';

export interface Route {
  pattern: string;
  component: Component<{ params: Record<string, string> }>;
  title: string;
  /** Any one of these lets the user open the page; none means any user. */
  permissions?: Permission[];
  /** The label in the main navigation, if the page is listed there. */
  nav?: string;
  /** Signed-out visitors may open it too, when the public role allows (README §3.3). */
  public?: boolean;
}

export const routes: Route[] = [
  { pattern: '/', component: Home, title: 'Home' },
  { pattern: '/assets', component: AssetsPage, title: 'Assets', permissions: ['assets.read'], nav: 'Assets' },
  { pattern: '/assets/new', component: AssetForm, title: 'New asset', permissions: ['assets.write'] },
  { pattern: '/assets/import', component: ImportPage, title: 'Import assets', permissions: ['assets.write'] },
  { pattern: '/assets/:id', component: AssetPage, title: 'Asset', permissions: ['assets.read'], public: true },
  { pattern: '/scan/:code', component: ScanPage, title: 'Scan', permissions: ['assets.scan'], public: true },
  { pattern: '/assets/:id/edit', component: AssetForm, title: 'Edit asset', permissions: ['assets.write'] },
  { pattern: '/reports', component: ReportsPage, title: 'Reports', permissions: ['assets.read'], nav: 'Reports' },
  { pattern: '/reports/new', component: ReportEditor, title: 'New report', permissions: ['reports.manage'] },
  { pattern: '/reports/:id', component: ReportView, title: 'Report', permissions: ['assets.read'] },
  { pattern: '/reports/:id/edit', component: ReportEditor, title: 'Edit report', permissions: ['reports.manage'] },
  { pattern: '/scan-templates/new', component: ScanTemplateEditor, title: 'New scan template', permissions: ['reports.manage'] },
  { pattern: '/scan-templates/:id/edit', component: ScanTemplateEditor, title: 'Edit scan template', permissions: ['reports.manage'] },
  { pattern: '/loans', component: LoansPage, title: 'Loans', permissions: ['assets.track'], nav: 'Loans' },
  { pattern: '/spool', component: SpoolPage, title: 'Spool', permissions: ['labels.print'], nav: 'Spool' },
  { pattern: '/categories', component: CategoriesPage, title: 'Categories', permissions: ['assets.read'], nav: 'Categories' },
  { pattern: '/categories/new', component: CategoryPage, title: 'New category', permissions: ['categories.manage'] },
  { pattern: '/categories/:id', component: CategoryPage, title: 'Category', permissions: ['assets.read'] },
  { pattern: '/fields', component: FieldsPage, title: 'Fields', permissions: ['categories.manage'], nav: 'Fields' },
  { pattern: '/relations', component: RelationsPage, title: 'Relations', permissions: ['categories.manage'], nav: 'Relations' },
  { pattern: '/actions', component: ActionsPage, title: 'Actions', permissions: ['categories.manage'], nav: 'Actions' },
  { pattern: '/templates', component: TemplatesPage, title: 'Templates', permissions: ['templates.manage'], nav: 'Templates' },
  { pattern: '/templates/new', component: TemplateEditor, title: 'New template', permissions: ['templates.manage'] },
  { pattern: '/templates/:id/edit', component: TemplateEditor, title: 'Edit template', permissions: ['templates.manage'] },
  { pattern: '/images', component: ImagesPage, title: 'Images', permissions: ['templates.manage'], nav: 'Images' },
  { pattern: '/printers', component: PrintersPage, title: 'Printers', permissions: ['settings.manage', 'labels.print'], nav: 'Printers' },
  { pattern: '/notifications', component: NotificationsPage, title: 'Notifications', permissions: ['settings.manage'], nav: 'Notifications' },
  { pattern: '/users', component: UsersPage, title: 'Users', permissions: ['users.manage'], nav: 'Users' },
  { pattern: '/roles', component: RolesPage, title: 'Roles', permissions: ['users.manage'], nav: 'Roles' },
  { pattern: '/tokens', component: TokensPage, title: 'API tokens', permissions: ['users.manage'], nav: 'API tokens' },
  { pattern: '/account', component: AccountPage, title: 'Your account' },
  // Help is for everyone, signed in or not (README §6.5).
  { pattern: '/help', component: HelpPage, title: 'Help', public: true },
  { pattern: '/help/:topic', component: HelpPage, title: 'Help', public: true },
];
