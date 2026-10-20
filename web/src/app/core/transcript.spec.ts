import { Message } from './models';
import { pretty, stepLabel, transcript } from './transcript';

describe('transcript', () => {
  it('groups tool calls and results under the agent turn', () => {
    const messages: Message[] = [
      { role: 'user', content: 'Track my rent' },
      {
        role: 'assistant',
        content: '',
        tool_calls: [
          { id: 'a', name: 'add_obligation', arguments: '{"title":"Rent"}' },
          { id: 'b', name: 'recall', arguments: '{}' },
        ],
      },
      { role: 'tool', tool_call_id: 'a', content: '{"id":1}' },
      { role: 'tool', tool_call_id: 'b', content: '{"error":"no memory"}' },
      { role: 'assistant', content: 'Saved your rent.' },
      { role: 'user', content: 'Thanks' },
      { role: 'assistant', content: 'You are welcome.' },
    ];
    const turns = transcript(messages);
    expect(turns.map((t) => t.role)).toEqual(['user', 'assistant', 'user', 'assistant']);
    const agent = turns[1];
    if (agent.role !== 'assistant') throw new Error('want an agent turn');
    expect(agent.text).toBe('Saved your rent.');
    expect(agent.steps).toEqual([
      {
        name: 'add_obligation',
        arguments: '{"title":"Rent"}',
        result: '{"id":1}',
        done: true,
        failed: false,
      },
      {
        name: 'recall',
        arguments: '{}',
        result: '{"error":"no memory"}',
        done: true,
        failed: true,
      },
    ]);
  });

  it('names tools for people', () => {
    expect(stepLabel('web_search')).toBe('Searched the web');
    expect(stepLabel('new_tool')).toBe('new tool');
    expect(pretty('{"a":1}')).toBe('{\n  "a": 1\n}');
    expect(pretty('not json')).toBe('not json');
  });
});
