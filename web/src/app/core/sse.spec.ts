import { SseParser } from './sse';

describe('SseParser', () => {
  it('parses frames split across chunks', () => {
    const p = new SseParser();
    expect(p.push('event: tool_call\ndata: {"name":')).toEqual([]);
    expect(p.push('"recall"}\n\nevent: reply\r\ndata: {}\r\n\r\n')).toEqual([
      { event: 'tool_call', data: '{"name":"recall"}' },
      { event: 'reply', data: '{}' },
    ]);
  });

  it('skips comments and joins multi-line data', () => {
    const p = new SseParser();
    expect(p.push(': ping\n\ndata: line one\ndata: line two\n\n')).toEqual([
      { event: 'message', data: 'line one\nline two' },
    ]);
  });
});
