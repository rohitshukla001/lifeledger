import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { forkJoin } from 'rxjs';
import { Api, errorMessage } from '../../core/api';
import { AuditEntry, Notification } from '../../core/models';
import { AgoPipe } from '../../core/pipes';

type Tab = 'notifications' | 'audit';

@Component({
  selector: 'll-activity',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [AgoPipe],
  templateUrl: './activity.html',
  styleUrl: './activity.css',
})
export class Activity {
  private readonly api = inject(Api);

  protected readonly tab = signal<Tab>('notifications');
  protected readonly notifications = signal<Notification[]>([]);
  protected readonly audit = signal<AuditEntry[]>([]);
  protected readonly open = signal<number | null>(null);
  protected readonly error = signal<string | null>(null);

  constructor() {
    forkJoin({ notifications: this.api.notifications(100), audit: this.api.audit(200) }).subscribe({
      next: (r) => {
        this.notifications.set(r.notifications);
        this.audit.set(r.audit);
      },
      error: (err: unknown) => this.error.set(errorMessage(err)),
    });
  }

  protected toggle(id: number): void {
    this.open.update((current) => (current === id ? null : id));
  }

  protected delivery(n: Notification): string {
    if (n.delivered_at) {
      return 'sent to Telegram';
    }
    if (n.last_error) {
      return `not sent (${n.attempts}×): ${n.last_error}`;
    }
    return 'in LifeLedger only';
  }
}
