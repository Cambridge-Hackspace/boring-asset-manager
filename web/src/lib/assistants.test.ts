import { describe, expect, it } from 'vitest';
import { assistantSetup } from './assistants';

describe('assistantSetup', () => {
  it('connects each kind of assistant to the MCP endpoint with the token', () => {
    const steps = assistantSetup('https://bam.example.org/', 'bam_secret');
    expect(steps.map((s) => s.label)).toEqual(['Claude Code', 'Claude Desktop, and assistants that start programs', 'Any other assistant']);
    expect(steps[0]!.text).toBe('claude mcp add --transport http bam https://bam.example.org/api/v1/mcp --header "Authorization: Bearer bam_secret"');
    expect(JSON.parse(steps[1]!.text)).toEqual({
      mcpServers: { bam: { command: 'bam', args: ['mcp', '--url', 'https://bam.example.org'], env: { BAM_TOKEN: 'bam_secret' } } },
    });
    expect(steps[2]!.text).toContain('https://bam.example.org/api/v1/mcp');
  });
});
