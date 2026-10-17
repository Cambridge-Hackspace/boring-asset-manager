// Types mirroring internal/models as the API returns them.

export type Permission =
  | 'assets.read'
  | 'assets.write'
  | 'assets.delete'
  | 'templates.manage'
  | 'categories.manage'
  | 'settings.manage'
  | 'users.manage'
  | 'labels.print'
  | 'stock.record'
  | 'assets.report'
  | 'assets.borrow'
  | 'assets.lend'
  | 'reports.manage'
  | 'assets.track'
  | 'assets.scan'
  | 'assets.act'
  | 'events.read';

/** A permission held in every scope, or in one as scope:permission (README §3.3). */
export type PermissionKey = Permission | `${string}:${Permission}`;

/** A label on categories that roles can hold asset permissions in. */
export interface Scope {
  id: string;
  name: string;
  created_at: string;
  /** The default scope, of assets in no other; it cannot be changed. */
  reserved: boolean;
  category_count: number;
  role_count: number;
}

export interface User {
  id: string;
  email: string;
  /** For display only; empty shows the email instead. */
  name: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  archived_at?: string | null;
}

export interface Me {
  /** Null for a signed-out visitor, who holds the public role's permissions. */
  user: User | null;
  permissions: PermissionKey[];
}

export const fieldTypes = ['text', 'number', 'date', 'url', 'select'] as const;
export type FieldType = (typeof fieldTypes)[number];

export interface SelectChoice {
  label: string;
  value: string;
}

export interface SelectOptions {
  choices: SelectChoice[];
  default?: string | null;
}

export interface Field {
  id: string;
  key: string;
  label: string;
  validation_type: FieldType;
  options?: SelectOptions | null;
  created_at: string;
  updated_at: string;
}

/** The system category assets fall back to; it stays outside the tree. */
export const UNCATEGORIZED = 'uncategorized';

export interface Category {
  id: string;
  name: string;
  parent_id?: string | null;
  color_hex: string;
  is_consumable: boolean;
  /** On a consumable category, totals its assets' stock in this unit instead of counting them; empty counts them. */
  unit: string;
  /** Its assets, and those of its subcategories, can be lent. */
  is_lendable: boolean;
  is_running_low: boolean;
  /** Flags the category when fewer active assets remain; null for none. */
  low_stock_threshold?: number | null;
  /** The scan template its assets, and its subcategories' that name none, show. */
  scan_template_id?: string | null;
  /** The label template its assets, and its subcategories' that name none, join the spool with. */
  label_template_id?: string | null;
  created_at: string;
  updated_at: string;
}

export interface EffectiveField {
  field: Field;
  is_required: boolean;
  /** The category the field is assigned to, itself or an ancestor. */
  source_category_id?: string;
}

export interface Asset {
  id: string;
  name: string;
  created_at: string;
  updated_at: string;
  archived_at?: string | null;
  /** Set when the asset's stock is counted by quantity (README §5.1). */
  stock?: AssetStock | null;
  /** How it is: working, needing maintenance or out of service, with a note saying what is wrong. */
  condition: Condition;
  condition_note: string;
}

export type Condition = 'ok' | 'needs_maintenance' | 'out_of_service';

export interface AssetStock {
  quantity: number;
  unit: string;
  /** Flags the asset when its quantity drops below it; null for none. */
  low_stock_threshold: number | null;
  is_running_low: boolean;
}

/** Something that happened to an asset, as its history lists it. */
export interface AssetEvent {
  id: string;
  kind: string;
  created_at: string;
  payload: {
    /** Who caused it, with their name as it was then. */
    actor?: { id: string; email: string; name?: string; token?: boolean };
    trigger: 'manual' | 'threshold' | 'import' | 'schedule';
    quantity?: number;
    change?: number;
    threshold?: number;
    unit?: string;
    /** What a created, edited or archived event changed. */
    changes?: AssetChange[];
    /** What someone said about a change of condition. */
    note?: string;
    /** Who has, or had, the asset of a loan event, and when it is due back. */
    borrower?: { id: string; email: string; name?: string };
    due_at?: string;
    /** What a label printed event printed: the template and printer as named then, or a download. */
    label?: { template: string; printer?: string; download?: boolean; copies: number };
    /** What an action event pressed, and the asset and the containers it is in. */
    action?: { key: string; label: string };
  };
}

/** One change to an asset, as it read then; an absent from or to means none. */
export interface AssetChange {
  what: 'name' | 'category' | 'field' | 'code' | 'stock' | 'unit' | 'threshold' | 'condition' | 'link' | 'photo';
  key?: string;
  label?: string;
  from?: string;
  to?: string;
}

export interface GeneratorKey {
  asset_id?: string;
  generator_type: string;
  assigned_code: string;
}

export interface AssetWithDetails {
  asset: Asset;
  categories: Category[];
  generator_keys: GeneratorKey[];
  /** Values by field key, including ones kept from earlier categories. */
  field_values: Record<string, string>;
  effective_fields: EffectiveField[];
  /** Whether it can be lent, and its open loan, if it is out. */
  lendable: boolean;
  /** Its stock is its weight in a unit one of its categories totals: a spool, not a pooled quantity. */
  weighed: boolean;
  loan: Loan | null;
  /** Its links to other assets, where it is (outermost first), and the relation types its categories suggest. */
  links: AssetLink[];
  location: LinkedAsset[];
  suggested_relation_type_ids: string[];
  /** The scopes it is in, from its categories and theirs, or default. */
  scopes: string[];
  scan_template: TemplateChoice;
  /** The label template the spool queues it with; template_id is empty when no category names one. */
  label_template: TemplateChoice;
  /** The actions its categories offer on it, such as Identify (README §5.4). */
  actions: OfferedAction[];
  /** Its photos, in order; the first is the one lists show. */
  photos: AssetPhoto[];
}

export interface GeneratorInfo {
  type: string;
  name: string;
  error_correction_levels: string[];
  code_rules: string;
}

/** The parts of a label template lists and pickers need. */
export interface TemplateSummary {
  id: string;
  name: string;
  units: 'mm' | 'in';
  width: number;
  height: number;
  dpi: number;
}

export interface PrinterConnection {
  type: 'tcp' | 'device' | 'ipp' | 'windows';
  address: string;
}

export interface Printer {
  id: string;
  name: string;
  driver: string;
  model: string;
  connection: PrinterConnection;
  media: string;
  options?: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
}

export interface DriverOption {
  name: string;
  type: 'bool' | 'string' | 'int';
  default: unknown;
  values?: string[];
  description: string;
}

export interface DriverMedia {
  id: string;
  name: string;
  width_mm: number;
  length_mm?: number;
  die_cut: boolean;
}

export interface DriverInfo {
  driver: string;
  models: string[];
  model_dpi?: Record<string, number>;
  media: DriverMedia[];
  options?: DriverOption[];
}

export interface Role {
  id: string;
  name: string;
  description: string;
  permissions: PermissionKey[];
  /** How many users who are not archived hold it. */
  user_count: number;
  /** Set for the admin role, which always has every permission. */
  protected: boolean;
  /** Set for the public role, which everyone holds, signed in or not; it may only read. */
  public: boolean;
}

export interface UserWithRoles extends User {
  role_ids: string[];
}

export interface ImageInfo {
  id: string;
  filename: string;
  content_type: string;
  width_px: number;
  height_px: number;
  byte_size: number;
  created_at: string;
}

/** A column of an import file, with where it goes. */
export interface ImportColumn {
  header: string;
  suggested: string;
  target: string;
  sample: string;
}

/** Something wrong with an import, at a line (the header is line 1). */
export interface ImportProblem {
  row: number;
  column?: string;
  message: string;
}

/** What an import would do (README §6.1). */
export interface ImportPlan {
  columns: ImportColumn[];
  rows: number;
  create: number;
  update: number;
  unchanged: number;
  problems: ImportProblem[];
  problem_count: number;
  unknown_categories: string[];
  /** The links it adds, moves and removes, counted, with the first of them listed. */
  links_added: number;
  links_moved: number;
  links_removed: number;
  link_changes: LinkChange[];
  remove_links: boolean;
}

/** A link an import would change, as "Screws contained in Bin A3". */
export interface LinkChange {
  row?: number;
  change: 'add' | 'move' | 'remove';
  asset: string;
  label: string;
  other: string;
}

export interface ImportResult {
  created: number;
  updated: number;
  unchanged: number;
  links_added: number;
  links_moved: number;
  links_removed: number;
  created_ids: string[];
}

/** An option of a notifier driver; every option is a string. */
export interface NotifierOption {
  name: string;
  label: string;
  description: string;
  required: boolean;
  /** Secret options can be written but are never returned. */
  secret: boolean;
}

export interface NotifierDriver {
  driver: string;
  label: string;
  options: NotifierOption[];
}

export interface EventKind {
  kind: string;
  label: string;
}

/** A notification channel (README §5.2). */
export interface Channel {
  id: string;
  name: string;
  driver: string;
  /** Options without secret values. */
  options: Record<string, string>;
  /** Which secret options have a value. */
  secrets_set: Record<string, boolean>;
  enabled: boolean;
  events: string[];
  created_at: string;
  updated_at: string;
}

export interface Delivery {
  id: string;
  event_id: string;
  channel_id: string;
  status: 'pending' | 'sent' | 'failed';
  attempts: number;
  next_attempt_at: string;
  last_error?: string;
  created_at: string;
  finished_at?: string | null;
  channel_name: string;
  event_kind: string;
  category_name?: string;
  asset_name?: string;
}

/** An API token for scripts and dashboards; its secret is shown only once. */
export interface APIToken {
  id: string;
  name: string;
  role_ids: string[];
  created_by?: string;
  created_at: string;
  expires_at: string | null;
  last_used_at: string | null;
  revoked_at: string | null;
}

/** A user as others see them, in loans and pickers. */
export interface Person {
  id: string;
  email: string;
  name: string;
  /** Set for someone who has left; their loans stay theirs. */
  archived?: boolean;
}

/** An asset lent to someone; open until checked in. */
export interface Loan {
  id: string;
  asset_id: string;
  asset_name: string;
  borrower: Person;
  checked_out_at: string;
  due_at: string | null;
  checked_in_at: string | null;
  overdue: boolean;
}

/** A kind of link between assets, named from each end. */
export interface RelationType {
  id: string;
  name: string;
  inverse_name: string;
  /** Reads the same both ways, as "used with" does. */
  is_symmetric: boolean;
  /** The far end can be linked once only, so linking it again moves it. */
  is_single: boolean;
  /** Its links make an asset's location. */
  is_location: boolean;
  created_at: string;
  updated_at: string;
  link_count: number;
}

/** The asset at the other end of a link, or in a location. */
export interface LinkedAsset {
  id: string;
  name: string;
  archived?: boolean;
}

/** A link as one of its assets sees it. */
export interface AssetLink {
  id: string;
  type_id: string;
  /** The relation's name read from this asset: "contains" or "contained in". */
  label: string;
  asset: LinkedAsset;
  outgoing: boolean;
}

/** A saved report: a filter over assets and sections showing them. */
export interface Report {
  id: string;
  name: string;
  /** A report, or a scan template: what scanning an asset shows (README §6.4). */
  kind: 'report' | 'scan';
  /** The scan template assets show when no category names one. */
  default: boolean;
  spec: ReportSpec;
  created_at: string;
  updated_at: string;
}

export interface ReportSpec {
  filter: ReportFilter;
  sections: ReportSection[];
}

export interface ReportFilter {
  category_ids?: string[];
  search?: string;
  archived?: boolean;
  running_low?: boolean;
  loan?: '' | 'open' | 'overdue';
  condition?: '' | 'problem' | Condition;
}

export interface ReportSection {
  /** details and actions are a scan template's. */
  type: 'table' | 'chart' | 'details' | 'actions';
  title?: string;
  /** A table's columns, sorting and grouping. */
  columns: { heading: string; binding: string }[];
  sort_by?: number;
  descending?: boolean;
  /** For a table '' or 'category'; for a bar or donut chart what its bars group by: category, condition, borrower, location, unit or field:<key>. */
  group_by?: string;
  chart?: 'bar' | 'donut' | 'line';
  /** assets, quantity or loans; for a line chart used, restocked or loans. */
  measure?: string;
  period?: '' | 'week' | 'month';
  /** In a scan template, a table's rows: contents, what the scanned asset holds. */
  source?: 'contents';
  /** An actions section's quick actions. */
  actions?: ScanAction[];
}

export const scanActions = ['open', 'edit', 'use', 'borrow', 'return', 'report', 'actions'] as const;
export type ScanAction = (typeof scanActions)[number];

/** The scan or label template an asset uses, and why: the closest category naming one, or for scan templates the default. */
export interface TemplateChoice {
  template_id: string;
  source_category_id?: string;
  /** Set when equally close categories named different templates, and the tie was broken by depth, size or age. */
  conflict?: boolean;
  contenders?: string[];
}

export interface ChartSeries {
  label: string;
  points: { label: string; value: number }[];
}

/** A report run against the assets its filter found. */
export interface ReportResult {
  assets: number;
  sections: {
    type: 'table' | 'chart' | 'details' | 'actions';
    title: string;
    actions?: ScanAction[];
    columns?: string[];
    groups?: { title: string; running_low?: boolean; rows: { asset_id: string; cells: string[] }[] }[];
    chart?: 'bar' | 'donut' | 'line';
    measure?: string;
    series?: ChartSeries[];
  }[];
}

/** A photo of an asset. */
export interface AssetPhoto {
  id: string;
  filename: string;
  content_type: string;
  width_px: number;
  height_px: number;
  byte_size: number;
  created_at: string;
}

/** Where a photo of an asset, or its thumbnail, is served. */
export function photoUrl(assetId: string, photoId: string, thumb = false): string {
  return `/api/v1/assets/${encodeURIComponent(assetId)}/photos/${encodeURIComponent(photoId)}${thumb ? '?size=thumb' : ''}`;
}

/** Where a spool entry prints: a printer, a download, or nowhere yet (README §6.2). */
export type SpoolDestination = `printer:${string}` | `pool:${string}` | 'download' | '';

/** Someone who added or printed a spool entry, by name. */
export interface SpoolActor {
  id: string;
  name?: string;
  token?: boolean;
}

/** One asset's label in the spool: waiting, or printed once printed_at is set. */
export interface SpoolEntry {
  id: string;
  asset_id: string;
  asset_name: string;
  asset_archived: boolean;
  template_id: string | null;
  destination: SpoolDestination;
  copies: number;
  /** Why its label could not be produced last time. */
  problem?: string;
  added_by: SpoolActor;
  added_at: string;
  printed_at?: string;
  printed_by?: SpoolActor;
  printed_template?: string;
  printed_on?: string;
  /** What keeps a waiting entry from printing as it stands, and why it could not be produced last time. */
  problems: string[];
  /** It can be printed as it stands: nothing blocks it and no job is printing it. */
  ready?: boolean;
  /** The job printing it, while that waits or sends; it cannot be changed meanwhile. */
  job?: { id: string; state: SpoolJobState; target: string };
}

/** The spool, with the templates and printers its entries can take and the printers each template suits. */
export interface Spool {
  entries: SpoolEntry[];
  templates: { id: string; name: string }[];
  printers: { id: string; name: string }[];
  pools: { id: string; name: string }[];
  fits: Record<string, string[]>;
  /** For each template, the pools with a printer its labels suit. */
  pool_fits: Record<string, string[]>;
}

/** What adding to the spool did. */
export interface SpoolAddResult {
  added: SpoolEntry[];
  already_waiting: string[];
  skipped: { asset_id: string; reason: string }[];
}

export type SpoolJobState = 'waiting' | 'sending' | 'done' | 'failed' | 'canceled';

/** A printing of spool entries on one printer with one template, or a download of them. */
export interface SpoolJob {
  id: string;
  template_id: string | null;
  template_name: string;
  printer_id: string | null;
  /** The pool it went to, if any, and how each of its printers is getting on. */
  pool_id?: string;
  members?: { printer_id: string; printer: string; printed: number; sending: boolean; dropped: boolean }[];
  /** The printer or pool as it was named, or "Download". */
  target: string;
  download: boolean;
  state: SpoolJobState;
  error?: string;
  /** Every copy, and those printed so far. */
  labels: number;
  printed: number;
  entries: number;
  created_by: SpoolActor;
  created_at: string;
  started_at?: string;
  finished_at?: string;
}

/** What printing the spool did: the jobs made, and the entries left out with why. */
export interface SpoolPrintResult {
  jobs: SpoolJob[];
  skipped: { entry_id: string; asset_name: string; reason: string }[];
  /** Such as a pool's printer sitting a job out. */
  warnings?: string[];
}

/** Printers loaded alike that share print jobs: each job is cut into batches of batch_size labels (README §6.2). */
export interface PrinterPool {
  id: string;
  name: string;
  batch_size: number;
  /** Every member takes a fixed share of the batches, rather than those on direct connections taking the next as they free up. */
  fixed_shares: boolean;
  members: { printer_id: string; share: number }[];
  created_at: string;
  updated_at: string;
}

/** A button people press on assets, such as Identify, recording an asset.action event (README §5.4). */
export interface AssetAction {
  id: string;
  key: string;
  label: string;
  /** Asked before it is taken, when set. */
  confirm: string;
  created_at: string;
  updated_at: string;
}

/** An action a category offers on its assets, and with cascades on its subcategories'. */
export interface CategoryAction {
  action_id: string;
  cascades: boolean;
}

/** An action an asset's categories offer on it. */
export interface OfferedAction {
  id: string;
  key: string;
  label: string;
  confirm?: string;
}

/** A live event connection a server holds open, for an API token or a signed-in user (README §5.4). */
export interface LiveConnection {
  id: string;
  token_id?: string;
  user_id?: string;
  remote: string;
  opened_at: string;
  seen_at: string;
}

/** A device's answer to an action pressed: done or failed, with a message. */
export interface ActionReply {
  id: string;
  event_id: string;
  name: string;
  token: boolean;
  ok: boolean;
  message: string;
  created_at: string;
}
