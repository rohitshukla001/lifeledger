import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { Subject, of } from 'rxjs';
import { Api } from '../../core/api';
import { ChatEvent } from '../../core/models';
import { OverviewState } from '../../core/overview-state';
import { Chat } from './chat';

describe('Chat', () => {
  let events: Subject<ChatEvent>;
  let api: Record<string, ReturnType<typeof vi.fn>>;
  let overview: { refresh: ReturnType<typeof vi.fn>; set: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    events = new Subject<ChatEvent>();
    api = {
      conversations: vi.fn(() => of([])),
      conversation: vi.fn(),
      chat: vi.fn(() => events),
      deleteConversation: vi.fn(),
    };
    overview = { refresh: vi.fn(), set: vi.fn() };
    TestBed.configureTestingModule({
      imports: [Chat],
      providers: [
        provideRouter([]),
        { provide: Api, useValue: api },
        { provide: OverviewState, useValue: overview },
      ],
    });
    vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
  });

  it('shows tool steps live and then the reply with links', async () => {
    const fixture = TestBed.createComponent(Chat);
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;

    const box = el.querySelector('textarea')!;
    box.value = 'Find the ITR deadline';
    box.dispatchEvent(new Event('input'));
    box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    await fixture.whenStable();

    expect(api['chat']).toHaveBeenCalledWith('Find the ITR deadline', undefined);
    expect(el.querySelector('.user')?.textContent).toContain('Find the ITR deadline');
    expect(el.querySelector('.thinking')).not.toBeNull();

    events.next({
      type: 'conversation',
      conversation: { id: 5, title: 'Find the ITR deadline', created_at: '', updated_at: '' },
    });
    events.next({
      type: 'tool_call',
      step: { name: 'web_search', arguments: '{"query":"ITR deadline"}' },
    });
    await fixture.whenStable();
    expect(el.querySelector('.steps li.running')?.textContent).toContain('Searched the web');

    events.next({
      type: 'tool_result',
      step: { name: 'web_search', arguments: '{"query":"ITR deadline"}', result: '{"results":[]}' },
    });
    events.next({
      type: 'reply',
      reply: {
        conversation: { id: 5, title: '', created_at: '', updated_at: '' },
        reply: 'It is 15 September. Source: https://incometax.gov.in/due-dates',
        steps: [],
        usage: { prompt_tokens: 100, completion_tokens: 20 },
        cost_usd: 0.0012,
      },
    });
    events.complete();
    await fixture.whenStable();

    expect(el.querySelector('.steps li.running')).toBeNull();
    expect(el.querySelector('.reply a')?.getAttribute('href')).toBe(
      'https://incometax.gov.in/due-dates',
    );
    expect(el.querySelector('.reply')?.textContent).toBe(
      'It is 15 September. Source: https://incometax.gov.in/due-dates',
    );
    expect(el.querySelector('.cost')?.textContent).toContain('120 tokens');
    expect(TestBed.inject(Router).navigate).toHaveBeenCalledWith(['/chat', 5], {
      replaceUrl: true,
    });
    expect(overview.refresh).toHaveBeenCalledOnce();
  });

  it('shows an error from the stream', async () => {
    const fixture = TestBed.createComponent(Chat);
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;
    const box = el.querySelector('textarea')!;
    box.value = 'hi';
    box.dispatchEvent(new Event('input'));
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    events.next({
      type: 'error',
      code: 'budget_exceeded',
      message: 'llm: daily spend limit reached',
    });
    events.complete();
    await fixture.whenStable();

    expect(el.querySelector('.agent .banner.error')?.textContent).toContain('daily spend limit');
    expect(el.querySelector('textarea')?.disabled).toBe(false);
  });
});
