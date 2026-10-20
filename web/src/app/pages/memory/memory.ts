import { ChangeDetectionStrategy, Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { Subject, debounceTime, distinctUntilChanged, of, switchMap } from 'rxjs';
import { Api, errorMessage } from '../../core/api';
import { Memory, MemoryKind } from '../../core/models';
import { Notices } from '../../core/notices';
import { AgoPipe } from '../../core/pipes';

@Component({
  selector: 'll-memory',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, AgoPipe],
  templateUrl: './memory.html',
  styleUrl: './memory.css',
})
export class MemoryPage {
  private readonly api = inject(Api);
  private readonly notices = inject(Notices);
  private readonly queries = new Subject<string>();

  protected readonly kinds: MemoryKind[] = ['fact', 'preference', 'note'];
  protected readonly items = signal<Memory[]>([]);
  protected readonly hits = signal<Memory[] | null>(null);
  protected readonly query = signal('');
  protected readonly content = signal('');
  protected readonly kind = signal<MemoryKind>('fact');
  protected readonly editing = signal<number | null>(null);
  protected readonly editText = signal('');
  protected readonly loading = signal(true);
  protected readonly error = signal<string | null>(null);

  constructor() {
    this.load();
    this.queries
      .pipe(
        debounceTime(300),
        distinctUntilChanged(),
        switchMap((q) => (q.trim() ? this.api.recall(q.trim(), 10) : of(null))),
        takeUntilDestroyed(inject(DestroyRef)),
      )
      .subscribe({
        next: (hits) => this.hits.set(hits),
        error: (err: unknown) => this.notices.error(errorMessage(err)),
      });
  }

  protected search(q: string): void {
    this.query.set(q);
    this.queries.next(q);
  }

  protected source(m: Memory): string {
    switch (m.source) {
      case 'chat':
        return 'from a chat';
      case 'web':
        return 'from the web';
      case 'user':
      case 'cli':
        return 'added by you';
      default:
        return m.source;
    }
  }

  protected add(): void {
    const text = this.content().trim();
    if (!text) {
      return;
    }
    this.api.remember(text, this.kind()).subscribe({
      next: (m) => {
        this.content.set('');
        this.notices.info(
          this.items().some((x) => x.id === m.id) ? 'I already knew that.' : 'Saved to memory.',
        );
        this.load();
      },
      error: (err: unknown) => this.notices.error(errorMessage(err)),
    });
  }

  protected startEdit(m: Memory): void {
    this.editing.set(m.id);
    this.editText.set(m.content);
  }

  protected saveEdit(m: Memory): void {
    const text = this.editText().trim();
    if (!text || text === m.content) {
      this.editing.set(null);
      return;
    }
    this.api.editMemory(m.id, text).subscribe({
      next: () => {
        this.editing.set(null);
        this.load();
      },
      error: (err: unknown) => this.notices.error(errorMessage(err)),
    });
  }

  protected forget(m: Memory): void {
    if (!confirm(`Forget “${m.content}”? This cannot be undone.`)) {
      return;
    }
    this.api.forget(m.id).subscribe({
      next: () => {
        this.notices.info('Forgotten.');
        this.hits.update((h) => h?.filter((x) => x.id !== m.id) ?? null);
        this.load();
      },
      error: (err: unknown) => this.notices.error(errorMessage(err)),
    });
  }

  private load(): void {
    this.api.memories(500).subscribe({
      next: (items) => {
        this.items.set(items);
        this.loading.set(false);
        this.error.set(null);
      },
      error: (err: unknown) => {
        this.loading.set(false);
        this.error.set(errorMessage(err));
      },
    });
  }
}
