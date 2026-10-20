import { addDays, ago, day, dueLabel, dueTone, linkify, longDay, money } from './format';

describe('format', () => {
  it('formats money with the Indian grouping for INR', () => {
    expect(money('234050.50', 'INR')).toBe('₹2,34,050.50');
    expect(money('12.5', 'USD')).toBe('$12.50');
    expect(money(undefined, 'INR')).toBe('');
    expect(money('9', 'XYZ1')).toBe('XYZ1 9');
  });

  it('labels due dates', () => {
    expect([-3, -1, 0, 1, 5].map(dueLabel)).toEqual([
      '3 days overdue',
      '1 day overdue',
      'Due today',
      'Tomorrow',
      'In 5 days',
    ]);
    expect(dueLabel(undefined)).toBe('No date');
    expect([-1, 0, 7, 8, undefined].map(dueTone)).toEqual([
      'overdue',
      'today',
      'soon',
      'later',
      'none',
    ]);
  });

  it('formats days in UTC', () => {
    expect(day('2026-10-19')).toBe('Mon 19 Oct 2026');
    expect(longDay('2026-10-19')).toBe('Monday 19 October');
    expect(addDays('2026-10-30', 3)).toBe('2026-11-02');
  });

  it('describes elapsed time', () => {
    const now = new Date('2026-10-19T12:00:00Z');
    expect(ago('2026-10-19T11:59:30Z', now)).toBe('just now');
    expect(ago('2026-10-19T11:15:00Z', now)).toBe('45 min ago');
    expect(ago('2026-10-19T07:00:00Z', now)).toBe('5 h ago');
    expect(ago('2026-10-18T10:00:00Z', now)).toBe('yesterday');
    expect(ago('2026-10-15T12:00:00Z', now)).toBe('4 days ago');
  });

  it('turns URLs into links without the trailing period', () => {
    expect(linkify('See https://incometax.gov.in/due-dates. Then pay.')).toEqual([
      { text: 'See ' },
      { text: 'https://incometax.gov.in/due-dates', href: 'https://incometax.gov.in/due-dates' },
      { text: '. Then pay.' },
    ]);
    expect(linkify('no links')).toEqual([{ text: 'no links' }]);
  });
});
