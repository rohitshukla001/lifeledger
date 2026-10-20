const moneyFormats = new Map<string, Intl.NumberFormat>();

export function money(amount: string | undefined, currency: string | undefined): string {
  if (!amount) {
    return '';
  }
  const value = Number(amount);
  if (!Number.isFinite(value)) {
    return amount;
  }
  const code = currency || 'USD';
  let fmt = moneyFormats.get(code);
  if (!fmt) {
    try {
      fmt = new Intl.NumberFormat(code === 'INR' ? 'en-IN' : 'en', {
        style: 'currency',
        currency: code,
      });
    } catch {
      return `${code} ${amount}`;
    }
    moneyFormats.set(code, fmt);
  }
  return fmt.format(value);
}

export function settleLabel(category: string): string {
  return ['bill', 'subscription', 'renewal'].includes(category) ? 'Paid' : 'Done';
}

export type DueTone = 'overdue' | 'today' | 'soon' | 'later' | 'none';

export function dueTone(days: number | undefined): DueTone {
  if (days === undefined) {
    return 'none';
  }
  if (days < 0) {
    return 'overdue';
  }
  if (days === 0) {
    return 'today';
  }
  return days <= 7 ? 'soon' : 'later';
}

export function dueLabel(days: number | undefined): string {
  if (days === undefined) {
    return 'No date';
  }
  if (days < -1) {
    return `${-days} days overdue`;
  }
  if (days === -1) {
    return '1 day overdue';
  }
  if (days === 0) {
    return 'Due today';
  }
  if (days === 1) {
    return 'Tomorrow';
  }
  return `In ${days} days`;
}

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const MONTHS = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
];

function parseDay(date: string | undefined): Date | null {
  if (!date) {
    return null;
  }
  const parsed = new Date(`${date}T00:00:00Z`);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

export function day(date: string | undefined): string {
  const d = parseDay(date);
  if (!d) {
    return date ?? '';
  }
  return `${WEEKDAYS[d.getUTCDay()].slice(0, 3)} ${d.getUTCDate()} ${MONTHS[d.getUTCMonth()].slice(0, 3)} ${d.getUTCFullYear()}`;
}

export function longDay(date: string | undefined): string {
  const d = parseDay(date);
  if (!d) {
    return date ?? '';
  }
  return `${WEEKDAYS[d.getUTCDay()]} ${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
}

export function shortMonth(date: string | undefined): string {
  const d = parseDay(date);
  return d ? MONTHS[d.getUTCMonth()].slice(0, 3) : '';
}

export function ago(timestamp: string, now: Date = new Date()): string {
  const then = new Date(timestamp).getTime();
  if (Number.isNaN(then)) {
    return '';
  }
  const seconds = Math.round((now.getTime() - then) / 1000);
  if (seconds < 60) {
    return 'just now';
  }
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${minutes} min ago`;
  }
  const hours = Math.round(minutes / 60);
  if (hours < 24) {
    return `${hours} h ago`;
  }
  const days = Math.round(hours / 24);
  return days === 1 ? 'yesterday' : `${days} days ago`;
}

export function addDays(date: string, days: number): string {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

export type Segment = { text: string; href?: string };

const urlPattern = /https?:\/\/[^\s<>()"']+[^\s<>()"'.,;:!?]/g;

export function linkify(text: string): Segment[] {
  const out: Segment[] = [];
  let last = 0;
  for (const match of text.matchAll(urlPattern)) {
    const start = match.index ?? 0;
    if (start > last) {
      out.push({ text: text.slice(last, start) });
    }
    out.push({ text: match[0], href: match[0] });
    last = start + match[0].length;
  }
  if (last < text.length) {
    out.push({ text: text.slice(last) });
  }
  return out;
}
