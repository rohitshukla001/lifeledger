import {
  ChangeDetectionStrategy,
  Component,
  effect,
  inject,
  input,
  output,
  signal,
} from '@angular/core';
import { NonNullableFormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { Api, errorMessage } from '../../core/api';
import { CATEGORIES, Obligation, ObligationInput, RECURRENCES } from '../../core/models';

const AMOUNT = /^\d[\d,]*(\.\d{1,2})?$/;

type Checked = 'title' | 'amount' | 'currency';

@Component({
  selector: 'll-obligation-form',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ReactiveFormsModule],
  templateUrl: './obligation-form.html',
  styleUrl: './obligation-form.css',
})
export class ObligationForm {
  private readonly api = inject(Api);
  private readonly fb = inject(NonNullableFormBuilder);

  readonly obligation = input<Obligation | null>(null);
  readonly currency = input('INR');
  readonly saved = output<Obligation>();
  readonly cancelled = output();

  protected readonly categories = CATEGORIES;
  protected readonly recurrences = RECURRENCES;
  protected readonly saving = signal(false);
  protected readonly error = signal<string | null>(null);

  protected readonly form = this.fb.group({
    title: ['', [Validators.required, Validators.maxLength(200)]],
    category: this.fb.control<Obligation['category']>('bill'),
    amount: ['', Validators.pattern(AMOUNT)],
    currency: ['', [Validators.pattern(/^[A-Za-z]{3}$/)]],
    due_on: [''],
    recurrence: this.fb.control<Obligation['recurrence']>('none'),
    status: this.fb.control<Obligation['status']>('open'),
    snoozed_until: [''],
    notes: ['', Validators.maxLength(2000)],
  });

  constructor() {
    effect(() => {
      const o = this.obligation();
      this.form.reset({
        title: o?.title ?? '',
        category: o?.category ?? 'bill',
        amount: o?.amount ?? '',
        currency: o?.currency ?? this.currency(),
        due_on: o?.due_on ?? '',
        recurrence: o?.recurrence ?? 'none',
        status: o?.status ?? 'open',
        snoozed_until: o?.snoozed_until ?? '',
        notes: o?.notes ?? '',
      });
    });
  }

  protected invalid(name: Checked): boolean {
    const c = this.form.controls[name];
    return c.invalid && (c.dirty || c.touched);
  }

  protected submit(): void {
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    const v = this.form.getRawValue();
    if (v.status === 'snoozed' && !v.snoozed_until) {
      this.error.set('Choose the date when the snooze ends.');
      return;
    }
    const input: ObligationInput = {
      title: v.title.trim(),
      category: v.category,
      amount: v.amount.trim(),
      currency: v.currency.trim().toUpperCase(),
      due_on: v.due_on,
      recurrence: v.recurrence,
      notes: v.notes.trim(),
    };
    const existing = this.obligation();
    if (existing) {
      input.status = v.status;
      input.snoozed_until = v.status === 'snoozed' ? v.snoozed_until : '';
    }
    this.saving.set(true);
    this.error.set(null);
    const req = existing
      ? this.api.updateObligation(existing.id, input)
      : this.api.createObligation(input);
    req.subscribe({
      next: (o) => {
        this.saving.set(false);
        this.saved.emit(o);
      },
      error: (err: unknown) => {
        this.saving.set(false);
        this.error.set(errorMessage(err));
      },
    });
  }
}
