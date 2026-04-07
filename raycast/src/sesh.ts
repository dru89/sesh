import { exec, execSync } from "child_process";
import { getPreferenceValues, showToast, Toast } from "@raycast/api";
import { SeshSession } from "./types";

export function getSeshPath(): string {
  const prefs = getPreferenceValues<{ seshPath?: string }>();
  return prefs.seshPath || "sesh";
}

function seshEnv(): NodeJS.ProcessEnv {
  return {
    ...process.env,
    SESH_DEBUG: "1",
    PATH: [
      process.env.PATH,
      "/usr/local/bin",
      "/opt/homebrew/bin",
      `${process.env.HOME}/.local/bin`,
      `${process.env.HOME}/go/bin`,
      `${process.env.HOME}/.opencode/bin`,
    ].join(":"),
  };
}

export function loadSessions(): SeshSession[] {
  const sesh = getSeshPath();
  try {
    const output = execSync(`${sesh} --json`, {
      timeout: 10000,
      encoding: "utf-8",
      shell: "/bin/bash",
      env: seshEnv(),
    });
    return JSON.parse(output);
  } catch (err) {
    showToast({
      style: Toast.Style.Failure,
      title: "Failed to load sessions",
      message: err instanceof Error ? err.message : String(err),
    });
    return [];
  }
}

// aiSearchSessions is async so the loading indicator actually renders.
// execSync would block the event loop and prevent Raycast from updating the UI.
export async function aiSearchSessions(query: string): Promise<SeshSession[]> {
  const sesh = getSeshPath();

  await showToast({
    style: Toast.Style.Animated,
    title: "Searching with AI...",
  });

  return new Promise((resolve) => {
    const cmd = `${sesh} --json --ai-search ${shellQuote(query)}`;
    exec(
      cmd,
      {
        timeout: 30000,
        maxBuffer: 10 * 1024 * 1024, // 10MB — session list can be large
        encoding: "utf-8",
        shell: "/bin/bash",
        env: seshEnv(),
      },
      (err, stdout, stderr) => {
        if (err) {
          const msg = stderr?.trim() || err.message;
          showToast({
            style: Toast.Style.Failure,
            title: "AI search failed",
            message: msg.slice(0, 200),
          });
          resolve([]);
          return;
        }
        try {
          const parsed = JSON.parse(stdout);
          const results: SeshSession[] = Array.isArray(parsed) ? parsed : [];
          if (results.length === 0) {
            const hint = stderr?.trim();
            showToast({
              style: Toast.Style.Failure,
              title: "No relevant sessions found",
              message: hint ? hint.slice(0, 200) : undefined,
            });
          } else {
            showToast({
              style: Toast.Style.Success,
              title: `Found ${results.length} session${results.length === 1 ? "" : "s"}`,
            });
          }
          resolve(results);
        } catch (parseErr) {
          showToast({
            style: Toast.Style.Failure,
            title: "Failed to parse AI search results",
            message: `stdout length: ${stdout?.length ?? 0}, first 100 chars: ${(stdout ?? "").slice(0, 100)}`,
          });
          resolve([]);
        }
        }
      }
    );
  });
}

export function relativeTime(isoDate: string): string {
  const d = Date.now() - new Date(isoDate).getTime();
  if (d < 60_000) return "just now";
  if (d < 3_600_000) return `${Math.floor(d / 60_000)}m ago`;
  if (d < 86_400_000) return `${Math.floor(d / 3_600_000)}h ago`;
  if (d < 30 * 86_400_000) return `${Math.floor(d / 86_400_000)}d ago`;
  return new Date(isoDate).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

function shellQuote(s: string): string {
  return "'" + s.replace(/'/g, "'\\''") + "'";
}
