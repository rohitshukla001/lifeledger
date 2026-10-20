import { Injectable, inject } from '@angular/core';
import { Observable, map, switchMap, tap } from 'rxjs';
import { Api } from './api';
import { saveBlob } from './download';
import { addDays } from './format';
import { Action, Obligation } from './models';

@Injectable({ providedIn: 'root' })
export class ObligationActions {
  private readonly api = inject(Api);

  markPaid(o: Obligation): Observable<Action> {
    return this.api
      .requestAction({ kind: 'mark_paid', obligation_id: o.id })
      .pipe(map(failIfFailed));
  }

  snooze(o: Obligation, today: string, days: number): Observable<Action> {
    return this.api
      .requestAction({
        kind: 'snooze',
        obligation_id: o.id,
        payload: { until: addDays(today, days) },
      })
      .pipe(map(failIfFailed));
  }

  calendar(o: Obligation, remindDaysBefore = 3): Observable<Action> {
    return this.api
      .requestAction({
        kind: 'calendar_event',
        obligation_id: o.id,
        payload: { remind_days_before: remindDaysBefore },
      })
      .pipe(
        map(failIfFailed),
        switchMap((action) =>
          this.api.calendarFile(action.calendar_url ?? '').pipe(
            tap((blob) => saveBlob(blob, `${slug(o.title)}.ics`)),
            map(() => action),
          ),
        ),
      );
  }
}

function failIfFailed(action: Action): Action {
  if (action.status === 'failed') {
    throw new Error(action.error || 'The action failed.');
  }
  return action;
}

function slug(title: string): string {
  return (
    title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '') || 'reminder'
  );
}
