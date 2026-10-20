import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { firstValueFrom, toArray } from 'rxjs';
import { Api, errorMessage } from './api';
import { Session } from './session';

function sseResponse(body: string, status = 200): Response {
  return new Response(body, { status, headers: { 'Content-Type': 'text/event-stream' } });
}

describe('Api', () => {
  let api: Api;
  let backend: HttpTestingController;

  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({
      providers: [provideRouter([]), provideHttpClient(), provideHttpClientTesting()],
    });
    api = TestBed.inject(Api);
    backend = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    backend.verify();
    vi.unstubAllGlobals();
  });

  it('sends list filters as query parameters and unwraps items', async () => {
    const result = firstValueFrom(api.actions('pending', 10));
    const req = backend.expectOne((r) => r.url === '/api/v1/actions');
    expect(req.request.params.get('status')).toBe('pending');
    expect(req.request.params.get('limit')).toBe('10');
    req.flush({ items: [{ id: 1 }] });
    expect(await result).toEqual([{ id: 1 }]);

    api.obligations({ status: 'active', category: '', due_within_days: 7 }).subscribe();
    const o = backend.expectOne((r) => r.url === '/api/v1/obligations');
    expect(o.request.params.keys().sort()).toEqual(['due_within_days', 'status']);
    o.flush({ today: '2026-10-19', items: [] });
  });

  it('posts actions with their payload', () => {
    api
      .requestAction({ kind: 'snooze', obligation_id: 4, payload: { until: '2026-10-26' } })
      .subscribe();
    const req = backend.expectOne('/api/v1/actions');
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({
      kind: 'snooze',
      obligation_id: 4,
      payload: { until: '2026-10-26' },
    });
    req.flush({ id: 1 });
  });

  it('streams chat events with the bearer token', async () => {
    TestBed.inject(Session).start('stream-token-0123456789');
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        sseResponse(
          'event: conversation\ndata: {"id":3,"title":"t"}\n\n: ping\n\n' +
            'event: tool_call\ndata: {"name":"recall","arguments":"{}"}\n\n' +
            'event: reply\ndata: {"conversation":{"id":3},"reply":"Done","steps":[],"usage":{"prompt_tokens":1,"completion_tokens":2},"cost_usd":0}\n\n',
        ),
      );
    vi.stubGlobal('fetch', fetchMock);

    const events = await firstValueFrom(api.chat('hello', 3).pipe(toArray()));
    expect(events.map((e) => e.type)).toEqual(['conversation', 'tool_call', 'reply']);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/v1/chat');
    expect((init.headers as Record<string, string>)['Authorization']).toBe(
      'Bearer stream-token-0123456789',
    );
    expect(JSON.parse(init.body as string)).toEqual({ message: 'hello', conversation_id: 3 });
  });

  it('turns an HTTP error before the stream into an error event', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: { code: 'conversation_busy', message: 'busy' } }), {
          status: 409,
        }),
      ),
    );
    const events = await firstValueFrom(api.chat('again').pipe(toArray()));
    expect(events).toEqual([{ type: 'error', code: 'conversation_busy', message: 'busy' }]);
  });

  it('extracts readable error messages', () => {
    expect(
      errorMessage(
        new HttpErrorResponse({
          status: 400,
          error: { error: { code: 'invalid_request', message: 'title is required' } },
        }),
      ),
    ).toBe('title is required');
    expect(errorMessage(new HttpErrorResponse({ status: 0 }))).toContain('not reachable');
    expect(errorMessage(new HttpErrorResponse({ status: 502, error: 'bad gateway' }))).toBe(
      'The server returned HTTP 502.',
    );
    expect(errorMessage(new Error('boom'))).toBe('boom');
  });
});
