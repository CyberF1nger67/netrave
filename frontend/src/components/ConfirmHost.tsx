import { useState } from 'react';
import type { FormEvent } from 'react';
import { Modal } from './ui/Modal';
import { t } from '../i18n';
import { type DialogRequest, useDialogStore } from '../stores/dialogs';

const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'border-transparent bg-bad font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(220,38,38,.5)] hover:brightness-110';

/** Shows the confirmation or text dialog requested through askConfirm / askText. Mount it once. */
export function ConfirmHost() {
  const request = useDialogStore((state) => state.request);
  if (!request) return null;
  return <DialogBody key={request.id} request={request} />;
}

function DialogBody({ request }: { request: DialogRequest }) {
  const [text, setText] = useState(request.kind === 'text' ? request.options.initial ?? '' : '');
  const finish = (value: boolean | string | null) => {
    if (request.kind === 'confirm') request.resolve(value === true);
    else request.resolve(typeof value === 'string' && value.trim() ? value.trim() : null);
    useDialogStore.getState().set(null);
  };
  const cancel = () => finish(request.kind === 'confirm' ? false : null);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    finish(request.kind === 'confirm' ? true : text);
  };

  const isDanger = request.kind === 'confirm' && request.options.danger === true;
  const label = request.options.confirmLabel ?? (request.kind === 'confirm' && isDanger ? t('Delete') : t('Confirm'));

  return (
    <Modal title={request.options.title} onClose={cancel}>
      <form onSubmit={submit}>
        {request.kind === 'confirm' && request.options.message && (
          <p className="m-0 text-sm leading-relaxed text-muted">{request.options.message}</p>
        )}
        {request.kind === 'text' && (
          <>
            {request.options.label && <label className="mb-1 block text-sm text-muted">{request.options.label}</label>}
            <input autoFocus value={text} onChange={(event) => setText(event.target.value)} />
          </>
        )}
        <div className="mt-5 flex flex-wrap justify-end gap-2">
          <button type="button" onClick={cancel}>{t('Cancel')}</button>
          <button type="submit" className={isDanger ? danger : primary}>{label}</button>
        </div>
      </form>
    </Modal>
  );
}
