import { ChangeDetectionStrategy, Component, DestroyRef, inject } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter, fromEvent, merge, startWith } from 'rxjs';
import { Notices } from '../core/notices';
import { OverviewState } from '../core/overview-state';
import { Session } from '../core/session';
import { Icon } from '../shared/icon';

interface NavItem {
  path: string;
  label: string;
  icon: string;
}

@Component({
  selector: 'll-shell',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterOutlet, RouterLink, RouterLinkActive, Icon],
  templateUrl: './shell.html',
  styleUrl: './shell.css',
})
export class Shell {
  private readonly session = inject(Session);
  private readonly router = inject(Router);
  protected readonly notices = inject(Notices);

  protected readonly overview = inject(OverviewState);
  protected readonly nav: NavItem[] = [
    { path: '/today', label: 'Today', icon: 'today' },
    { path: '/chat', label: 'Chat', icon: 'chat' },
    { path: '/ledger', label: 'Ledger', icon: 'ledger' },
    { path: '/approvals', label: 'Approvals', icon: 'approvals' },
    { path: '/import', label: 'Import', icon: 'import' },
    { path: '/memory', label: 'Memory', icon: 'memory' },
    { path: '/activity', label: 'Activity', icon: 'activity' },
  ];

  constructor() {
    merge(
      this.router.events.pipe(filter((e) => e instanceof NavigationEnd)),
      fromEvent(document, 'visibilitychange').pipe(
        filter(() => document.visibilityState === 'visible'),
      ),
    )
      .pipe(startWith(null), takeUntilDestroyed(inject(DestroyRef)))
      .subscribe(() => this.overview.refresh());
  }

  protected badge(path: string): number {
    const o = this.overview.value();
    if (!o) {
      return 0;
    }
    switch (path) {
      case '/approvals':
        return o.pending_actions + o.pending_proposals;
      case '/today':
        return o.overdue + o.due_today;
      default:
        return 0;
    }
  }

  protected lock(): void {
    this.session.end();
    void this.router.navigate(['/unlock']);
  }
}
