import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Overview } from './models';
import { OverviewState, changesInterceptor } from './overview-state';

describe('OverviewState', () => {
  let http: HttpClient;
  let backend: HttpTestingController;
  let state: OverviewState;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([changesInterceptor])),
        provideHttpClientTesting(),
      ],
    });
    http = TestBed.inject(HttpClient);
    backend = TestBed.inject(HttpTestingController);
    state = TestBed.inject(OverviewState);
  });

  afterEach(() => backend.verify());

  it('reloads the counts after a successful change', () => {
    http.post('/api/v1/obligations/1/paid', {}).subscribe();
    backend.expectOne('/api/v1/obligations/1/paid').flush({});
    backend.expectOne('/api/v1/overview').flush({ overdue: 0, due_today: 0 } as Overview);
    expect(state.value()?.overdue).toBe(0);
  });

  it('does not reload after a read or a failed change', () => {
    http.get('/api/v1/obligations').subscribe();
    backend.expectOne('/api/v1/obligations').flush([]);
    http.post('/api/v1/obligations', {}).subscribe({ error: () => undefined });
    backend.expectOne('/api/v1/obligations').flush({}, { status: 400, statusText: 'Bad Request' });
    backend.expectNone('/api/v1/overview');
  });

  it('keeps the last counts when a reload fails', () => {
    state.set({ overdue: 2, due_today: 1 } as Overview);
    state.refresh();
    backend.expectOne('/api/v1/overview').flush({}, { status: 500, statusText: 'Error' });
    expect(state.value()?.overdue).toBe(2);
  });
});
