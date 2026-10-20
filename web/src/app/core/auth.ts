import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { catchError, throwError } from 'rxjs';
import { Session } from './session';

export const authInterceptor: HttpInterceptorFn = (req, next) => {
  if (!req.url.startsWith('/api/')) {
    return next(req);
  }
  const session = inject(Session);
  const router = inject(Router);
  const token = session.token();
  const authed = token ? req.clone({ setHeaders: { Authorization: `Bearer ${token}` } }) : req;
  return next(authed).pipe(
    catchError((err: unknown) => {
      if (err instanceof HttpErrorResponse && err.status === 401 && session.active()) {
        session.end();
        void router.navigate(['/unlock'], { queryParams: { returnTo: router.url } });
      }
      return throwError(() => err);
    }),
  );
};

export const sessionGuard: CanActivateFn = (_route, state) => {
  if (inject(Session).active()) {
    return true;
  }
  return inject(Router).createUrlTree(['/unlock'], { queryParams: { returnTo: state.url } });
};
