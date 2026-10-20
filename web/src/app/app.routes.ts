import { Routes, UrlMatchResult, UrlSegment } from '@angular/router';
import { sessionGuard } from './core/auth';
import { Shell } from './layout/shell';

export const routes: Routes = [
  {
    path: 'unlock',
    title: 'Unlock · LifeLedger',
    loadComponent: () => import('./pages/unlock/unlock').then((m) => m.Unlock),
  },
  {
    path: '',
    component: Shell,
    canActivate: [sessionGuard],
    children: [
      { path: '', pathMatch: 'full', redirectTo: 'today' },
      {
        path: 'today',
        title: 'Today · LifeLedger',
        loadComponent: () => import('./pages/today/today').then((m) => m.Today),
      },
      {
        matcher: chatMatcher,
        title: 'Chat · LifeLedger',
        loadComponent: () => import('./pages/chat/chat').then((m) => m.Chat),
      },
      {
        path: 'ledger',
        title: 'Ledger · LifeLedger',
        loadComponent: () => import('./pages/ledger/ledger').then((m) => m.Ledger),
      },
      {
        path: 'approvals',
        title: 'Approvals · LifeLedger',
        loadComponent: () => import('./pages/approvals/approvals').then((m) => m.Approvals),
      },
      {
        path: 'import',
        title: 'Import · LifeLedger',
        loadComponent: () => import('./pages/import/import').then((m) => m.Import),
      },
      {
        path: 'memory',
        title: 'Memory · LifeLedger',
        loadComponent: () => import('./pages/memory/memory').then((m) => m.MemoryPage),
      },
      {
        path: 'activity',
        title: 'Activity · LifeLedger',
        loadComponent: () => import('./pages/activity/activity').then((m) => m.Activity),
      },
    ],
  },
  { path: '**', redirectTo: '' },
];

export function chatMatcher(segments: UrlSegment[]): UrlMatchResult | null {
  if (segments[0]?.path !== 'chat' || segments.length > 2) {
    return null;
  }
  return { consumed: segments, posParams: segments.length === 2 ? { id: segments[1] } : {} };
}
