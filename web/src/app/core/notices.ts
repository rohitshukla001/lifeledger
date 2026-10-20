import { Injectable, signal } from '@angular/core';

export interface Notice {
  id: number;
  text: string;
  tone: 'info' | 'error';
}

@Injectable({ providedIn: 'root' })
export class Notices {
  private next = 1;
  readonly items = signal<Notice[]>([]);

  info(text: string): void {
    this.push(text, 'info');
  }

  error(text: string): void {
    this.push(text, 'error');
  }

  dismiss(id: number): void {
    this.items.update((list) => list.filter((n) => n.id !== id));
  }

  private push(text: string, tone: Notice['tone']): void {
    const id = this.next++;
    this.items.update((list) => [...list.slice(-3), { id, text, tone }]);
    setTimeout(() => this.dismiss(id), tone === 'error' ? 7000 : 4000);
  }
}
