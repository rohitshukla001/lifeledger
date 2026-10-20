import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';
import { Api } from '../../core/api';
import { ObligationForm } from './obligation-form';

describe('ObligationForm', () => {
  const created = { id: 9, title: 'Water bill' };
  let api: {
    createObligation: ReturnType<typeof vi.fn>;
    updateObligation: ReturnType<typeof vi.fn>;
  };

  beforeEach(() => {
    api = {
      createObligation: vi.fn(() => of(created)),
      updateObligation: vi.fn(() => of(created)),
    };
    TestBed.configureTestingModule({
      imports: [ObligationForm],
      providers: [{ provide: Api, useValue: api }],
    });
  });

  function setValue(el: HTMLElement, name: string, value: string): void {
    const input = el.querySelector<HTMLInputElement | HTMLSelectElement>(
      `[formControlName="${name}"]`,
    )!;
    input.value = value;
    input.dispatchEvent(new Event(input.tagName === 'SELECT' ? 'change' : 'input'));
  }

  it('validates before it saves', async () => {
    const fixture = TestBed.createComponent(ObligationForm);
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;

    setValue(el, 'amount', '12.345');
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(api.createObligation).not.toHaveBeenCalled();
    expect(el.textContent).toContain('A title is required.');
    expect(el.textContent).toContain('up to 2 decimals');
  });

  it('creates an entry with the default currency', async () => {
    const fixture = TestBed.createComponent(ObligationForm);
    fixture.componentRef.setInput('currency', 'INR');
    let saved: unknown;
    fixture.componentInstance.saved.subscribe((o) => (saved = o));
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;

    setValue(el, 'title', ' Water bill ');
    setValue(el, 'amount', '1,250.5');
    setValue(el, 'due_on', '2026-11-05');
    setValue(el, 'recurrence', 'monthly');
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(api.createObligation).toHaveBeenCalledWith({
      title: 'Water bill',
      category: 'bill',
      amount: '1,250.5',
      currency: 'INR',
      due_on: '2026-11-05',
      recurrence: 'monthly',
      notes: '',
    });
    expect(saved).toBe(created);
  });

  it('updates status and snooze date of an existing item', async () => {
    const fixture = TestBed.createComponent(ObligationForm);
    fixture.componentRef.setInput('obligation', {
      id: 4,
      title: 'Gym',
      category: 'subscription',
      recurrence: 'monthly',
      status: 'open',
      source: 'user',
      created_at: '',
      updated_at: '',
    });
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;

    setValue(el, 'status', 'snoozed');
    await fixture.whenStable();
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect(api.updateObligation).not.toHaveBeenCalled();
    expect(el.textContent).toContain('Choose the date when the snooze ends.');

    setValue(el, 'snoozed_until', '2026-11-01');
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect(api.updateObligation).toHaveBeenCalledWith(
      4,
      expect.objectContaining({ status: 'snoozed', snoozed_until: '2026-11-01', title: 'Gym' }),
    );
  });
});
