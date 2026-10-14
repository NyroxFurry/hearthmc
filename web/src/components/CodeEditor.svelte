<script lang="ts">
  import { onMount } from 'svelte';
  import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection } from '@codemirror/view';
  import { EditorState, type Extension } from '@codemirror/state';
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
  import { HighlightStyle, syntaxHighlighting, StreamLanguage, bracketMatching, indentOnInput, foldGutter } from '@codemirror/language';
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
  import { tags as t } from '@lezer/highlight';
  import { json } from '@codemirror/lang-json';
  import { yaml } from '@codemirror/lang-yaml';
  import { properties } from '@codemirror/legacy-modes/mode/properties';
  import { toml } from '@codemirror/legacy-modes/mode/toml';
  import { shell } from '@codemirror/legacy-modes/mode/shell';

  let {
    value = $bindable(''),
    filename = '',
    readonly = false,
    onsave,
  }: { value: string; filename?: string; readonly?: boolean; onsave?: () => void } = $props();

  let host: HTMLDivElement;
  let view: EditorView;

  function language(name: string): Extension {
    const n = name.toLowerCase();
    if (n.endsWith('.json') || n.endsWith('.mcmeta') || n.endsWith('.json5')) return json();
    if (n.endsWith('.yml') || n.endsWith('.yaml')) return yaml();
    if (n.endsWith('.properties') || n.endsWith('.cfg') || n.endsWith('.ini') || n.endsWith('.conf') || n.endsWith('.env') || n === 'eula.txt')
      return StreamLanguage.define(properties);
    if (n.endsWith('.toml')) return StreamLanguage.define(toml);
    if (n.endsWith('.sh') || n.endsWith('.bash')) return StreamLanguage.define(shell);
    return [];
  }

  const highlight = HighlightStyle.define([
    { tag: [t.keyword, t.operatorKeyword, t.modifier], color: 'var(--info)' },
    { tag: [t.propertyName, t.attributeName, t.definition(t.propertyName)], color: 'var(--accent)' },
    { tag: [t.string, t.special(t.string)], color: 'var(--copper)' },
    { tag: [t.number, t.bool, t.null, t.atom], color: 'var(--warn)' },
    { tag: [t.comment, t.lineComment, t.blockComment], color: 'var(--text-3)', fontStyle: 'italic' },
    { tag: [t.heading, t.typeName, t.className], color: 'var(--info)', fontWeight: '600' },
    { tag: [t.punctuation, t.separator, t.bracket], color: 'var(--text-2)' },
    { tag: t.invalid, color: 'var(--danger)' },
  ]);

  const theme = EditorView.theme({
    '&': { height: '100%', backgroundColor: 'var(--surface)', color: 'var(--text)', fontSize: '13.5px' },
    '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.55' },
    '.cm-content': { caretColor: 'var(--accent)', padding: '10px 0' },
    '.cm-gutters': { backgroundColor: 'var(--surface)', color: 'var(--text-3)', border: 'none', borderRight: '1px solid var(--line)' },
    '.cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--surface-2) 70%, transparent)' },
    '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--text)' },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: 'var(--accent-soft) !important' },
    '.cm-cursor': { borderLeftColor: 'var(--accent)', borderLeftWidth: '2px' },
    '&.cm-focused': { outline: 'none' },
    '.cm-panels': { backgroundColor: 'var(--surface-2)', color: 'var(--text)' },
    '.cm-searchMatch': { backgroundColor: 'var(--warn-soft)' },
    '.cm-matchingBracket': { backgroundColor: 'var(--accent-soft)', outline: 'none' },
  });

  onMount(() => {
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          foldGutter(),
          highlightActiveLine(),
          highlightActiveLineGutter(),
          drawSelection(),
          history(),
          bracketMatching(),
          indentOnInput(),
          highlightSelectionMatches(),
          syntaxHighlighting(highlight),
          language(filename),
          theme,
          EditorState.readOnly.of(readonly),
          EditorView.lineWrapping,
          keymap.of([
            { key: 'Mod-s', preventDefault: true, run: () => (onsave?.(), true) },
            ...defaultKeymap,
            ...historyKeymap,
            ...searchKeymap,
            indentWithTab,
          ]),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) value = u.state.doc.toString();
          }),
        ],
      }),
    });
    return () => view.destroy();
  });

  // external value changes (e.g. reload)
  $effect(() => {
    if (view && value !== view.state.doc.toString()) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
    }
  });
</script>

<div class="editor" bind:this={host}></div>

<style>
  .editor {
    height: 100%;
    min-height: 300px;
    border: 1px solid var(--line);
    border-radius: var(--r-m);
    overflow: hidden;
  }
</style>
