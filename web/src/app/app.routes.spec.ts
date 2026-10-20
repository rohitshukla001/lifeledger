import { UrlSegment } from '@angular/router';
import { chatMatcher } from './app.routes';

const seg = (...paths: string[]) => paths.map((p) => new UrlSegment(p, {}));

describe('chatMatcher', () => {
  it('matches /chat and /chat/:id with one route so the page survives the first reply', () => {
    expect(chatMatcher(seg('chat'))?.posParams).toEqual({});
    expect(chatMatcher(seg('chat', '12'))?.posParams?.['id']?.path).toBe('12');
    expect(chatMatcher(seg('chat', '12', 'x'))).toBeNull();
    expect(chatMatcher(seg('ledger'))).toBeNull();
  });
});
