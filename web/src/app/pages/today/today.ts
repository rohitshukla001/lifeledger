import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Observable, forkJoin } from 'rxjs';
import { Api, errorMessage } from '../../core/api';
import { dueTone, longDay, settleLabel, shortMonth } from '../../core/format';
import { Notification, Obligation, Overview } from '../../core/models';
import { Notices } from '../../core/notices';
import { ObligationActions } from '../../core/obligation-actions';
import { AgoPipe, DuePipe, MoneyPipe } from '../../core/pipes';
import { Stamp } from '../../shared/stamp';

@Component({
  selector: 'll-today',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, MoneyPipe, DuePipe, AgoPipe, Stamp],
  templateUrl: './today.html',
  styleUrl: './today.css',
})
export class Today {
  private readonly api = inject(Api);
  private readonly actions = inject(ObligationActions);
  private readonly notices = inject(Notices);

  protected readonly overview = signal<Overview | null>(null);
  protected readonly notifications = signal<Notification[]>([]);
  protected readonly error = signal<string | null>(null);
  protected readonly busy = signal<number | 'sweep' | 'review' | null>(null);

  protected readonly heading = computed(() => longDay(this.overview()?.today));
  protected readonly review = computed(
    () => this.notifications().find((n) => n.kind === 'review') ?? null,
  );
  protected readonly reminders = computed(() =>
    this.notifications()
      .filter((n) => n.kind === 'reminder')
      .slice(0, 6),
  );
  protected readonly summary = computed(() => {
    const o = this.overview();
    if (!o) {
      return '';
    }
    const parts: string[] = [];
    if (o.overdue) parts.push(`${o.overdue} overdue`);
    if (o.due_today) parts.push(`${o.due_today} due today`);
    if (o.due_next_7_days) parts.push(`${o.due_next_7_days} due this week`);
    return parts.length ? parts.join(' · ') : 'Nothing needs you today.';
  });
  protected readonly tone = dueTone;
  protected readonly settleLabel = settleLabel;

  constructor() {
    this.load();
  }

  protected load(): void {
    forkJoin({
      overview: this.api.overview(),
      notifications: this.api.notifications(30),
    }).subscribe({
      next: ({ overview, notifications }) => {
        this.overview.set(overview);
        this.notifications.set(notifications);
        this.error.set(null);
      },
      error: (err: unknown) => this.error.set(errorMessage(err)),
    });
  }

  protected day(o: Obligation): string {
    return o.due_on ? String(Number(o.due_on.slice(8, 10))) : '—';
  }

  protected month(o: Obligation): string {
    return shortMonth(o.due_on);
  }

  protected markPaid(o: Obligation): void {
    this.run(o.id, this.actions.markPaid(o), (a) => `${o.title}: ${a.result || 'done'}.`);
  }

  protected snooze(o: Obligation, days: number): void {
    const today = this.overview()?.today;
    if (today) {
      this.run(o.id, this.actions.snooze(o, today, days), (a) => `${o.title}: ${a.result}.`);
    }
  }

  protected calendar(o: Obligation): void {
    this.run(o.id, this.actions.calendar(o), () => `Calendar file for ${o.title} downloaded.`);
  }

  protected sweep(review: boolean): void {
    this.busy.set(review ? 'review' : 'sweep');
    this.api.sweep(review).subscribe({
      next: (r) => {
        this.busy.set(null);
        this.notices.info(
          review
            ? 'Weekly review written.'
            : `Sweep done: ${r.reminders} new reminders, ${r.woken} snoozed items woken, ${r.delivered} delivered.`,
        );
        this.load();
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.notices.error(errorMessage(err));
      },
    });
  }

  private run<T>(id: number, op: Observable<T>, done: (v: T) => string): void {
    this.busy.set(id);
    op.subscribe({
      next: (v) => {
        this.busy.set(null);
        this.notices.info(done(v));
        this.load();
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.notices.error(errorMessage(err));
      },
    });
  }
}
