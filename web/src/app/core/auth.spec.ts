import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { authInterceptor, sessionGuard } from './auth';
import { Session } from './session';

describe('auth', () => {
  let http: HttpClient;
  let backend: HttpTestingController;
  let session: Session;
  let router: Router;

  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(withInterceptors([authInterceptor])),
        provideHttpClientTesting(),
      ],
    });
    http = TestBed.inject(HttpClient);
    backend = TestBed.inject(HttpTestingController);
    session = TestBed.inject(Session);
    router = TestBed.inject(Router);
  });

  afterEach(() => backend.verify());

  it('adds the bearer token to API requests only', () => {
    session.start('secret-token-123');
    http.get('/api/v1/overview').subscribe();
    http.get('/assets/x.json').subscribe();
    expect(backend.expectOne('/api/v1/overview').request.headers.get('Authorization')).toBe(
      'Bearer secret-token-123',
    );
    expect(backend.expectOne('/assets/x.json').request.headers.has('Authorization')).toBe(false);
  });

  it('sends no header in open mode', () => {
    session.start(null);
    expect(session.active()).toBe(true);
    http.get('/api/v1/overview').subscribe();
    expect(backend.expectOne('/api/v1/overview').request.headers.has('Authorization')).toBe(false);
  });

  it('ends the session and goes to unlock on 401', () => {
    const navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    session.start('old-token-0123456789');
    http.get('/api/v1/overview').subscribe({ error: () => undefined });
    backend
      .expectOne('/api/v1/overview')
      .flush(
        { error: { code: 'unauthorized', message: 'no' } },
        { status: 401, statusText: 'Unauthorized' },
      );
    expect(session.active()).toBe(false);
    expect(localStorage.getItem('lifeledger.token')).toBeNull();
    expect(navigate).toHaveBeenCalledWith(['/unlock'], expect.anything());
  });

  it('guards routes without a session', () => {
    const guard = (url: string) =>
      TestBed.runInInjectionContext(() => sessionGuard({} as never, { url } as never));
    expect(router.serializeUrl(guard('/ledger') as never)).toBe('/unlock?returnTo=%2Fledger');
    session.start('token-0123456789abcdef');
    expect(guard('/ledger')).toBe(true);
  });
});
