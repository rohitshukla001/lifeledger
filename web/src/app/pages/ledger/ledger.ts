import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Observable } from 'rxjs';
import { Api, errorMessage } from '../../core/api';
import { dueTone, settleLabel } from '../../core/format';
import { CATEGORIES, Category, Obligation, ObligationFilter } from '../../core/models';
import { Notices } from '../../core/notices';
import { ObligationActions } from '../../core/obligation-actions';
import { DayPipe, DuePipe, MoneyPipe } from '../../core/pipes';
import { Stamp } from '../../shared/stamp';
import { ObligationForm } from './obligation-form';

type View = 'active' | 'done' | 'all';

@Component({
  selector: 'll-ledger',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, MoneyPipe, DayPipe, DuePipe, Stamp, ObligationForm],
  host: { '(document:keydown.escape)': 'editing.set(null)' },
  templateUrl: './ledger.html',
  styleUrl: './ledger.css',
})
export class Ledger {
  private readonly api = inject(Api);
  private readonly actions = inject(ObligationActions);
  private readonly notices = inject(Notices);

  protected readonly categories = CATEGORIES;
  protected readonly views: View[] = ['active', 'done', 'all'];
  protected readonly view = signal<View>('active');
  protected readonly category = signal<Category | ''>('');
  protected readonly search = signal('');
  protected readonly items = signal<Obligation[]>([]);
  protected readonly today = signal('');
  protected readonly loading = signal(true);
  protected readonly error = signal<string | null>(null);
  protected readonly busy = signal<number | null>(null);
  protected readonly editing = signal<Obligation | 'new' | null>(null);

  protected readonly shown = computed(() => {
    const q = this.search().trim().toLowerCase();
    return q
      ? this.items().filter(
          (o) => o.title.toLowerCase().includes(q) || (o.notes ?? '').toLowerCase().includes(q),
        )
      : this.items();
  });
  protected readonly total = computed(() => {
    const sums = new Map<string, number>();
    for (const o of this.shown()) {
      if (o.amount && o.status !== 'done' && o.status !== 'cancelled') {
        const code = o.currency || '';
        sums.set(code, (sums.get(code) ?? 0) + Number(o.amount));
      }
    }
    return [...sums.entries()].map(([currency, sum]) => ({ currency, amount: sum.toFixed(2) }));
  });
  protected readonly formTarget = computed(() => {
    const e = this.editing();
    return e === 'new' ? null : e;
  });
  protected readonly tone = dueTone;
  protected readonly settleLabel = settleLabel;

  constructor() {
    this.load();
  }

  protected setView(view: View): void {
    this.view.set(view);
    this.load();
  }

  protected setCategory(category: Category | ''): void {
    this.category.set(category);
    this.load();
  }

  protected load(): void {
    const status: ObligationFilter['status'] = this.view() === 'done' ? 'done' : this.view();
    this.loading.set(true);
    this.api.obligations({ status, category: this.category(), limit: 500 }).subscribe({
      next: (res) => {
        this.items.set(res.items);
        this.today.set(res.today);
        this.loading.set(false);
        this.error.set(null);
      },
      error: (err: unknown) => {
        this.loading.set(false);
        this.error.set(errorMessage(err));
      },
    });
  }

  protected state(o: Obligation): string {
    return o.status === 'open' ? this.tone(o.days_until_due) : o.status;
  }

  protected saved(o: Obligation): void {
    this.editing.set(null);
    this.notices.info(`Saved “${o.title}”.`);
    this.load();
  }

  protected markPaid(o: Obligation): void {
    this.run(o.id, this.actions.markPaid(o), (a) => `${o.title}: ${a.result || 'done'}.`);
  }

  protected snooze(o: Obligation): void {
    this.run(o.id, this.actions.snooze(o, this.today(), 7), (a) => `${o.title}: ${a.result}.`);
  }

  protected calendar(o: Obligation): void {
    this.run(o.id, this.actions.calendar(o), () => `Calendar file for ${o.title} downloaded.`);
  }

  protected remove(o: Obligation): void {
    if (!confirm(`Delete “${o.title}” and its reminders? This cannot be undone.`)) {
      return;
    }
    this.run(o.id, this.api.deleteObligation(o.id), () => `Deleted “${o.title}”.`);
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
