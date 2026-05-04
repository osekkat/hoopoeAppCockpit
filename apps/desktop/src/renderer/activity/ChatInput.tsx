// Hoopoe-owned. User → orchestrator chat input pinned to the bottom of
// the Activity drawer. Submission is delegated upstream — the actual
// orchestrator-chat tending agent wiring lands in hp-tg6. This
// component handles textarea resizing, ⏎ to send (⇧⏎ for newline), and
// disables submit when empty.

import { useCallback, useState, type KeyboardEvent } from "react";

export interface ChatInputProps {
  readonly placeholder?: string;
  readonly disabled?: boolean;
  readonly onSubmit: (text: string) => void;
}

export function ChatInput({ placeholder, disabled, onSubmit }: ChatInputProps) {
  const [value, setValue] = useState("");

  const submit = useCallback(() => {
    const trimmed = value.trim();
    if (!trimmed) return;
    onSubmit(trimmed);
    setValue("");
  }, [onSubmit, value]);

  const onKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key !== "Enter") return;
      if (e.shiftKey) return; // newline
      e.preventDefault();
      submit();
    },
    [submit],
  );

  return (
    <form
      aria-label="Send message to orchestrator"
      className="hh-activity-chat"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <textarea
        aria-label="Message to orchestrator"
        className="hh-activity-chat-input"
        disabled={disabled}
        onChange={(e) => setValue(e.currentTarget.value)}
        onKeyDown={onKeyDown}
        placeholder={placeholder ?? "Message orchestrator-chat (⏎ to send, ⇧⏎ for newline)"}
        rows={2}
        value={value}
      />
      <button
        aria-label="Send"
        className="hh-text-button"
        disabled={disabled || value.trim().length === 0}
        type="submit"
      >
        Send
      </button>
    </form>
  );
}
