import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { Observable } from 'rxjs';
import { Api, errorMessage } from '../../core/api';
import { Ingestion, Proposal } from '../../core/models';
import { Notices } from '../../core/notices';
import { ProposalCard } from '../../shared/proposal-card';

const MAX_BYTES = 10 * 1024 * 1024;

@Component({
  selector: 'll-import',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, RouterLink, ProposalCard],
  templateUrl: './import.html',
  styleUrl: './import.css',
})
export class Import {
  private readonly api = inject(Api);
  private readonly notices = inject(Notices);

  protected readonly text = signal('');
  protected readonly dragging = signal(false);
  protected readonly working = signal(false);
  protected readonly error = signal<string | null>(null);
  protected readonly result = signal<Ingestion | null>(null);
  protected readonly busy = signal<number | null>(null);

  protected dropped(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(false);
    const file = event.dataTransfer?.files.item(0);
    if (file) {
      this.upload(file);
    }
  }

  protected picked(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.item(0);
    input.value = '';
    if (file) {
      this.upload(file);
    }
  }

  protected paste(): void {
    const text = this.text().trim();
    if (text) {
      this.submit(this.api.ingestText('pasted text', text));
    }
  }

  protected accept(p: Proposal): void {
    this.decide(p, this.api.acceptProposal(p.id), 'accepted', `Added “${p.title}” to the ledger.`);
  }

  protected discard(p: Proposal): void {
    this.decide(p, this.api.rejectProposal(p.id), 'rejected', `Discarded “${p.title}”.`);
  }

  protected pending(r: Ingestion): Proposal[] {
    return r.proposals.filter((p) => p.status === 'pending');
  }

  private upload(file: File): void {
    if (file.size > MAX_BYTES) {
      this.error.set('The file is larger than 10 MB.');
      return;
    }
    this.submit(this.api.ingestFile(file));
  }

  private submit(op: Observable<Ingestion>): void {
    this.working.set(true);
    this.error.set(null);
    this.result.set(null);
    op.subscribe({
      next: (r) => {
        this.working.set(false);
        this.result.set(r);
        this.text.set('');
      },
      error: (err: unknown) => {
        this.working.set(false);
        this.error.set(errorMessage(err));
      },
    });
  }

  private decide(
    p: Proposal,
    op: Observable<unknown>,
    status: Proposal['status'],
    message: string,
  ): void {
    this.busy.set(p.id);
    op.subscribe({
      next: () => {
        this.busy.set(null);
        this.notices.info(message);
        this.result.update((r) =>
          r
            ? { ...r, proposals: r.proposals.map((x) => (x.id === p.id ? { ...x, status } : x)) }
            : r,
        );
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.notices.error(errorMessage(err));
      },
    });
  }
}
