import { HttpClient, HttpErrorResponse, HttpParams } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Router } from '@angular/router';
import { Observable, map } from 'rxjs';
import {
  Action,
  ActionRequest,
  ActionStatus,
  ApiErrorBody,
  AuditEntry,
  ChatEvent,
  ChatReply,
  Conversation,
  ConversationDetail,
  Ingestion,
  ListResponse,
  Memory,
  MemoryKind,
  Notification,
  Obligation,
  ObligationFilter,
  ObligationInput,
  ObligationList,
  Overview,
  Proposal,
  ProposalStatus,
  Step,
  SweepReport,
} from './models';
import { Session } from './session';
import { SseParser } from './sse';

const BASE = '/api/v1';

@Injectable({ providedIn: 'root' })
export class Api {
  private readonly http = inject(HttpClient);
  private readonly session = inject(Session);
  private readonly router = inject(Router);

  overview(): Observable<Overview> {
    return this.http.get<Overview>(`${BASE}/overview`);
  }

  obligations(filter: ObligationFilter = {}): Observable<ObligationList> {
    return this.http.get<ObligationList>(`${BASE}/obligations`, { params: params(filter) });
  }

  createObligation(input: ObligationInput): Observable<Obligation> {
    return this.http.post<Obligation>(`${BASE}/obligations`, input);
  }

  updateObligation(id: number, input: ObligationInput): Observable<Obligation> {
    return this.http.patch<Obligation>(`${BASE}/obligations/${id}`, input);
  }

  deleteObligation(id: number): Observable<void> {
    return this.http.delete<void>(`${BASE}/obligations/${id}`);
  }

  actions(status?: ActionStatus, limit = 50): Observable<Action[]> {
    return this.list<Action>(`${BASE}/actions`, { status, limit });
  }

  requestAction(req: ActionRequest): Observable<Action> {
    return this.http.post<Action>(`${BASE}/actions`, req);
  }

  approveAction(id: number): Observable<Action> {
    return this.http.post<Action>(`${BASE}/actions/${id}/approve`, null);
  }

  rejectAction(id: number): Observable<Action> {
    return this.http.post<Action>(`${BASE}/actions/${id}/reject`, null);
  }

  calendarFile(path: string): Observable<Blob> {
    return this.http.get(path, { responseType: 'blob' });
  }

  audit(limit = 100): Observable<AuditEntry[]> {
    return this.list<AuditEntry>(`${BASE}/audit`, { limit });
  }

  memories(limit = 200): Observable<Memory[]> {
    return this.list<Memory>(`${BASE}/memories`, { limit });
  }

  recall(q: string, limit = 8): Observable<Memory[]> {
    return this.list<Memory>(`${BASE}/memories/recall`, { q, limit });
  }

  remember(content: string, kind: MemoryKind): Observable<Memory> {
    return this.http.post<Memory>(`${BASE}/memories`, { content, kind });
  }

  editMemory(id: number, content: string): Observable<Memory> {
    return this.http.patch<Memory>(`${BASE}/memories/${id}`, { content });
  }

  forget(id: number): Observable<void> {
    return this.http.delete<void>(`${BASE}/memories/${id}`);
  }

  conversations(limit = 50): Observable<Conversation[]> {
    return this.list<Conversation>(`${BASE}/conversations`, { limit });
  }

  conversation(id: number): Observable<ConversationDetail> {
    return this.http.get<ConversationDetail>(`${BASE}/conversations/${id}`);
  }

  deleteConversation(id: number): Observable<void> {
    return this.http.delete<void>(`${BASE}/conversations/${id}`);
  }

  ingestText(name: string, text: string): Observable<Ingestion> {
    return this.http.post<Ingestion>(`${BASE}/ingest`, { name, text });
  }

  ingestFile(file: File): Observable<Ingestion> {
    const form = new FormData();
    form.append('file', file, file.name);
    return this.http.post<Ingestion>(`${BASE}/ingest`, form);
  }

  proposals(status: ProposalStatus | 'all' = 'pending'): Observable<Proposal[]> {
    return this.list<Proposal>(`${BASE}/proposals`, { status });
  }

  acceptProposal(id: number): Observable<Obligation> {
    return this.http.post<Obligation>(`${BASE}/proposals/${id}/accept`, null);
  }

  rejectProposal(id: number): Observable<void> {
    return this.http.post<void>(`${BASE}/proposals/${id}/reject`, null);
  }

  notifications(limit = 50): Observable<Notification[]> {
    return this.list<Notification>(`${BASE}/notifications`, { limit });
  }

  sweep(review: boolean): Observable<SweepReport> {
    return this.http.post<SweepReport>(`${BASE}/sweep`, { review });
  }

  chat(message: string, conversationId?: number): Observable<ChatEvent> {
    return new Observable<ChatEvent>((subscriber) => {
      const abort = new AbortController();
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
      };
      const token = this.session.token();
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }
      const body = JSON.stringify({ message, conversation_id: conversationId ?? 0 });

      const run = async () => {
        const res = await fetch(`${BASE}/chat`, {
          method: 'POST',
          headers,
          body,
          signal: abort.signal,
        });
        if (!res.ok || !res.body) {
          const err = await errorFromResponse(res);
          if (res.status === 401) {
            this.session.end();
            void this.router.navigate(['/unlock']);
          }
          subscriber.next({ type: 'error', ...err });
          subscriber.complete();
          return;
        }
        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
        const parser = new SseParser();
        for (;;) {
          const { value, done } = await reader.read();
          if (done) {
            break;
          }
          for (const frame of parser.push(value)) {
            const event = toChatEvent(frame.event, frame.data);
            if (event) {
              subscriber.next(event);
            }
          }
        }
        subscriber.complete();
      };

      run().catch((err: unknown) => {
        if (abort.signal.aborted) {
          return;
        }
        subscriber.next({ type: 'error', code: 'network', message: describe(err) });
        subscriber.complete();
      });
      return () => abort.abort();
    });
  }

  private list<T>(
    url: string,
    query: Record<string, string | number | undefined>,
  ): Observable<T[]> {
    return this.http
      .get<ListResponse<T>>(url, { params: params(query) })
      .pipe(map((res) => res.items));
  }
}

function params(query: object): HttpParams {
  let p = new HttpParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null && value !== '') {
      p = p.set(key, String(value));
    }
  }
  return p;
}

function toChatEvent(name: string, data: string): ChatEvent | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  switch (name) {
    case 'conversation':
      return { type: 'conversation', conversation: parsed as Conversation };
    case 'tool_call':
      return { type: 'tool_call', step: parsed as Step };
    case 'tool_result':
      return { type: 'tool_result', step: parsed as Step };
    case 'reply':
      return { type: 'reply', reply: parsed as ChatReply };
    case 'error': {
      const body = parsed as ApiErrorBody;
      return {
        type: 'error',
        code: body.error?.code ?? 'error',
        message: body.error?.message ?? 'The agent failed.',
      };
    }
  }
  return null;
}

async function errorFromResponse(res: Response): Promise<{ code: string; message: string }> {
  try {
    const body = (await res.json()) as ApiErrorBody;
    return { code: body.error.code, message: body.error.message };
  } catch {
    return { code: 'http_' + res.status, message: `The server returned HTTP ${res.status}.` };
  }
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : 'The connection failed.';
}

export function errorMessage(err: unknown): string {
  if (err instanceof HttpErrorResponse) {
    const body = err.error as Partial<ApiErrorBody> | null;
    if (body && typeof body === 'object' && body.error?.message) {
      return body.error.message;
    }
    if (err.status === 0) {
      return 'LifeLedger is not reachable. Check that the server runs.';
    }
    return `The server returned HTTP ${err.status}.`;
  }
  return describe(err);
}
