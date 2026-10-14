export type ToastKind = 'ok' | 'error' | 'info';

export interface Toast {
  id: number;
  kind: ToastKind;
  text: string;
}

let next = 1;

class Toasts {
  list = $state<Toast[]>([]);

  push(kind: ToastKind, text: string, ms = 4200) {
    const id = next++;
    this.list = [...this.list, { id, kind, text }];
    setTimeout(() => this.dismiss(id), ms);
  }

  dismiss(id: number) {
    this.list = this.list.filter((t) => t.id !== id);
  }
}

export const toasts = new Toasts();
export const toastOk = (t: string) => toasts.push('ok', t);
export const toastInfo = (t: string) => toasts.push('info', t);
export const toastError = (e: unknown) => toasts.push('error', e instanceof Error ? e.message : String(e), 6500);
