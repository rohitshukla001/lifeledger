import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { Session } from '../../core/session';
import { Unlock } from './unlock';

describe('Unlock', () => {
  let backend: HttpTestingController;
  let navigate: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({
      imports: [Unlock],
      providers: [provideRouter([]), provideHttpClient(), provideHttpClientTesting()],
    });
    backend = TestBed.inject(HttpTestingController);
    navigate = vi.spyOn(TestBed.inject(Router), 'navigateByUrl').mockResolvedValue(true);
  });

  it('opens at once when the server needs no token', async () => {
    const fixture = TestBed.createComponent(Unlock);
    await fixture.whenStable();
    backend.expectOne('/api/v1/overview').flush({});
    expect(TestBed.inject(Session).active()).toBe(true);
    expect(navigate).toHaveBeenCalledWith('/today');
  });

  it('checks the token before it saves it', async () => {
    const fixture = TestBed.createComponent(Unlock);
    fixture.componentRef.setInput('returnTo', '/ledger');
    await fixture.whenStable();
    backend.expectOne('/api/v1/overview').flush({}, { status: 401, statusText: 'Unauthorized' });
    await fixture.whenStable();
    const el = fixture.nativeElement as HTMLElement;

    const input = el.querySelector('input')!;
    input.value = 'wrong-token';
    input.dispatchEvent(new Event('input'));
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    const bad = backend.expectOne('/api/v1/overview');
    expect(bad.request.headers.get('Authorization')).toBe('Bearer wrong-token');
    bad.flush({}, { status: 401, statusText: 'Unauthorized' });
    await fixture.whenStable();
    expect(el.querySelector('.banner.error')?.textContent).toContain('did not accept');
    expect(TestBed.inject(Session).active()).toBe(false);

    input.value = 'good-token-0123456789';
    input.dispatchEvent(new Event('input'));
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    backend.expectOne('/api/v1/overview').flush({});
    expect(TestBed.inject(Session).token()).toBe('good-token-0123456789');
    expect(navigate).toHaveBeenCalledWith('/ledger');
  });
});
