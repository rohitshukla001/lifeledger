export interface SseFrame {
  event: string;
  data: string;
}

export class SseParser {
  private buffer = '';

  push(chunk: string): SseFrame[] {
    this.buffer += chunk.replace(/\r\n?/g, '\n');
    const frames: SseFrame[] = [];
    let end: number;
    while ((end = this.buffer.indexOf('\n\n')) >= 0) {
      const block = this.buffer.slice(0, end);
      this.buffer = this.buffer.slice(end + 2);
      const frame = parseBlock(block);
      if (frame) {
        frames.push(frame);
      }
    }
    return frames;
  }
}

function parseBlock(block: string): SseFrame | null {
  let event = 'message';
  const data: string[] = [];
  for (const line of block.split('\n')) {
    if (line === '' || line.startsWith(':')) {
      continue;
    }
    const colon = line.indexOf(':');
    const field = colon < 0 ? line : line.slice(0, colon);
    const value = colon < 0 ? '' : line.slice(colon + 1).replace(/^ /, '');
    if (field === 'event') {
      event = value;
    } else if (field === 'data') {
      data.push(value);
    }
  }
  return data.length ? { event, data: data.join('\n') } : null;
}
