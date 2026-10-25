import { HttpInterceptorFn, HttpResponse } from '@angular/common/http';
import { Injectable, inject, signal } from '@angular/core';
import { EMPTY, Subject, catchError, switchMap, tap } from 'rxjs';
import { Api } from './api';
import { Overview } from './models';

@Injectable({ providedIn: 'root' })
export class OverviewState {
  private readonly api = inject(Api);
  private readonly requests = new Subject<void>();
  readonly value = signal<Overview | null>(null);

  constructor() {
    this.requests
      .pipe(switchMap(() => this.api.overview().pipe(catchError(() => EMPTY))))
      .subscribe((o) => this.value.set(o));
  }

  refresh(): void {
    this.requests.next();
  }

  set(o: Overview): void {
    this.value.set(o);
  }
}

export const changesInterceptor: HttpInterceptorFn = (req, next) => {
  if (req.method === 'GET' || !req.url.startsWith('/api/')) {
    return next(req);
  }
  const state = inject(OverviewState);
  return next(req).pipe(
    tap((event) => {
      if (event instanceof HttpResponse) {
        state.refresh();
      }
    }),
  );
};
