// The app's pages, in navigation order. Entries with a nav label appear in
// the main navigation for users holding one of their permissions, under
// their group if they have one (lib/nav.ts).

import type { Component } from 'svelte';
import type { Permission } from './lib/models';
import type { NavGroup } from './lib/nav';
import Home from './views/Home.svelte';
import AssetsPage from './views/assets/AssetsPage.svelte';
import AssetPage from './views/assets/AssetPage.svelte';
import ScenePage from './views/assets/ScenePage.svelte';
import GraphPage from './views/assets/GraphPage.svelte';
import AssetForm from './views/assets/AssetForm.svelte';
import ImportPage from './views/assets/ImportPage.svelte';
import CategoriesPage from './views/categories/CategoriesPage.svelte';
import CategoryPage from './views/categories/CategoryPage.svelte';
import VendorsPage from './views/vendors/VendorsPage.svelte';
import VendorPage from './views/vendors/VendorPage.svelte';
import ContractsPage from './views/contracts/ContractsPage.svelte';
import ContractPage from './views/contracts/ContractPage.svelte';
import OrdersPage from './views/orders/OrdersPage.svelte';
import OrderPage from './views/orders/OrderPage.svelte';
import ReceivePage from './views/orders/ReceivePage.svelte';
import FieldsPage from './views/fields/FieldsPage.svelte';
import TemplatesPage from './views/templates/TemplatesPage.svelte';
import TemplateEditor from './views/templates/TemplateEditor.svelte';
import ImagesPage from './views/admin/ImagesPage.svelte';
import PrintersPage from './views/admin/PrintersPage.svelte';
import DisplaysPage from './views/admin/DisplaysPage.svelte';
import LasersPage from './views/lasers/LasersPage.svelte';
import UsersPage from './views/admin/UsersPage.svelte';
import RolesPage from './views/admin/RolesPage.svelte';
import TokensPage from './views/admin/TokensPage.svelte';
import IncomingHooksPage from './views/admin/IncomingHooksPage.svelte';
import ReportsPage from './views/reports/ReportsPage.svelte';
import ReportView from './views/reports/ReportView.svelte';
import ReportEditor from './views/reports/ReportEditor.svelte';
import ScanTemplateEditor from './views/reports/ScanTemplateEditor.svelte';
import NotificationsPage from './views/admin/NotificationsPage.svelte';
import RelaysPage from './views/admin/RelaysPage.svelte';
import SignInPage from './views/admin/SignInPage.svelte';
import BackupsPage from './views/admin/BackupsPage.svelte';
import InstalledAppPage from './views/admin/InstalledAppPage.svelte';
import PackagesPage from './views/packages/PackagesPage.svelte';
import AccountPage from './views/AccountPage.svelte';
import OAuthConsentPage from './views/OAuthConsentPage.svelte';
import LoansPage from './views/LoansPage.svelte';
import ShoppingListPage from './views/ShoppingListPage.svelte';
import OfflineCopyPage from './views/OfflineCopyPage.svelte';
import SpoolPage from './views/SpoolPage.svelte';
import ScanPage from './views/ScanPage.svelte';
import ScanLookupPage from './views/ScanLookupPage.svelte';
import HelpPage from './views/HelpPage.svelte';
import RelationsPage from './views/RelationsPage.svelte';
import TermsPage from './views/TermsPage.svelte';
import HooksPage from './views/HooksPage.svelte';
import IconsPage from './views/icons/IconsPage.svelte';
import FlowsPage from './views/flows/FlowsPage.svelte';
import FlowRunPage from './views/flows/FlowRunPage.svelte';
import FlowEditor from './views/flows/FlowEditor.svelte';
import FlowPosterPage from './views/flows/FlowPosterPage.svelte';
import AssistantPage from './views/admin/AssistantPage.svelte';
import GuidePage from './views/assistant/GuidePage.svelte';
import GuidePosterPage from './views/assistant/GuidePosterPage.svelte';

export interface Route {
  pattern: string;
  component: Component<{ params: Record<string, string> }>;
  title: string;
  /** Any one of these lets the user open the page; none means any user. */
  permissions?: Permission[];
  /** The label in the main navigation, if the page is listed there. */
  nav?: string;
  /** The navigation group it is listed under; none puts it on its own. */
  group?: NavGroup;
  /** Signed-out visitors may open it too, when the public role allows (docs/access.md). */
  public?: boolean;
  /** A workspace, such as the template editor: the whole window below the header, and no footer. */
  workspace?: boolean;
  /** It works from the offline copy, opened with no connection (docs/installing.md#offline). */
  offline?: boolean;
}

export const routes: Route[] = [
  { pattern: '/', component: Home, title: 'Home' },
  { pattern: '/assets', component: AssetsPage, title: 'Assets', permissions: ['assets.read'], nav: 'Assets' },
  { pattern: '/assets/new', component: AssetForm, title: 'New asset', permissions: ['assets.write'] },
  { pattern: '/assets/import', component: ImportPage, title: 'Import assets', permissions: ['assets.write'] },
  { pattern: '/assets/graph', component: GraphPage, title: 'Graph', permissions: ['assets.read'] },
  { pattern: '/assets/:id', component: AssetPage, title: 'Asset', permissions: ['assets.read'], public: true },
  { pattern: '/scan', component: ScanLookupPage, title: 'Scan', permissions: ['assets.scan'], nav: 'Scan', public: true, offline: true },
  { pattern: '/scan/:code', component: ScanPage, title: 'Scan', permissions: ['assets.scan'], public: true, offline: true },
  { pattern: '/assets/:id/scene', component: ScenePage, title: 'Scene', permissions: ['assets.read'], public: true },
  { pattern: '/assets/:id/edit', component: AssetForm, title: 'Edit asset', permissions: ['assets.write'] },
  { pattern: '/flows', component: FlowsPage, title: 'Flows', permissions: ['flows.run', 'flows.manage'], nav: 'Flows' },
  { pattern: '/flows/new', component: FlowEditor, title: 'New flow', permissions: ['flows.manage'] },
  { pattern: '/flows/:slug/edit', component: FlowEditor, title: 'Edit flow', permissions: ['flows.manage'] },
  { pattern: '/flows/:slug/poster', component: FlowPosterPage, title: 'Flow poster', permissions: ['flows.manage', 'flows.run'] },
  { pattern: '/f/:slug', component: FlowRunPage, title: 'Flow', permissions: ['flows.run'] },
  { pattern: '/loans', component: LoansPage, title: 'Loans', permissions: ['assets.track'], nav: 'Loans' },
  // The installed app's Shopping list shortcut (docs/installing.md#shortcuts).
  { pattern: '/shopping', component: ShoppingListPage, title: 'Shopping list', permissions: ['assets.read'], offline: true },
  // The copy kept to use offline: finding things in it, and keeping it.
  { pattern: '/offline', component: OfflineCopyPage, title: 'Offline copy', permissions: ['assets.read'], offline: true },
  { pattern: '/spool', component: SpoolPage, title: 'Print queue', permissions: ['labels.print'], nav: 'Print queue' },
  { pattern: '/reports', component: ReportsPage, title: 'Reports', permissions: ['assets.read'], nav: 'Reports' },
  { pattern: '/reports/new', component: ReportEditor, title: 'New report', permissions: ['reports.manage'] },
  { pattern: '/reports/:id', component: ReportView, title: 'Report', permissions: ['assets.read'] },
  { pattern: '/reports/:id/edit', component: ReportEditor, title: 'Edit report', permissions: ['reports.manage'] },
  { pattern: '/scan-templates/new', component: ScanTemplateEditor, title: 'New scan template', permissions: ['reports.manage'] },
  { pattern: '/scan-templates/:id/edit', component: ScanTemplateEditor, title: 'Edit scan template', permissions: ['reports.manage'] },
  { pattern: '/categories', component: CategoriesPage, title: 'Categories', permissions: ['assets.read'], nav: 'Categories', group: 'Catalog' },
  { pattern: '/categories/new', component: CategoryPage, title: 'New category', permissions: ['categories.manage'] },
  { pattern: '/categories/:id', component: CategoryPage, title: 'Category', permissions: ['assets.read'] },
  { pattern: '/vendors', component: VendorsPage, title: 'Vendors', permissions: ['assets.read'], nav: 'Vendors', group: 'Catalog' },
  { pattern: '/vendors/new', component: VendorPage, title: 'New vendor', permissions: ['vendors.manage'] },
  { pattern: '/vendors/:id', component: VendorPage, title: 'Vendor', permissions: ['assets.read'] },
  { pattern: '/contracts', component: ContractsPage, title: 'Contracts', permissions: ['assets.read'], nav: 'Contracts', group: 'Catalog' },
  { pattern: '/contracts/new', component: ContractPage, title: 'New contract', permissions: ['vendors.manage'] },
  { pattern: '/contracts/:id', component: ContractPage, title: 'Contract', permissions: ['assets.read'] },
  { pattern: '/orders', component: OrdersPage, title: 'Orders', permissions: ['assets.read'], nav: 'Orders', group: 'Catalog' },
  { pattern: '/orders/new', component: OrderPage, title: 'New order', permissions: ['vendors.manage'] },
  { pattern: '/orders/:id', component: OrderPage, title: 'Order', permissions: ['assets.read'] },
  { pattern: '/orders/:id/receive', component: ReceivePage, title: 'Receive an order', permissions: ['assets.read'] },
  { pattern: '/fields', component: FieldsPage, title: 'Fields', permissions: ['categories.manage'], nav: 'Fields', group: 'Catalog' },
  { pattern: '/relations', component: RelationsPage, title: 'Link types', permissions: ['categories.manage'], nav: 'Link types', group: 'Catalog' },
  { pattern: '/terms', component: TermsPage, title: 'Search terms', permissions: ['terms.manage'], nav: 'Search terms', group: 'Catalog' },
  { pattern: '/hooks', component: HooksPage, title: 'Hooks', permissions: ['categories.manage'], nav: 'Hooks', group: 'Catalog' },
  { pattern: '/icons', component: IconsPage, title: 'Icons', permissions: ['categories.manage', 'icons.manage'], nav: 'Icons', group: 'Catalog' },
  { pattern: '/templates', component: TemplatesPage, title: 'Templates', permissions: ['templates.manage'], nav: 'Templates', group: 'Labels' },
  { pattern: '/templates/new', component: TemplateEditor, title: 'New template', permissions: ['templates.manage'], workspace: true },
  { pattern: '/templates/:id/edit', component: TemplateEditor, title: 'Edit template', permissions: ['templates.manage'], workspace: true },
  { pattern: '/images', component: ImagesPage, title: 'Images', permissions: ['templates.manage'], nav: 'Images', group: 'Labels' },
  { pattern: '/printers', component: PrintersPage, title: 'Printers', permissions: ['settings.manage', 'labels.print'], nav: 'Printers', group: 'Labels' },
  { pattern: '/displays', component: DisplaysPage, title: 'Displays', permissions: ['settings.manage', 'labels.print'], nav: 'Displays', group: 'Labels' },
  { pattern: '/lasers', component: LasersPage, title: 'Lasers', permissions: ['settings.manage', 'labels.print'], nav: 'Lasers', group: 'Labels' },
  { pattern: '/users', component: UsersPage, title: 'Users', permissions: ['users.manage'], nav: 'Users', group: 'Admin' },
  { pattern: '/roles', component: RolesPage, title: 'Roles', permissions: ['users.manage'], nav: 'Roles', group: 'Admin' },
  { pattern: '/tokens', component: TokensPage, title: 'API tokens', permissions: ['users.manage'], nav: 'API tokens', group: 'Admin' },
  { pattern: '/notifications', component: NotificationsPage, title: 'Notifications', permissions: ['settings.manage'], nav: 'Notifications', group: 'Admin' },
  { pattern: '/relays', component: RelaysPage, title: 'Relays', permissions: ['settings.manage'], nav: 'Relays', group: 'Admin' },
  { pattern: '/incoming-hooks', component: IncomingHooksPage, title: 'Incoming hooks', permissions: ['settings.manage'], nav: 'Incoming hooks', group: 'Admin' },
  { pattern: '/sign-in-settings', component: SignInPage, title: 'Sign-in', permissions: ['settings.manage'], nav: 'Sign-in', group: 'Admin' },
  { pattern: '/assistant-settings', component: AssistantPage, title: 'Assistant', permissions: ['settings.manage'], nav: 'Assistant', group: 'Admin' },
  { pattern: '/assistant/guides/:slug/poster', component: GuidePosterPage, title: 'Guide poster', permissions: ['settings.manage', 'assistant.use'] },
  { pattern: '/g/:slug', component: GuidePage, title: 'Guide', permissions: ['assistant.use', 'settings.manage'] },
  { pattern: '/backups', component: BackupsPage, title: 'Backups', permissions: ['settings.manage'], nav: 'Backups', group: 'Admin' },
  { pattern: '/installed-app', component: InstalledAppPage, title: 'Installed app', permissions: ['settings.manage'], nav: 'Installed app', group: 'Admin' },
  { pattern: '/packages', component: PackagesPage, title: 'Import & export', permissions: ['settings.manage'], nav: 'Import & export', group: 'Admin' },
  { pattern: '/account', component: AccountPage, title: 'Your account' },
  { pattern: '/oauth/consent', component: OAuthConsentPage, title: 'Connect an assistant' },
  // Help is for everyone, signed in or not (docs/architecture.md).
  { pattern: '/help', component: HelpPage, title: 'Help', public: true, offline: true },
  { pattern: '/help/:topic', component: HelpPage, title: 'Help', public: true, offline: true },
];

// Pages that moved, from the address they had to the one they have: only
// a bookmark is at stake, so the old address goes to the new one.
export const moved: Record<string, string> = {
  // Actions became hooks (#173).
  '/actions': '/hooks',
  '/help/actions': '/help/hooks',
};

/** Where a page that moved is now, or null for any other path. */
export function movedTo(path: string): string | null {
  return moved[path.length > 1 ? path.replace(/\/+$/, '') : path] ?? null;
}
