/**
 * Project Tasks Extension
 *
 * Project-specific build and format tools for the Go local-mcp server.
 */

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

export default function (pi: ExtensionAPI) {
  pi.registerTool({
    name: "build",
    label: "Build",
    description: "Build the Go project with go build",
    promptSnippet: "Build the Go project with go build",
    parameters: Type.Object({}),
    async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
      const result = await pi.exec("go", ["build", "-o", "bin/local-mcp", "."], {
        cwd: ctx.cwd,
      });
      return {
        content: [
          {
            type: "text",
            text: result.stdout || result.stderr || `exit code ${result.code}`,
          },
        ],
        details: { exitCode: result.code },
      };
    },
  });

  pi.registerTool({
    name: "format",
    label: "Format",
    description: "Format Go source files with gofmt",
    promptSnippet: "Format Go source files with gofmt",
    parameters: Type.Object({}),
    async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
      const result = await pi.exec("gofmt", ["-w", "."], {
        cwd: ctx.cwd,
      });
      return {
        content: [
          {
            type: "text",
            text: result.stdout || result.stderr || `exit code ${result.code}`,
          },
        ],
        details: { exitCode: result.code },
      };
    },
  });
}