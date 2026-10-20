import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { Subscription } from 'rxjs';
import { Api, errorMessage } from '../../core/api';
import { Segment, linkify } from '../../core/format';
import { ChatEvent, Conversation, Step } from '../../core/models';
import { Notices } from '../../core/notices';
import { AgoPipe } from '../../core/pipes';
import { AgentTurn, LiveStep, Turn, pretty, stepLabel, transcript } from '../../core/transcript';

const SUGGESTIONS = [
  'My electricity bill of 2,340.50 is due on the 21st, every month.',
  'What is due in the next two weeks?',
  'Remember that my car insurance is with Acko.',
  'Search the web for this year’s ITR filing deadline and save it.',
];

@Component({
  selector: 'll-chat',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule, RouterLink, AgoPipe],
  templateUrl: './chat.html',
  styleUrl: './chat.css',
})
export class Chat {
  private readonly api = inject(Api);
  private readonly router = inject(Router);
  private readonly notices = inject(Notices);
  private readonly log = viewChild<ElementRef<HTMLElement>>('log');

  readonly id = input<string>();
  protected readonly conversations = signal<Conversation[]>([]);
  protected readonly turns = signal<Turn[]>([]);
  protected readonly draft = signal('');
  protected readonly streaming = signal(false);
  protected readonly loading = signal(false);
  protected readonly current = signal<number | null>(null);
  protected readonly suggestions = SUGGESTIONS;
  protected readonly title = computed(
    () => this.conversations().find((c) => c.id === this.current())?.title ?? 'New conversation',
  );
  protected readonly label = stepLabel;
  protected readonly pretty = pretty;

  private stream: Subscription | null = null;

  constructor() {
    this.loadList();
    effect(() => {
      const raw = this.id();
      untracked(() => this.open(raw ? Number(raw) : null));
    });
    effect(() => {
      this.turns();
      const el = this.log()?.nativeElement;
      if (el) {
        queueMicrotask(() => (el.scrollTop = el.scrollHeight));
      }
    });
    inject(DestroyRef).onDestroy(() => this.stream?.unsubscribe());
  }

  protected segments(text: string): Segment[] {
    return linkify(text);
  }

  protected isAgent(turn: Turn): turn is AgentTurn {
    return turn.role === 'assistant';
  }

  protected use(suggestion: string): void {
    this.draft.set(suggestion);
  }

  protected keydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      this.send();
    }
  }

  protected send(): void {
    const text = this.draft().trim();
    if (!text || this.streaming()) {
      return;
    }
    this.draft.set('');
    this.streaming.set(true);
    const live: AgentTurn = { role: 'assistant', text: '', steps: [], pending: true };
    this.turns.update((t) => [...t, { role: 'user', text }, live]);

    this.stream = this.api.chat(text, this.current() ?? undefined).subscribe({
      next: (event) => this.apply(event),
      complete: () => {
        this.streaming.set(false);
        this.patchLive((turn) =>
          turn.pending
            ? {
                ...turn,
                pending: false,
                error:
                  turn.error ??
                  'The reply stopped. Open the conversation again to see the saved result.',
              }
            : turn,
        );
        this.loadList();
      },
    });
  }

  protected newChat(): void {
    void this.router.navigate(['/chat']);
  }

  protected remove(c: Conversation, event: Event): void {
    event.preventDefault();
    event.stopPropagation();
    if (!confirm(`Delete the conversation “${c.title}”?`)) {
      return;
    }
    this.api.deleteConversation(c.id).subscribe({
      next: () => {
        this.conversations.update((list) => list.filter((x) => x.id !== c.id));
        if (this.current() === c.id) {
          this.newChat();
        }
      },
      error: (err: unknown) => this.notices.error(errorMessage(err)),
    });
  }

  private apply(event: ChatEvent): void {
    switch (event.type) {
      case 'conversation':
        if (this.current() !== event.conversation.id) {
          this.current.set(event.conversation.id);
          void this.router.navigate(['/chat', event.conversation.id], { replaceUrl: true });
        }
        break;
      case 'tool_call':
        this.patchLive((turn) => ({
          ...turn,
          steps: [...turn.steps, { ...event.step, done: false }],
        }));
        break;
      case 'tool_result':
        this.patchLive((turn) => ({ ...turn, steps: settle(turn.steps, event.step) }));
        break;
      case 'reply':
        this.patchLive((turn) => ({
          ...turn,
          text: event.reply.reply,
          pending: false,
          cost: event.reply.cost_usd,
          tokens: event.reply.usage.prompt_tokens + event.reply.usage.completion_tokens,
        }));
        break;
      case 'error':
        this.patchLive((turn) => ({ ...turn, pending: false, error: event.message }));
        break;
    }
  }

  private patchLive(fn: (turn: AgentTurn) => AgentTurn): void {
    this.turns.update((turns) => {
      const last = turns[turns.length - 1];
      if (!last || last.role !== 'assistant') {
        return turns;
      }
      return [...turns.slice(0, -1), fn(last)];
    });
  }

  private open(id: number | null): void {
    if (id !== null && id === this.current() && this.streaming()) {
      return;
    }
    this.stream?.unsubscribe();
    this.streaming.set(false);
    this.current.set(id);
    if (id === null || Number.isNaN(id)) {
      this.turns.set([]);
      return;
    }
    this.loading.set(true);
    this.api.conversation(id).subscribe({
      next: (c) => {
        this.loading.set(false);
        this.turns.set(transcript(c.messages));
      },
      error: (err: unknown) => {
        this.loading.set(false);
        this.notices.error(errorMessage(err));
        this.newChat();
      },
    });
  }

  private loadList(): void {
    this.api.conversations(50).subscribe({
      next: (list) => this.conversations.set(list),
      error: (err: unknown) => this.notices.error(errorMessage(err)),
    });
  }
}

function settle(steps: LiveStep[], result: Step): LiveStep[] {
  const i = steps.findIndex(
    (s) => !s.done && s.name === result.name && s.arguments === result.arguments,
  );
  const settled: LiveStep = { ...result, done: true };
  if (i < 0) {
    return [...steps, settled];
  }
  return [...steps.slice(0, i), settled, ...steps.slice(i + 1)];
}
