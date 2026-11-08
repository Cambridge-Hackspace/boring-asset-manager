// Types mirroring internal/models as the API returns them.

import type { IrIcon } from './isolex/ir';

export type Permission =
  | 'assets.read'
  | 'assets.write'
  | 'assets.delete'
  | 'templates.manage'
  | 'categories.manage'
  | 'icons.manage'
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
  | 'assets.hook'
  | 'events.read'
  | 'flows.manage'
  | 'flows.run'
  | 'vendors.manage'
  | 'comments.write'
  | 'comments.moderate'
  | 'purchases.read';

/** A permission held in every scope, or in one as scope:permission (docs/access.md). */
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
  /** How many roles of users and API tokens are narrowed to it. */
  assignment_count: number;
  /** How many vendors it is on (docs/vendors.md). */
  vendor_count?: number;
}

export interface User {
  id: string;
  email: string;
  /** For display only; empty shows the email instead. */
  name: string;
  /** Light or dark, or the system's (#168). */
  theme?: 'system' | 'light' | 'dark';
  /** Whether email notification channels send to a role they hold reaches them (docs/notifications.md). */
  email_notifications?: boolean;
  /** Whether they are emailed about their loans, when the instance sends loan reminders. */
  loan_reminders?: boolean;
  /** Whether they are emailed when a comment mentions them. */
  mention_emails?: boolean;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  archived_at?: string | null;
}

export interface Me {
  /** Null for a signed-out visitor, who holds the public role's permissions. */
  user: User | null;
  permissions: PermissionKey[];
  /** The names of the roles the user holds. */
  roles?: string[];
  /** The names of the scopes each role narrowed to some holds, by role name. */
  role_scopes?: Record<string, string[]>;
  /** A role they hold requires a second factor and they have none: they must set one up first. */
  factor_setup_required?: boolean;
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
  /** The label template its assets, and its subcategories' that name none, join the print queue with. */
  label_template_id?: string | null;
  /** Its label templates, in order of preference; the first is label_template_id. */
  label_template_ids?: string[];
  /** The vendors it is bought from, in order of preference: those the requester may see (docs/vendors.md). */
  vendor_ids?: string[];
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
  /** Set when the asset's stock is counted by quantity (docs/stock-and-lending.md). */
  stock?: AssetStock | null;
  /** How it is: working, needing maintenance or out of service, with a note saying what is wrong. */
  condition: Condition;
  condition_note: string;
  /** Its own icon (docs/icons.md), drawn in place of its categories'; null when it has none. Its screen icon. */
  icon_name?: string | null;
  /** Its own label icon, drawn on its labels in place of its categories'; null when it has none. */
  label_icon_name?: string | null;
}

/**
 * The form an icon takes for where it is drawn (docs/icons.md): screen,
 * everywhere on screen, or label, on labels. A category, and an asset, has
 * an icon for each; with no label icon, labels draw the screen icon.
 */
export type Rendition = 'screen' | 'label';

/**
 * The icon an asset is drawn with (docs/icons.md): its own, or that of the
 * nearest category with one, source_category_id, named
 * source_category_name. missing is set when the
 * build draws no icon by the name, so the unknown icon is drawn; color is
 * what the category token paints in.
 */
export interface ResolvedIcon {
  name: string;
  from: 'asset' | 'category';
  /** Whose icon it is: the rendition asked for, or screen when there is none for it. */
  rendition?: Rendition;
  source_category_id?: string;
  source_category_name?: string;
  missing?: boolean;
  color?: string;
  /** The icon as it is named, set whenever name is not it: drawn with its params, or in a state. */
  base?: string;
  /** The icon drawn with params set to the asset's fields, and those params. */
  bound?: string;
  params?: Record<string, number>;
  state?: IconState;
  variant?: string;
  /** The asset's own texts its icon's text params are set to, by param: its name, a code, a field's value. */
  texts?: Record<string, string>;
  /** The icon drawn with texts, by the name it is drawn under; one it lacks is drawn as the build draws it. */
  drawn?: Record<string, IrIcon>;
}

/** A param of a category's icon bound to what an asset has (docs/icons.md): a field's value, or for a text param its name or first code. */
export interface IconParamBinding {
  param: string;
  source: 'field' | 'name' | 'code';
  field_id: string;
  field_key: string;
  field_label: string;
}

/** The states an asset's icon can show, worst first (docs/icons.md). */
export type IconState = 'out_of_service' | 'needs_maintenance' | 'overdue' | 'on_loan' | 'out_of_stock' | 'low_stock';

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
    actor?: { id: string; email: string; name?: string; token?: boolean; incoming?: boolean };
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
    /** What a hook event triggered, and the asset and the containers it is in. */
    hook?: { key: string; label: string; trigger?: 'button' | 'event' | 'schedule'; cause?: { id: string; kind: string } };
    /** The comment a comment event is about: what it is on, and what it said when written (docs/comments.md). */
    comment?: { id: string; on: CommentOn; loan_id?: string; flow?: { slug: string; name: string }; text?: string };
  };
}

/** One change to an asset, as it read then; an absent from or to means none. */
export interface AssetChange {
  what:
    | 'name'
    | 'icon'
    | 'category'
    | 'field'
    | 'code'
    | 'stock'
    | 'unit'
    | 'threshold'
    | 'condition'
    | 'link'
    | 'photo'
    | 'attachment'
    | 'place'
    | 'comment'
    // Purchase details (docs/data-model.md#purchases).
    | 'vendor'
    | 'order_date'
    | 'received_date'
    | 'price'
    | 'order_number'
    | 'warranty_end'
    | 'invoice';
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
  /** How many active assets are in it, through location links, directly: what its scene shows. */
  holds?: number;
  /** The scopes it is in, from its categories and theirs, or default. */
  scopes: string[];
  scan_template: TemplateChoice;
  /** The label template the spool queues it with; template_id is empty when no category names one. */
  label_template: TemplateChoice;
  /** The hooks its categories offer on it, such as Identify (docs/hooks.md). */
  hooks: OfferedHook[];
  /** Its photos, in order; the first is the one lists show. */
  photos: AssetPhoto[];
  /** The icon it is drawn with, and why; null when neither it nor its categories have one. */
  resolved_icon?: ResolvedIcon | null;
  /** The icon it is drawn with for each rendition it has one for, resolved_icon among them. */
  resolved_icons?: Partial<Record<Rendition, ResolvedIcon>>;
  /** Where it came from (docs/data-model.md#purchases); empty when nothing is recorded. */
  purchase?: Purchase;
  /** The contracts covering it, directly or through a category (docs/contracts.md), by when they end. */
  contracts?: ContractCover[];
}

/**
 * Where an asset came from. Dates are YYYY-MM-DD, empty when not known.
 * The price, currency, order number and invoice are left out for someone
 * without purchases.read for the asset, and prices_hidden says so; the
 * vendor is left out when they may not see it.
 */
export interface Purchase {
  vendor_id: string;
  vendor_name: string;
  order_date: string;
  received_date: string;
  warranty_end: string;
  /** A decimal with as many places as its currency has, such as 12.50. */
  price?: string;
  /** Its ISO 4217 code, such as GBP. */
  currency?: string;
  order_number?: string;
  invoice_attachment_id?: string;
  invoice_name?: string;
  prices_hidden?: boolean;
}

/** Purchase details as the asset form sends them: every one, so an absent one is emptied. */
export interface PurchaseRequest {
  vendor_id: string;
  order_date: string;
  received_date: string;
  warranty_end: string;
  price: string;
  currency: string;
  order_number: string;
  invoice_attachment_id: string;
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
  type: 'tcp' | 'device' | 'ipp' | 'windows' | 'ble';
  /** For ble, the Bluetooth address and any service and characteristics as a query (lib/bluetooth.ts). */
  address: string;
  /** The relay the printer is reached through, if any (docs/relays.md). */
  relay?: string;
}

export interface Printer {
  id: string;
  name: string;
  driver: string;
  model: string;
  connection: PrinterConnection;
  /** A built-in roll's ID, or custom stock's. */
  media: string;
  /** The custom stock media names, if it names any. */
  stock?: LabelStockSize;
  /** What media names, built in or custom. */
  loaded?: DriverMedia;
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
  /** Set for custom stock. */
  custom?: boolean;
  /** The models that take the roll, when not all of the driver's do. */
  models?: string[];
}

/** What changing a printer would leave without a printer, said before it is saved. */
export interface PrinterChanges {
  /** Labels it would leave where they no longer fit; those with move_to would take that template of their categories' instead. */
  stranded: { template_id: string; template_name: string; destination: string; target: string; labels: number; move_to?: string; move_to_name?: string }[];
  categories: { id: string; name: string }[];
  pools: { id: string; name: string }[];
}

/** What custom stock a driver takes. */
export interface CustomStockInfo {
  /** The widest label each model takes. */
  max_width_mm: Record<string, number>;
  /** Labels from a continuous roll are this long. */
  min_length_mm: number;
  max_length_mm: number;
  note?: string;
}

/** A roll known by its size alone. A length of 0 is a continuous roll. */
export interface LabelStockSize {
  id: string;
  name: string;
  width_mm: number;
  length_mm: number;
  gap_mm: number;
}

export interface LabelStock extends LabelStockSize {
  /** The printers with it loaded, by name. */
  printers: string[];
  created_at: string;
  updated_at: string;
}

export interface DriverInfo {
  driver: string;
  models: string[];
  model_dpi?: Record<string, number>;
  media: DriverMedia[];
  /** Set when the driver takes custom stock too. */
  custom_stock?: CustomStockInfo;
  options?: DriverOption[];
  /** Set when the driver's printers connect over Bluetooth LE: the default service and characteristics. */
  ble?: BLEProfile;
}

/** How a family of printers takes jobs over Bluetooth LE (docs/printing.md#bluetooth). */
export interface BLEProfile {
  service: string;
  write: string;
  notify?: string;
  /** Each write waits for the printer to acknowledge it. */
  ack?: boolean;
}

export interface Role {
  id: string;
  name: string;
  description: string;
  permissions: PermissionKey[];
  /** How many users who are not archived hold it. */
  user_count: number;
  /** Whoever holds it must sign in with a second factor. */
  requires_two_factor: boolean;
  /** Set for the admin role, which always has every permission. */
  protected: boolean;
  /** Set for the public role, which everyone holds, signed in or not; it may only read. */
  public: boolean;
}

/** A role as a user or token holds it: in every scope, or, when scope_ids names any, narrowed to those (docs/access.md). */
export interface RoleAssignment {
  role_id: string;
  scope_ids: string[];
}

export interface UserWithRoles extends User {
  role_ids: string[];
  roles: RoleAssignment[];
  /** Set while too many wrong passwords hold their sign-ins back (docs/access.md). */
  sign_in_delayed_until?: string;
  /** Whether they have a second factor, such as an authenticator app (docs/access.md). */
  two_factor?: boolean;
}

/** A second factor of one's own: never its secret. */
export interface Factor {
  id: string;
  kind: 'totp' | 'passkey';
  /** What a passkey's owner called it. */
  name?: string;
  /** The host a passkey was registered for, and can be used at alone. */
  rp_id?: string;
  created_at: string;
  confirmed_at?: string;
  last_used_at?: string;
}

/** A rule giving a role, optionally narrowed, to whoever's claim holds a value (docs/access.md). */
export interface ProviderRule {
  id?: string;
  claim: string;
  value: string;
  role_id: string;
  scope_ids: string[];
}

/** A protocol's setting, shown in a provider's form; a secret one is written but never shown. */
export interface ProtocolField {
  name: string;
  label: string;
  help?: string;
  required: boolean;
  secret: boolean;
  /** Long enough to want several lines, such as pasted XML. */
  multiline?: boolean;
}

export interface Protocol {
  name: string;
  label: string;
  fields: ProtocolField[];
  /** It makes keys of its own, which can be made again. */
  renewable?: boolean;
}

/** An identity provider people sign in through (docs/access.md#signing-in-with-an-identity-provider). */
export interface Provider {
  id: string;
  key: string;
  name: string;
  protocol: string;
  settings: Record<string, string>;
  secrets_set: Record<string, boolean>;
  secrets_unreadable?: boolean;
  callback_url: string;
  /** Where BAM!'s own metadata for it is, for SAML, for the identity provider to read. */
  sp_metadata_url?: string;
  button_label: string;
  icon: string;
  enabled: boolean;
  position: number;
  provision: boolean;
  default_role_ids: string[];
  allowed_domains: string[];
  allowed_groups: string[];
  link_by_email: boolean;
  trust_email: boolean;
  /** The amr values that make its sign-in count as two-factor. */
  mfa_amr: string[];
  /** The acr values, or SAML AuthnContextClassRef, that do. */
  mfa_acr: string[];
  /** Ask for BAM!'s own second factor when its sign-in does not count, rather than refusing. */
  mfa_fallback: boolean;
  /** Always ask for BAM!'s own second factor, whatever it says. */
  mfa_always_local: boolean;
  from_config: boolean;
  rules: ProviderRule[];
}

/** The amr values that count as two-factor unless a provider says otherwise. */
export const DEFAULT_MFA_AMR = ['mfa', 'otp', 'hwk', 'swk', 'fpt'];

/** The sign-in page's buttons, and whether it shows the password form. */
export interface PublicProviders {
  providers: { key: string; label: string; icon?: string }[];
  local_passwords: boolean;
  /** Forgot password is offered: a link emailed to set a new one. */
  password_reset?: boolean;
  /** A lost second factor can be removed by a link emailed after the password. */
  factor_recovery_email?: boolean;
}

/** The settings of account emails (docs/access.md#account-emails). */
export interface AccountEmailSettings {
  password_reset: boolean;
  factor_recovery_email: boolean;
  factor_recovery_email_required: boolean;
  /** Whether links can be emailed at all: email and the server's base URL set up. */
  can_email_links: boolean;
}

/** Someone's identity at a provider. */
export interface Identity {
  id: string;
  user_id: string;
  provider_id: string;
  provider_key: string;
  provider_name: string;
  subject: string;
  email: string;
  created_at: string;
  last_used_at?: string;
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
  /** A link's reference that names no asset, which an earlier file may create. */
  ref?: string;
}

/** What an import would do (docs/csv.md). */
export interface ImportPlan {
  columns: ImportColumn[];
  rows: number;
  create: number;
  update: number;
  unchanged: number;
  problems: ImportProblem[];
  problem_count: number;
  /** How many of the problems are links naming no asset. */
  unresolved: number;
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
  /** Takes several lines, such as a PEM certificate. */
  multiline?: boolean;
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

/** A notification channel (docs/notifications.md). */
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
  /** Failed attempts since it last delivered (docs/notifications.md). */
  failures?: number;
  failing_since?: string;
  /** Why BAM! turned it off, when it did, for failing. */
  disabled_reason?: string;
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
  /** What the receiver last answered, when its driver says: a webhook's HTTP status and the start of its body. */
  response_status?: number;
  response_body?: string;
}

/** A relay: BAM! run beside printers on a local network, linked to this one (docs/relays.md). */
export interface Relay {
  id: string;
  name: string;
  /** Holds a certificate and credential; until then, code_expires_at says when its code lapses. */
  paired: boolean;
  code_expires_at?: string | null;
  paired_at?: string | null;
  cert_expires_at?: string | null;
  /** Revoked relays are listed until their certificate expires, holding their name. */
  revoked_at?: string | null;
  online: boolean;
  /** Since when it is online, or offline. */
  status_at?: string | null;
  version: string;
  address: string;
  allowlist: string[];
  /** The transports it is linked to this server over now. */
  transports: string[];
  /** The printers reached through it, with their last job. */
  printers: RelayPrinter[];
  created_at: string;
}

/** A printer reached through a relay, with how its last print job went. */
export interface RelayPrinter {
  id: string;
  name: string;
  last_job?: { state: string; error?: string; at: string };
}

/** The relays page's list: the relays, the mesh root, and which transports are on. */
export interface RelayList {
  relays: Relay[];
  mesh: { name: string; created_at: string } | null;
  bonemesh: boolean;
  websocket: boolean;
  secret_key: boolean;
  url: string;
}

/** A relay just added, or given a new code: the code, shown once, and the command that pairs it. */
export interface RelayCode {
  relay: Relay;
  code: string;
  expires_at: string;
  command: string;
}

/** An API token for scripts and dashboards; its secret is shown only once. */
/** What an incoming hook does (docs/hooks.md#incoming-hooks). */
export type IncomingAction = 'hook' | 'quantity' | 'fields' | 'flow';

/** How an incoming hook finds its asset: fixed, or from the request; '' for a flow that finds none. */
export interface IncomingAsset {
  by: 'fixed' | 'id' | 'code' | 'field' | '';
  asset_id?: string;
  path?: string;
  field?: string;
}

/** A URL that lets something outside BAM! act on it. Its secret is shown once, when made or replaced. */
export interface IncomingHook {
  id: string;
  name: string;
  enabled: boolean;
  auth: 'bearer' | 'hmac';
  /** False after a restore without secrets: it refuses until given a new one. */
  has_secret: boolean;
  action: IncomingAction;
  hook?: string;
  flow_id?: string;
  /** Fields' keys to paths into the request's JSON; none sets the request's field_values. */
  fields?: Record<string, string>;
  asset: IncomingAsset;
  rate_limit: number;
  roles: RoleAssignment[];
  created_at: string;
  updated_at: string;
  last_used_at: string | null;
  url: string;
}

export interface APIToken {
  id: string;
  name: string;
  role_ids: string[];
  /** Its roles, with the scopes each is narrowed to. */
  roles: RoleAssignment[];
  /** Set on a personal token, which acts as this user, for an assistant. */
  user_id?: string;
  user_name?: string;
  /** The assistant a grant signed in through OAuth went to. */
  oauth_client?: string;
  refresh_expires_at?: string | null;
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
  /** A report, or a scan template: what scanning an asset shows (docs/scanning.md). */
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
  /** Assets bought from this vendor, by ID. */
  vendor?: string;
  /** Assets ordered (or, with no order date, received) within these dates, YYYY-MM-DD, either open. */
  purchased_between?: { from?: string; to?: string };
  /** Assets whose warranty ends before this date, YYYY-MM-DD. */
  warranty_ends_before?: string;
  /** Assets a contract covers, by ID (docs/contracts.md); its fields bind as {{asset.contract.*}}. */
  contract?: string;
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

export const scanActions = ['open', 'edit', 'use', 'borrow', 'return', 'report', 'hooks', 'add_similar', 'add_inside', 'flows'] as const;
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

/** What attachments belong to (docs/data-model.md#attachments). */
export type AttachmentOwner = 'asset' | 'category' | 'vendor' | 'contract';

/** Where in the API each owner's attachments are. */
export const attachmentBase: Record<AttachmentOwner, string> = { asset: 'assets', category: 'categories', vendor: 'vendors', contract: 'contracts' };

/** A file attached to an asset, a category or a vendor: an image (an asset's photo), a document, or another file the instance allows. */
export interface Attachment {
  id: string;
  kind: 'image' | 'document' | 'other';
  name: string;
  description: string;
  filename: string;
  content_type: string;
  byte_size: number;
  width_px?: number;
  height_px?: number;
  /** Who added it, to those who may see it. */
  added_by?: string;
  added_by_name?: string;
  created_at: string;
}

/** The instance's limits on attachments. */
export interface AttachmentLimits {
  max_file_bytes: number;
  max_image_bytes: number;
  max_per_object: number;
  allowed_types: string[];
}

/** Where an attachment of an asset, a category or a vendor, or an image's thumbnail, is served. */
export function attachmentUrl(owner: AttachmentOwner, ownerId: string, attachmentId: string, thumb = false): string {
  return `/api/v1/${attachmentBase[owner]}/${encodeURIComponent(ownerId)}/attachments/${encodeURIComponent(attachmentId)}${thumb ? '?size=thumb' : ''}`;
}

/** An asset's image attachment, as its photos list it. */
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

/** Where a spool entry prints: a printer, a download, or nowhere yet (docs/printing.md). */
export type SpoolDestination = `printer:${string}` | `pool:${string}` | 'download' | '';

/** Someone who added or printed a spool entry, by name. */
export interface SpoolActor {
  id: string;
  name?: string;
  token?: boolean;
}

/** One asset's label in the print queue: waiting, or printed once printed_at is set. */
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
  /** For a template and a destination it suits, what to know before printing it there. */
  fit_warnings?: Record<string, Record<string, string[]>>;
}

/** What adding to the print queue did. */
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

/** Printers loaded alike that share print jobs: each job is cut into batches of batch_size labels (docs/printing.md). */
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

/** A button people press on assets, such as Identify, triggering a hook: an asset.hook event (docs/hooks.md). */
export interface AssetHook {
  id: string;
  key: string;
  label: string;
  /** Asked before it is taken, when set. */
  confirm: string;
  /** What fires it: a button people press, events, or a schedule (docs/hooks.md). */
  triggers: HookTrigger[];
  created_at: string;
  updated_at: string;
}

/** A report's filter, as a schedule chooses its assets by (docs/reports.md). */
export interface HookFilter {
  running_low?: boolean;
  loan?: '' | 'open' | 'overdue';
  condition?: string;
  search?: string;
}

export type HookTrigger =
  | { type: 'button' }
  | { type: 'event'; kinds: string[]; condition?: { field: string; equals: string } | null; interval_seconds?: number }
  | { type: 'schedule'; cron: string; time_zone?: string; filter?: HookFilter };

/** A hook a category offers on its assets, and with cascades on its subcategories'. */
export interface CategoryHook {
  hook_id: string;
  cascades: boolean;
}

/** A hook an asset's categories offer on it. */
export interface OfferedHook {
  id: string;
  key: string;
  label: string;
  confirm?: string;
}

/** A live event connection a server holds open, for an API token or a signed-in user (docs/hooks.md). */
export interface LiveConnection {
  id: string;
  token_id?: string;
  user_id?: string;
  remote: string;
  opened_at: string;
  seen_at: string;
}

/** A device's answer to a hook triggered: done or failed, with a message. */
export interface HookReply {
  id: string;
  event_id: string;
  name: string;
  token: boolean;
  ok: boolean;
  message: string;
  created_at: string;
}

/** An error or warning from the icon compiler, at a place in a file of the build (docs/icons.md). */
export interface IconDiag {
  file: string;
  line: number;
  col: number;
  message: string;
}

/** A category's isolex source: the published version, and a later draft that did not compile. */
export interface CategoryIcon {
  category_id: string;
  rendition: Rendition;
  icon_name: string;
  source: string | null;
  draft: string | null;
  draft_errors: IconDiag[];
  updated_at: string;
}

/** Isolex source no one category owns. */
export interface IconLibraryFile {
  id: string;
  path: string;
  source: string | null;
  draft: string | null;
  draft_errors: IconDiag[];
  updated_at: string;
}

/** One published version of a source. */
export interface IconRevision {
  id: string;
  source: string;
  actor_user_id: string | null;
  created_at: string;
}

/** What saving a source came to. */
export interface IconSave {
  published: boolean;
  errors: IconDiag[];
  warnings: IconDiag[];
}

/** A compile that kept nothing: the IR when it is clean. */
export interface IconCheck {
  errors: IconDiag[];
  warnings: IconDiag[];
  ir?: import('./isolex/ir').IrDocument;
}

/** What can be starred (docs/favorites.md). */
export type FavoriteKind = 'asset' | 'category' | 'report' | 'flow';

/** Something the signed-in person has starred, as GET /auth/me/favorites lists it. */
export interface Favorite {
  kind: FavoriteKind;
  id: string;
  name: string;
  /** A flow's address, /f/<slug>. */
  slug?: string;
  /** Set on an archived asset, which the home page leaves out. */
  archived?: boolean;
  created_at: string;
}

/** Someone at a vendor: their role is what they are for, such as sales or support. */
export interface VendorContact {
  /** Absent on a contact not saved yet; kept when it is sent back. */
  id?: string;
  name: string;
  role: string;
  email: string;
  phone: string;
}

/** A supplier the space buys from, and how to reach it (docs/vendors.md). Its notes are Markdown. */
export interface Vendor {
  id: string;
  name: string;
  website: string;
  account_number: string;
  address: string;
  notes: string;
  contacts: VendorContact[];
  /** The scopes it is in, which decide who sees it; empty to those who do not see scopes. */
  scope_ids: string[];
  created_at: string;
  updated_at: string;
}

/** The kinds a contract may be (docs/contracts.md). */
export type ContractKind = 'warranty' | 'service' | 'maintenance' | 'lease' | 'license' | 'other';

/** How often a contract's cost is paid; empty when not said. */
export type Billing = '' | 'once' | 'monthly' | 'quarterly' | 'yearly';

/** Something named by its ID and name. */
export interface NamedThing {
  id: string;
  name: string;
}

/**
 * An agreement with a vendor about assets (docs/contracts.md). Dates are
 * YYYY-MM-DD, empty when not known. Its cost, currency and billing are
 * left out for someone without purchases.read in its vendor's scopes, and
 * costs_hidden says so. Its notes are Markdown.
 */
export interface Contract {
  id: string;
  vendor_id: string;
  vendor_name: string;
  kind: ContractKind;
  name: string;
  reference: string;
  start_date: string;
  end_date: string;
  auto_renews: boolean;
  notice_days: number;
  /** When notice must be given to stop it renewing; only for one that renews automatically. */
  notice_deadline?: string;
  cost?: string;
  currency?: string;
  billing?: Billing;
  costs_hidden?: boolean;
  notes: string;
  asset_ids: string[];
  category_ids: string[];
  assets: NamedThing[];
  categories: NamedThing[];
  created_at: string;
  updated_at: string;
}

/**
 * One thing a vendor sells (docs/vendors.md#catalogs), for a category
 * (things counted by items) or an asset (things counted by quantity). Its
 * price, currency and when it was checked are left out without
 * purchases.read for the vendor, and prices_hidden says so.
 */
export interface CatalogEntry {
  id: string;
  vendor_id: string;
  vendor_name: string;
  category_id?: string;
  asset_id?: string;
  /** The category's or the asset's name. */
  linked_name: string;
  part_number: string;
  description: string;
  url: string;
  pack_size: number | null;
  pack_unit: string;
  price?: string;
  currency?: string;
  /** YYYY-MM-DD. */
  price_checked?: string;
  prices_hidden?: boolean;
  /** The one to reorder from, of its category's or asset's entries. */
  preferred: boolean;
  /** Whether the requester may change it: vendors.manage for its vendor. */
  can_manage: boolean;
  created_at: string;
  updated_at: string;
}

/** A contract as the API takes it: every detail, so an absent one is emptied, but for what it covers, kept when left out. */
export interface ContractRequest {
  vendor_id: string;
  kind: ContractKind;
  name: string;
  reference: string;
  start_date: string;
  end_date: string;
  auto_renews: boolean;
  notice_days: number;
  cost: string;
  currency: string;
  billing: Billing;
  notes: string;
  asset_ids?: string[];
  category_ids?: string[];
}

/** A contract covering an asset, as the asset shows it; category_id is the category it covers it through. */
export interface ContractCover {
  id: string;
  name: string;
  kind: ContractKind;
  vendor_id: string;
  vendor_name: string;
  reference: string;
  start_date: string;
  end_date: string;
  auto_renews: boolean;
  category_id?: string;
  cost?: string;
  currency?: string;
  billing?: Billing;
}

/** A catalog entry as made or replaced: every property, so an absent one is emptied. */
export interface CatalogEntryRequest {
  vendor_id: string;
  category_id: string;
  asset_id: string;
  part_number: string;
  description: string;
  url: string;
  pack_size: number | null;
  pack_unit: string;
  price: string;
  currency: string;
  price_checked: string;
  preferred: boolean;
}

/** What comments are on (docs/comments.md). */
export type CommentOn = 'asset' | 'category' | 'loan' | 'filing';

/** A thing a comment is on, by name. */
export interface CommentPlace {
  id: string;
  name: string;
}

/**
 * A comment in Markdown, with its HTML rendered and sanitized on the
 * server. A deleted one has neither, and says who deleted it.
 */
export interface Comment {
  id: string;
  on: CommentOn;
  /** The asset, category or loan it is on; for a filing, the asset, with the flow as flow_id. */
  on_id: string;
  flow_id?: string;
  /** The asset it is about, for an asset's, a loan's or a filing's. */
  asset_id?: string;
  body: string;
  html: string;
  /** Who wrote and deleted it, to those who may see who. */
  author_id?: string;
  author_name?: string;
  mine: boolean;
  created_at: string;
  edited_at: string | null;
  deleted_at: string | null;
  deleted_by?: string;
  deleted_by_name?: string;
  /** The category it is on, when listed with another thing's comments. */
  category?: CommentPlace;
  /** What it is on, in search results. */
  place?: CommentPlace;
  can_edit: boolean;
  can_delete: boolean;
  /** The people its text mentions, by name now, to those who may see who. */
  mentions?: { id: string; name: string }[];
}

/** An asset a flow filed, which comments can be on. */
export interface Filing {
  flow_id: string;
  flow_slug: string;
  flow_name: string;
  asset_id: string;
  filed_at: string;
  comments: number;
}
