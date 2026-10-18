// How to connect an assistant to BAM!'s MCP endpoint (README §6.6), for the
// account page to show once a personal token is made.

export interface SetupStep {
  label: string;
  hint: string;
  text: string;
}

/** The ways to connect an assistant, each ready to paste. */
export function assistantSetup(baseURL: string, secret: string): SetupStep[] {
  const base = baseURL.replace(/\/+$/, '');
  const endpoint = `${base}/api/v1/mcp`;
  const desktop = {
    mcpServers: {
      bam: { command: 'bam', args: ['mcp', '--url', base], env: { BAM_TOKEN: secret } },
    },
  };
  return [
    {
      label: 'Claude Code',
      hint: 'Run this in a terminal.',
      text: `claude mcp add --transport http bam ${endpoint} --header "Authorization: Bearer ${secret}"`,
    },
    {
      label: 'Claude Desktop, and assistants that start programs',
      hint: 'Add this to the assistant’s MCP configuration. It runs bam mcp, so the bam program must be installed.',
      text: JSON.stringify(desktop, null, 2),
    },
    {
      label: 'Any other assistant',
      hint: 'Give it the MCP address and send the token as a header.',
      text: `${endpoint}\nAuthorization: Bearer ${secret}`,
    },
  ];
}
