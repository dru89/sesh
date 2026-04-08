import { useState, useEffect } from "react";
import { Action, ActionPanel, Icon, List, showToast, Toast } from "@raycast/api";
import { useCachedPromise } from "@raycast/utils";
import { loadSessions, aiSearchSessions, getSessionText } from "./sesh";
import {
  agentColor,
  displayTitle,
  SessionActions,
  sessionListItemProps,
} from "./components";
import { SeshSession } from "./types";

export default function SearchSessions() {
  const [showDetail, setShowDetail] = useState(false);
  const [searchText, setSearchText] = useState("");
  const [aiResults, setAiResults] = useState<SeshSession[] | null>(null);
  const [aiLoading, setAiLoading] = useState(false);
  const [filteredEmpty, setFilteredEmpty] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [textCache, setTextCache] = useState<Record<string, string>>({});

  const { data: sessions, isLoading } = useCachedPromise(
    async () => loadSessions(),
    [],
    { keepPreviousData: true }
  );

  const displaySessions = aiResults ?? sessions ?? [];
  const isAiMode = aiResults !== null;

  // Async-load session text when the detail pane is showing and selection changes.
  useEffect(() => {
    if (!showDetail || !selectedId) return;
    const session = displaySessions.find(
      (s) => `${s.agent}-${s.id}` === selectedId
    );
    if (!session || textCache[session.id]) return;

    let cancelled = false;
    getSessionText(session.id).then((text) => {
      if (!cancelled && text) {
        setTextCache((prev) => ({ ...prev, [session.id]: text }));
      }
    });
    return () => { cancelled = true; };
  }, [selectedId, showDetail]);

  async function handleAiSearch() {
    if (!searchText.trim()) return;
    setAiLoading(true);

    const results = await aiSearchSessions(searchText);
    setAiResults(results);
    setAiLoading(false);
  }

  function handleSearchChange(text: string) {
    setSearchText(text);
    setFilteredEmpty(false);
    if (aiResults !== null) {
      setAiResults(null);
    }
  }

  function handleSelectionChange(id: string | null) {
    setSelectedId(id);
    if (id === null && searchText.length >= 3 && !isLoading && displaySessions.length > 0) {
      setFilteredEmpty(true);
    } else {
      setFilteredEmpty(false);
    }
  }

  return (
    <List
      isLoading={isLoading || aiLoading}
      isShowingDetail={showDetail}
      searchBarPlaceholder="Search sessions..."
      onSearchTextChange={handleSearchChange}
      onSelectionChange={handleSelectionChange}
      filtering={!isAiMode}
      navigationTitle={isAiMode ? "Search Sessions (AI)" : "Search Sessions"}
      actions={
        filteredEmpty ? (
          <ActionPanel>
            <Action
              title="Search with AI"
              icon={Icon.Stars}
              onAction={handleAiSearch}
            />
          </ActionPanel>
        ) : undefined
      }
    >
      {displaySessions.map((session) => (
        <List.Item
          key={`${session.agent}-${session.id}`}
          id={`${session.agent}-${session.id}`}
          title={displayTitle(session)}
          icon={{
            source: Icon.Terminal,
            tintColor: agentColor(session.agent),
          }}
          keywords={[
            session.agent,
            session.slug ?? "",
            session.directory ?? "",
            session.title,
            session.summary ?? "",
          ].filter(Boolean)}
          {...sessionListItemProps(session, showDetail, textCache[session.id])}
          actions={
            <SessionActions
              session={session}
              showDetail={showDetail}
              onToggleDetail={() => setShowDetail(!showDetail)}
              extraActions={
                <ActionPanel.Section>
                  <Action
                    title="Search with AI"
                    icon={Icon.Stars}
                    shortcut={{ modifiers: ["cmd", "shift"], key: "a" }}
                    onAction={handleAiSearch}
                  />
                </ActionPanel.Section>
              }
            />
          }
        />
      ))}
    </List>
  );
}
