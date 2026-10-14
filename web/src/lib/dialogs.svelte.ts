// Promise based confirm / prompt dialogs rendered by <Dialogs/> in the app shell.

export interface ConfirmOpts {
  title: string;
  text?: string;
  confirm?: string;
  danger?: boolean;
  /** require typing this text to enable the confirm button */
  typed?: string;
  /** optional checkbox shown in the dialog */
  checkbox?: string;
  checkboxDefault?: boolean;
}

export interface PromptOpts {
  title: string;
  label?: string;
  value?: string;
  placeholder?: string;
  confirm?: string;
  hint?: string;
  /** allow confirming with an empty value */
  optional?: boolean;
}

type Pending =
  | { kind: 'confirm'; opts: ConfirmOpts; resolve: (v: { ok: boolean; checked: boolean }) => void }
  | { kind: 'prompt'; opts: PromptOpts; resolve: (v: string | null) => void };

class DialogState {
  current = $state<Pending | null>(null);
}

export const dialogs = new DialogState();

export function confirmDialog(opts: ConfirmOpts): Promise<boolean> {
  return new Promise((resolve) => {
    dialogs.current = { kind: 'confirm', opts, resolve: (v) => resolve(v.ok) };
  });
}

/** Like confirmDialog but also returns the checkbox state. */
export function confirmWithOption(opts: ConfirmOpts): Promise<{ ok: boolean; checked: boolean }> {
  return new Promise((resolve) => {
    dialogs.current = { kind: 'confirm', opts, resolve };
  });
}

export function promptDialog(opts: PromptOpts): Promise<string | null> {
  return new Promise((resolve) => {
    dialogs.current = { kind: 'prompt', opts, resolve };
  });
}
