// Package setuphelp renders MCP client setup instructions for the -help
// output of the servers in this module, so an agent (or human) reading the
// help can wire the server into its client without further research.
package setuphelp

import (
	"fmt"
	"io"
	"strings"
)

// EnvVar is one required environment variable with a placeholder value.
type EnvVar struct {
	Name        string
	Placeholder string
}

// Write prints compact setup instructions for OpenCode and pi, both of which
// launch stdio MCP servers. Binary paths must be absolute.
func Write(w io.Writer, serverName, binary string, envVars []EnvVar) {
	var opencodeEnv, piEnv, shellEnv []string
	for _, v := range envVars {
		opencodeEnv = append(opencodeEnv, fmt.Sprintf("            %q: %q", v.Name, v.Placeholder))
		piEnv = append(piEnv, fmt.Sprintf("            %q: %q", v.Name, v.Placeholder))
		shellEnv = append(shellEnv, fmt.Sprintf("--env %s=%s", v.Name, v.Placeholder))
	}

	fmt.Fprintf(w, `
Setup for MCP clients (stdio transport; use absolute binary paths):

  OpenCode - add to opencode.json (global or project):
    {
      "mcp": {
        %q: {
          "type": "local",
          "command": ["/absolute/path/to/%s"],
          "environment": {
%s
          }
        }
      }
    }

  pi - either run:
    pi mcp add %s %s -- /absolute/path/to/%s
  or add to ~/.pi/agent/mcp.json:
    {
      "mcpServers": {
        %q: {
          "command": "/absolute/path/to/%s",
          "env": {
%s
          }
        }
      }
    }

  Secrets can be referenced from the environment instead of being inlined:
  OpenCode uses "{env:NAME}", pi uses "${NAME}".
`, serverName, binary, strings.Join(opencodeEnv, ",\n"),
		serverName, strings.Join(shellEnv, " "), binary,
		serverName, binary, strings.Join(piEnv, ",\n"))
}
