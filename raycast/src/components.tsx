import { ActionPanel, Action, Icon, List, Color } from "@raycast/api";
import { relativeTime } from "./sesh";
import { openInTerminal } from "./terminal";
import { SeshSession } from "./types";

const AGENT_COLORS: Record<string, Color> = {
  opencode: Color.Blue,
  claude: Color.Purple,
};

export function agentColor(agent: string): Color {
  return AGENT_COLORS[agent] ?? Color.Yellow;
}

export function abbreviateHome(path: string): string {
  const home = process.env.HOME;
  if (home && path.startsWith(home)) {
    return "~" + path.slice(home.length);
  }
  return path;
}

export function displayTitle(session: SeshSession): string {
  return session.summary || session.title || session.slug || session.id;
}

export function sessionDetailMarkdown(
  session: SeshSession,
  sessionText?: string
): string {
  const lines: string[] = [];

  // Compact header: title, then one-line metadata.
  lines.push(`## ${displayTitle(session)}`);
  lines.push("");

  const meta: string[] = [`**${session.agent}**`];
  meta.push(relativeTime(session.last_used));
  if (session.directory) {
    meta.push(`\`${abbreviateHome(session.directory)}\``);
  }
  lines.push(meta.join("  ·  "));

  if (
    session.title &&
    session.summary &&
    session.title !== session.summary
  ) {
    lines.push("");
    lines.push(`*${session.title}*`);
  }

  if (sessionText) {
    lines.push("");
    lines.push("---");
    lines.push("");
    lines.push(sessionText);
  }

  return lines.join("\n");
}

export function SessionActions({
  session,
  showDetail,
  onToggleDetail,
  extraActions,
}: {
  session: SeshSession;
  showDetail?: boolean;
  onToggleDetail?: () => void;
  extraActions?: React.ReactNode;
}) {
  return (
    <ActionPanel>
      <ActionPanel.Section title="Resume">
        <Action
          title="Resume Session"
          icon={Icon.Terminal}
          onAction={() => openInTerminal(session.resume_command)}
        />
        {onToggleDetail && (
          <Action
            title={showDetail ? "Hide Detail" : "Show Detail"}
            icon={Icon.Sidebar}
            shortcut={{ modifiers: ["cmd"], key: "d" }}
            onAction={onToggleDetail}
          />
        )}
      </ActionPanel.Section>
      {extraActions}
      <ActionPanel.Section title="Copy">
        <Action.CopyToClipboard
          title="Copy Resume Command"
          content={session.resume_command}
          shortcut={{ modifiers: ["cmd", "shift"], key: "c" }}
        />
        <Action.CopyToClipboard
          title="Copy Session ID"
          content={session.id}
          shortcut={{ modifiers: ["cmd"], key: "i" }}
        />
        {session.directory && (
          <Action.CopyToClipboard title="Copy Directory" content={session.directory} />
        )}
      </ActionPanel.Section>
      {session.directory && (
        <ActionPanel.Section title="Open">
          <Action.ShowInFinder
            title="Open Directory in Finder"
            path={session.directory}
            shortcut={{ modifiers: ["cmd"], key: "o" }}
          />
          <Action.Open
            title="Open Directory in VS Code"
            target={session.directory}
            application="Visual Studio Code"
            shortcut={{ modifiers: ["cmd", "shift"], key: "o" }}
          />
        </ActionPanel.Section>
      )}
    </ActionPanel>
  );
}

export function sessionListItemProps(
  session: SeshSession,
  showDetail: boolean,
  sessionText?: string
): Partial<List.Item.Props> {
  if (showDetail) {
    return {
      detail: (
        <List.Item.Detail
          markdown={sessionDetailMarkdown(session, sessionText)}
        />
      ),
    };
  }
  return {
    subtitle: session.directory
      ? abbreviateHome(session.directory)
      : undefined,
    accessories: [
      { tag: { value: session.agent, color: agentColor(session.agent) } },
      { text: relativeTime(session.last_used) },
    ],
  };
}
