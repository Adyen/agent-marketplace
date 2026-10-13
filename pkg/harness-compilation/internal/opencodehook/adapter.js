// Agent Marketplace OpenCode command-hook adapter, format version 1.
// Resource-authored commands live in JSON declarations, never in this module.
import { readdir, readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const limit = 1024 * 1024;
const canonicalTools = { bash: "Bash", read: "Read", write: "Write", edit: "Edit", glob: "Glob", grep: "Grep", webfetch: "WebFetch", websearch: "WebSearch", todowrite: "TodoWrite", task: "Task", skill: "Skill" };
const refusal = () => new Error("Agent Marketplace hook failed or blocked this tool operation");

function execute(command, cwd, input, timeout) {
  return new Promise((resolveResult, reject) => {
    const child = spawn("/bin/sh", ["-c", command], { cwd, detached: true, stdio: ["pipe", "pipe", "pipe"] });
    let bytes = 0;
    const output = [];
    let settled = false;
    const stop = () => { if (child.pid) { try { process.kill(-child.pid, "SIGKILL"); } catch {} } };
    const fail = () => { if (!settled) { settled = true; clearTimeout(timer); stop(); reject(refusal()); } };
    const timer = setTimeout(fail, timeout);
    child.on("error", fail);
    // A handler that stopped reading closes the input pipe, and the pending
    // write then fails with EPIPE. That alone is not a refusal: the handler's
    // exit status and its validated output decide success. Failing here would
    // reject every successful handler that declines stdin. Spawn failures are
    // reported on the child, timeouts on the timer, and both output streams
    // below still fail closed. The listener must exist so EPIPE never becomes
    // an unhandled process error.
    child.stdin.on("error", () => {});
    for (const stream of [child.stdout, child.stderr]) {
      stream.on("data", chunk => { bytes += chunk.length; if (bytes > limit) fail(); else if (stream === child.stdout) output.push(chunk); });
      stream.on("error", fail);
    }
    child.on("close", code => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      stop();
      if (code !== 0) { reject(refusal()); return; }
      try {
        const text = Buffer.concat(output).toString("utf8").trim();
        if (!text) { resolveResult(); return; }
        const result = JSON.parse(text);
        if (!result || typeof result !== "object" || Array.isArray(result)) throw refusal();
        if (result.continue === false || result.decision === "block") throw refusal();
        const specific = result.hookSpecificOutput;
        if (specific !== undefined) {
          if (!specific || specific.hookEventName !== "PreToolUse") throw refusal();
          if (specific.permissionDecision !== undefined && specific.permissionDecision !== "allow") throw refusal();
          if (specific.updatedInput !== undefined || specific.additionalContext !== undefined) throw refusal();
        }
        resolveResult();
      } catch { reject(refusal()); }
    });
    child.stdin.end(input);
  });
}

export const AgentMarketplace = async ({ directory }) => {
  const hooks = resolve(dirname(fileURLToPath(import.meta.url)), "../hooks/agent-marketplace");
  const load = async () => {
  const declarations = [];
  let names;
  try { names = await readdir(hooks); } catch (error) { if (error.code === "ENOENT") names = []; else throw refusal(); }
  for (const name of names.sort()) {
    if (!name.endsWith(".json")) continue;
    const data = await readFile(resolve(hooks, name));
    if (data.length > limit) throw refusal();
    const value = JSON.parse(data);
    if (value.format !== "agent-marketplace/opencode-hook-v1") continue;
    if (value.event !== "PreToolUse" || typeof value.command !== "string" || !value.command || !Number.isInteger(value.timeout) || value.timeout < 1 || value.timeout > 600) throw refusal();
    if (typeof value.matcher !== "string" && value.matcher !== undefined) throw refusal();
    declarations.push(value);
  }
  if (declarations.length > 64) throw refusal();
  return declarations;
  };
  return {
    "tool.execute.before": async (input, output) => {
      const declarations = await load();
      const tool = canonicalTools[input.tool] || input.tool;
      const payload = JSON.stringify({ hook_event_name: "PreToolUse", session_id: input.sessionID, tool_use_id: input.callID, cwd: directory, tool_name: tool, tool_input: output.args });
      if (Buffer.byteLength(payload) > limit) throw refusal();
      const deadline = Date.now() + 600000;
      for (const declaration of declarations) {
        if (declaration.matcher && declaration.matcher !== "*" && !declaration.matcher.split("|").includes(tool)) continue;
        const remaining = deadline - Date.now();
        if (remaining <= 0) throw refusal();
        await execute(declaration.command, directory, payload, Math.min(declaration.timeout * 1000, remaining));
      }
    }
  };
};
