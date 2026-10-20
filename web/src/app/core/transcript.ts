import { Message, Step } from './models';

export interface UserTurn {
  role: 'user';
  text: string;
}

export interface AgentTurn {
  role: 'assistant';
  text: string;
  steps: LiveStep[];
  pending: boolean;
  error?: string;
  cost?: number;
  tokens?: number;
}

export interface LiveStep extends Step {
  done: boolean;
}

export type Turn = UserTurn | AgentTurn;

export function transcript(messages: Message[]): Turn[] {
  const turns: Turn[] = [];
  let agent: AgentTurn | null = null;
  const byCall = new Map<string, LiveStep>();

  const current = (): AgentTurn => {
    if (!agent) {
      agent = { role: 'assistant', text: '', steps: [], pending: false };
      turns.push(agent);
    }
    return agent;
  };

  for (const m of messages) {
    switch (m.role) {
      case 'user':
        agent = null;
        turns.push({ role: 'user', text: m.content });
        break;
      case 'assistant':
        for (const call of m.tool_calls ?? []) {
          const step: LiveStep = { name: call.name, arguments: call.arguments, done: false };
          byCall.set(call.id, step);
          current().steps.push(step);
        }
        if (m.content.trim()) {
          current().text = m.content;
        }
        break;
      case 'tool': {
        const step = byCall.get(m.tool_call_id ?? '');
        if (step) {
          step.result = m.content;
          step.done = true;
          step.failed = isFailure(m.content);
        }
        break;
      }
    }
  }
  return turns;
}

function isFailure(result: string): boolean {
  try {
    const parsed = JSON.parse(result) as Record<string, unknown>;
    return Object.keys(parsed).length === 1 && typeof parsed['error'] === 'string';
  } catch {
    return false;
  }
}

const labels: Record<string, string> = {
  remember: 'Saved to memory',
  recall: 'Searched memory',
  add_obligation: 'Added to ledger',
  list_obligations: 'Read the ledger',
  update_obligation: 'Updated an item',
  mark_paid: 'Marked as paid',
  snooze_obligation: 'Snoozed reminders',
  add_calendar_reminder: 'Made a calendar file',
  draft_email: 'Drafted an email for approval',
  web_search: 'Searched the web',
};

export function stepLabel(name: string): string {
  return labels[name] ?? name.replace(/_/g, ' ');
}

export function pretty(json: string | undefined): string {
  if (!json) {
    return '';
  }
  try {
    return JSON.stringify(JSON.parse(json), null, 2);
  } catch {
    return json;
  }
}
