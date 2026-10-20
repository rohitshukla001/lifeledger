import { Injectable, computed, signal } from '@angular/core';

const TOKEN_KEY = 'lifeledger.token';
const OPEN_MODE = '-';

@Injectable({ providedIn: 'root' })
export class Session {
  private readonly stored = signal<string | null>(read());

  readonly active = computed(() => this.stored() !== null);
  readonly token = computed(() => {
    const value = this.stored();
    return value === null || value === OPEN_MODE ? null : value;
  });

  start(token: string | null): void {
    const value = token?.trim() || OPEN_MODE;
    this.stored.set(value);
    write(value);
  }

  end(): void {
    this.stored.set(null);
    write(null);
  }
}

function read(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

function write(value: string | null): void {
  try {
    if (value === null) {
      localStorage.removeItem(TOKEN_KEY);
    } else {
      localStorage.setItem(TOKEN_KEY, value);
    }
  } catch {
    return;
  }
}
