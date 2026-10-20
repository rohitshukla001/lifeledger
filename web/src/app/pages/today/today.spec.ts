import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of } from 'rxjs';
import { Api } from '../../core/api';
import { Obligation, Overview } from '../../core/models';
import { Today } from './today';

const rent: Obligation = {
  id: 7,
  title: 'Rent',
  category: 'bill',
  amount: '25000.00',
  currency: 'INR',
  due_on: '2026-10-17',
  days_until_due: -2,
  recurrence: 'monthly',
  status: 'open',
  source: 'user',
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
};

const overview: Overview = {
  today: '2026-10-19',
  overdue: 1,
  due_today: 0,
  due_next_7_days: 2,
  snoozed: 0,
  without_due_date: 0,
  pending_actions: 1,
  pending_proposals: 0,
  upcoming: [rent],
};

describe('Today', () => {
  let api: {
    overview: ReturnType<typeof vi.fn>;
    notifications: ReturnType<typeof vi.fn>;
    requestAction: ReturnType<typeof vi.fn>;
    sweep: ReturnType<typeof vi.fn>;
  };

  beforeEach(() => {
    api = {
      overview: vi.fn(() => of(overview)),
      notifications: vi.fn(() =>
        of([
          {
            id: 1,
            kind: 'review',
            title: 'LifeLedger weekly review, Sun 18 Oct 2026',
            body: 'Rent is overdue.',
            obligation_id: null,
            attempts: 0,
            delivered_at: null,
            created_at: '2026-10-18T12:30:00Z',
          },
        ]),
      ),
      requestAction: vi.fn(() => of({ id: 3, status: 'done', result: 'next due 2026-11-17' })),
      sweep: vi.fn(() =>
        of({ woken: 0, reminders: 2, scheduled_review: false, delivered: 0, failed: 0 }),
      ),
    };
    TestBed.configureTestingModule({
      imports: [Today],
      providers: [provideRouter([]), { provide: Api, useValue: api }],
    });
  });

  it('shows the tally, the upcoming items, and the latest review', async () => {
    const fixture = TestBed.createComponent(Today);
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;

    expect(el.querySelector('h1')?.textContent).toContain('Monday 19 October');
    expect(el.textContent).toContain('1 overdue · 2 due this week');
    expect([...el.querySelectorAll('.tile .n')].map((n) => n.textContent)).toEqual([
      '1',
      '0',
      '2',
      '1',
      '0',
    ]);
    const entry = el.querySelector('.entry')!;
    expect(entry.textContent).toContain('Rent');
    expect(entry.textContent).toContain('₹25,000.00');
    expect(entry.textContent).toContain('2 days overdue');
    expect(entry.querySelector('.date')?.className).toContain('overdue');
    expect(el.querySelector('.review pre')?.textContent).toBe('Rent is overdue.');
  });

  it('marks an item paid and reloads', async () => {
    const fixture = TestBed.createComponent(Today);
    await fixture.whenStable();
    const paid = [
      ...(fixture.nativeElement as HTMLElement).querySelectorAll<HTMLButtonElement>('.acts button'),
    ].find((b) => b.textContent?.includes('Paid'))!;
    paid.click();
    await fixture.whenStable();

    expect(api.requestAction).toHaveBeenCalledWith({ kind: 'mark_paid', obligation_id: 7 });
    expect(api.overview).toHaveBeenCalledTimes(2);
  });

  it('snoozes for three days from the server date', async () => {
    const fixture = TestBed.createComponent(Today);
    await fixture.whenStable();
    const snooze = (fixture.nativeElement as HTMLElement).querySelector<HTMLButtonElement>(
      'button[title="Snooze for 3 days"]',
    )!;
    snooze.click();
    expect(api.requestAction).toHaveBeenCalledWith({
      kind: 'snooze',
      obligation_id: 7,
      payload: { until: '2026-10-22' },
    });
  });
});
