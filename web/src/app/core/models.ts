export type Category =
  'bill' | 'subscription' | 'renewal' | 'warranty' | 'document' | 'deadline' | 'other';
export type Recurrence = 'none' | 'weekly' | 'monthly' | 'quarterly' | 'yearly';
export type ObligationStatus = 'open' | 'done' | 'snoozed' | 'cancelled';
export type ActionStatus = 'pending' | 'approved' | 'rejected' | 'done' | 'failed';
export type ActionKind = 'mark_paid' | 'snooze' | 'calendar_event' | 'send_email';
export type MemoryKind = 'fact' | 'preference' | 'note';
export type ProposalStatus = 'pending' | 'accepted' | 'rejected';

export const CATEGORIES: Category[] = [
  'bill',
  'subscription',
  'renewal',
  'warranty',
  'document',
  'deadline',
  'other',
];
export const RECURRENCES: Recurrence[] = ['none', 'weekly', 'monthly', 'quarterly', 'yearly'];

export interface ListResponse<T> {
  items: T[];
}

export interface Obligation {
  id: number;
  title: string;
  category: Category;
  amount?: string;
  currency?: string;
  due_on?: string;
  days_until_due?: number;
  recurrence: Recurrence;
  status: ObligationStatus;
  snoozed_until?: string;
  notes?: string;
  source: string;
  source_ref?: string;
  created_at: string;
  updated_at: string;
}

export interface ObligationList extends ListResponse<Obligation> {
  today: string;
}

export interface ObligationInput {
  title?: string;
  category?: Category;
  amount?: string;
  currency?: string;
  due_on?: string;
  recurrence?: Recurrence;
  status?: ObligationStatus;
  snoozed_until?: string;
  notes?: string;
}

export interface ObligationFilter {
  status?: 'active' | ObligationStatus | 'all';
  category?: Category | '';
  due_within_days?: number;
  limit?: number;
}

export interface Overview {
  today: string;
  overdue: number;
  due_today: number;
  due_next_7_days: number;
  snoozed: number;
  without_due_date: number;
  pending_actions: number;
  pending_proposals: number;
  upcoming: Obligation[];
}

export interface Action {
  id: number;
  kind: ActionKind;
  obligation_id: number | null;
  payload: Record<string, unknown>;
  summary: string;
  status: ActionStatus;
  needs_approval: boolean;
  requested_by: string;
  source_ref?: string;
  result?: string;
  calendar_url?: string;
  error?: string;
  created_at: string;
  decided_at: string | null;
  executed_at: string | null;
}

export interface ActionRequest {
  kind: ActionKind;
  obligation_id?: number;
  payload?: Record<string, unknown>;
}

export interface AuditEntry {
  id: number;
  actor: string;
  event: string;
  subject: string;
  detail?: string;
  created_at: string;
}

export interface Memory {
  id: number;
  kind: MemoryKind;
  content: string;
  source: string;
  source_ref?: string;
  score?: number;
  created_at: string;
  updated_at: string;
}

export interface Conversation {
  id: number;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface ToolCall {
  id: string;
  name: string;
  arguments: string;
}

export interface Message {
  role: 'user' | 'assistant' | 'tool' | 'system';
  content: string;
  tool_calls?: ToolCall[];
  tool_call_id?: string;
}

export interface ConversationDetail extends Conversation {
  messages: Message[];
}

export interface Step {
  name: string;
  arguments: string;
  result?: string;
  failed?: boolean;
}

export interface ChatReply {
  conversation: Conversation;
  reply: string;
  steps: Step[];
  usage: { prompt_tokens: number; completion_tokens: number };
  cost_usd: number;
}

export interface ApiErrorBody {
  error: { code: string; message: string };
}

export type ChatEvent =
  | { type: 'conversation'; conversation: Conversation }
  | { type: 'tool_call'; step: Step }
  | { type: 'tool_result'; step: Step }
  | { type: 'reply'; reply: ChatReply }
  | { type: 'error'; code: string; message: string };

export interface Proposal {
  id: number;
  ingestion_id: number;
  title: string;
  category: Category;
  amount?: string;
  currency?: string;
  due_on?: string;
  recurrence: Recurrence;
  notes?: string;
  evidence?: string;
  confidence: number;
  duplicate_of: number | null;
  status: ProposalStatus;
  obligation_id: number | null;
  created_at: string;
  decided_at: string | null;
}

export interface Ingestion {
  id: number;
  source_kind: string;
  source_name: string;
  summary: string;
  created_at: string;
  proposals: Proposal[];
  reused: boolean;
  truncated: boolean;
}

export interface Notification {
  id: number;
  kind: 'reminder' | 'review';
  title: string;
  body: string;
  obligation_id: number | null;
  attempts: number;
  last_error?: string;
  delivered_at: string | null;
  created_at: string;
}

export interface SweepReport {
  woken: number;
  reminders: number;
  scheduled_review: boolean;
  delivered: number;
  failed: number;
  review?: Notification;
}
