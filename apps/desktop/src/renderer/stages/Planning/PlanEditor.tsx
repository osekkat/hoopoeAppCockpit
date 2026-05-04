import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { markdown } from "@codemirror/lang-markdown";
import { syntaxHighlighting, defaultHighlightStyle } from "@codemirror/language";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, keymap, lineNumbers } from "@codemirror/view";
import { Lock, Save } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

interface PlanEditorProps {
  readonly artifactPath: string;
  readonly initialContent: string;
  readonly readOnly: boolean;
  readonly readOnlyReason?: string | undefined;
  readonly onSave?: ((next: string) => void) | undefined;
}

interface SaveHandlerRef {
  current: () => void;
}

export function planEditorSaveKeyBinding(saveHandlerRef: SaveHandlerRef) {
  return {
    key: "Mod-s",
    run: () => {
      saveHandlerRef.current();
      return true;
    },
  };
}

export function PlanEditor({
  artifactPath,
  initialContent,
  readOnly,
  readOnlyReason,
  onSave,
}: PlanEditorProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const readOnlyCompartment = useMemo(() => new Compartment(), []);
  const [dirty, setDirty] = useState(false);
  const initialContentRef = useRef(initialContent);
  const saveHandlerRef = useRef<() => void>(() => undefined);

  const handleSave = useCallback(() => {
    if (!viewRef.current || !onSave || readOnly) return;
    const next = viewRef.current.state.doc.toString();
    onSave(next);
    initialContentRef.current = next;
    setDirty(false);
  }, [onSave, readOnly]);
  saveHandlerRef.current = handleSave;

  useEffect(() => {
    if (!hostRef.current) return;

    const updateListener = EditorView.updateListener.of((update) => {
      if (update.docChanged) {
        setDirty(update.state.doc.toString() !== initialContentRef.current);
      }
    });

    const state = EditorState.create({
      doc: initialContent,
      extensions: [
        lineNumbers(),
        history(),
        keymap.of([
          ...defaultKeymap,
          ...historyKeymap,
          planEditorSaveKeyBinding(saveHandlerRef),
        ]),
        markdown(),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        EditorView.lineWrapping,
        updateListener,
        readOnlyCompartment.of(EditorState.readOnly.of(readOnly)),
        EditorView.theme({
          "&": { height: "100%", fontSize: "13px" },
          ".cm-scroller": { fontFamily: "var(--hh-font-mono)", overflow: "auto" },
          ".cm-content": { padding: "16px 14px" },
          ".cm-gutters": {
            backgroundColor: "transparent",
            borderRight: "1px solid var(--hh-border-subtle)",
            color: "var(--hh-text-muted)",
          },
          ".cm-activeLineGutter": { backgroundColor: "transparent" },
          ".cm-line": { padding: "0 8px" },
        }),
      ],
    });

    const view = new EditorView({ state, parent: hostRef.current });
    viewRef.current = view;
    initialContentRef.current = initialContent;
    setDirty(false);

    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [artifactPath]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({
      effects: readOnlyCompartment.reconfigure(EditorState.readOnly.of(readOnly)),
    });
  }, [readOnly, readOnlyCompartment]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    if (view.state.doc.toString() === initialContent) return;
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: initialContent },
    });
    initialContentRef.current = initialContent;
    setDirty(false);
  }, [initialContent]);

  return (
    <div className="hh-plan-editor" data-testid="plan-editor">
      <div className="hh-plan-editor-toolbar">
        <span className="hh-plan-editor-path">{artifactPath}</span>
        {readOnly ? (
          <span className="hh-plan-editor-lock" title={readOnlyReason ?? "Read-only"}>
            <Lock size={13} strokeWidth={2.1} />
            <span>{readOnlyReason ?? "Read-only"}</span>
          </span>
        ) : (
          <button
            className="hh-plan-editor-save"
            type="button"
            onClick={handleSave}
            disabled={!dirty}
            data-testid="plan-editor-save"
          >
            <Save size={13} strokeWidth={2.1} />
            <span>{dirty ? "Save (⌘S)" : "Saved"}</span>
          </button>
        )}
      </div>
      <div ref={hostRef} className="hh-plan-editor-host" data-testid="plan-editor-host" />
    </div>
  );
}
