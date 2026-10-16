// Types mirroring internal/models as the API returns them.

export type Permission =
  | 'assets:read'
  | 'assets:write'
  | 'assets:delete'
  | 'templates:manage'
  | 'categories:manage'
  | 'settings:manage'
  | 'users:manage'
  | 'labels:print'
  | 'stock:record'
  | 'assets:report'
  | 'assets:borrow'
  | 'assets:lend';

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
  user: User;
  permissions: Permission[];
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
  /** Its assets, and those of its subcategories, can be lent. */
  is_lendable: boolean;
  is_running_low: boolean;
  /** Flags the category when fewer active assets remain; null for none. */
  low_stock_threshold?: number | null;
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
  };
}

/** One change to an asset, as it read then; an absent from or to means none. */
export interface AssetChange {
  what: 'name' | 'category' | 'field' | 'code' | 'stock' | 'unit' | 'threshold' | 'condition' | 'link';
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
  loan: Loan | null;
  /** Its links to other assets, where it is (outermost first), and the relation types its categories suggest. */
  links: AssetLink[];
  location: LinkedAsset[];
  suggested_relation_type_ids: string[];
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
  permissions: Permission[];
  /** How many users who are not archived hold it. */
  user_count: number;
  /** Set for the admin role, which always has every permission. */
  protected: boolean;
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
}

export interface ImportResult {
  created: number;
  updated: number;
  unchanged: number;
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
