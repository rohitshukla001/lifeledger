import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, OnInit, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { errorMessage } from '../../core/api';
import { Session } from '../../core/session';

@Component({
  selector: 'll-unlock',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [FormsModule],
  templateUrl: './unlock.html',
  styleUrl: './unlock.css',
})
export class Unlock implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly session = inject(Session);
  private readonly router = inject(Router);

  readonly returnTo = input<string>();
  protected readonly token = signal('');
  protected readonly checking = signal(true);
  protected readonly error = signal<string | null>(null);

  ngOnInit(): void {
    this.http.get('/api/v1/overview').subscribe({
      next: () => this.enter(null),
      error: () => this.checking.set(false),
    });
  }

  protected submit(): void {
    const token = this.token().trim();
    if (!token) {
      this.error.set('Enter the API token from LIFELEDGER_API_TOKEN.');
      return;
    }
    this.checking.set(true);
    this.error.set(null);
    this.http.get('/api/v1/overview', { headers: { Authorization: `Bearer ${token}` } }).subscribe({
      next: () => this.enter(token),
      error: (err: unknown) => {
        this.checking.set(false);
        this.error.set(
          err instanceof HttpErrorResponse && err.status === 401
            ? 'The server did not accept this token.'
            : errorMessage(err),
        );
      },
    });
  }

  private enter(token: string | null): void {
    this.session.start(token);
    const target = this.returnTo();
    void this.router.navigateByUrl(
      target && target.startsWith('/') && !target.startsWith('/unlock') ? target : '/today',
    );
  }
}
