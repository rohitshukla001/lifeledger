import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { Observable, forkJoin } from 'rxjs';
import { Api, errorMessage } from '../../core/api';
import { Action, Proposal } from '../../core/models';
import { Notices } from '../../core/notices';
import { AgoPipe } from '../../core/pipes';
import { ProposalCard } from '../../shared/proposal-card';
import { Stamp } from '../../shared/stamp';

interface EmailPayload {
  to?: string;
  subject?: string;
  body?: string;
}

@Component({
  selector: 'll-approvals',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgoPipe, ProposalCard, Stamp],
  templateUrl: './approvals.html',
  styleUrl: './approvals.css',
})
export class Approvals {
  private readonly api = inject(Api);
  private readonly notices = inject(Notices);

  protected readonly pending = signal<Action[]>([]);
  protected readonly proposals = signal<Proposal[]>([]);
  protected readonly recent = signal<Action[]>([]);
  protected readonly loading = signal(true);
  protected readonly error = signal<string | null>(null);
  protected readonly busy = signal<string | null>(null);
  protected readonly decided = computed(() =>
    this.recent()
      .filter((a) => a.status !== 'pending')
      .slice(0, 15),
  );

  constructor() {
    this.load();
  }

  protected email(a: Action): EmailPayload {
    return a.payload as EmailPayload;
  }

  protected approve(a: Action): void {
    this.run(`a${a.id}`, this.api.approveAction(a.id), (r) =>
      r.status === 'done'
        ? `Approved and done: ${a.summary}.`
        : `Approved, but it failed: ${r.error}`,
    );
  }

  protected reject(a: Action): void {
    this.run(`a${a.id}`, this.api.rejectAction(a.id), () => `Rejected: ${a.summary}.`);
  }

  protected accept(p: Proposal): void {
    this.run(`p${p.id}`, this.api.acceptProposal(p.id), (o) => `Added “${o.title}” to the ledger.`);
  }

  protected discard(p: Proposal): void {
    this.run(`p${p.id}`, this.api.rejectProposal(p.id), () => `Discarded “${p.title}”.`);
  }

  private load(): void {
    forkJoin({
      pending: this.api.actions('pending', 100),
      proposals: this.api.proposals('pending'),
      recent: this.api.actions(undefined, 40),
    }).subscribe({
      next: (r) => {
        this.pending.set(r.pending);
        this.proposals.set(r.proposals);
        this.recent.set(r.recent);
        this.loading.set(false);
        this.error.set(null);
      },
      error: (err: unknown) => {
        this.loading.set(false);
        this.error.set(errorMessage(err));
      },
    });
  }

  private run<T>(key: string, op: Observable<T>, done: (v: T) => string): void {
    this.busy.set(key);
    op.subscribe({
      next: (v) => {
        this.busy.set(null);
        this.notices.info(done(v));
        this.load();
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.notices.error(errorMessage(err));
        this.load();
      },
    });
  }
}
