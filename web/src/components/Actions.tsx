import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { api, type Action, type Job } from "../api";

// Action buttons for a service. Delete requires typing the service name.
export function Actions({
  server,
  app,
  busy,
  onStarted,
}: {
  server: string;
  app: string;
  busy: boolean;
  onStarted: (job: Job) => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);
  const start = useMutation({
    mutationFn: (action: Action) => api.startAction(server, app, action),
    onSuccess: (job) => {
      setConfirmDelete(false);
      onStarted(job);
    },
  });
  const disabled = busy || start.isPending;

  return (
    <>
      <div className="actions">
        <button className="secondary" disabled={disabled} onClick={() => start.mutate("restart")}>
          Restart
        </button>
        <button
          className="secondary"
          disabled={disabled}
          title="Pull or rebuild the image and recreate the container"
          onClick={() => start.mutate("update")}
        >
          Update
        </button>
        <button className="danger" disabled={disabled} onClick={() => setConfirmDelete(true)}>
          Delete
        </button>
      </div>
      {start.error && <div className="error">{start.error.message}</div>}
      {confirmDelete && (
        <ConfirmDelete
          app={app}
          pending={start.isPending}
          onCancel={() => setConfirmDelete(false)}
          onConfirm={() => start.mutate("delete")}
        />
      )}
    </>
  );
}

function ConfirmDelete({
  app,
  pending,
  onCancel,
  onConfirm,
}: {
  app: string;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const [typed, setTyped] = useState("");
  return (
    <div className="modal-backdrop" onClick={onCancel}>
      <div className="card modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <h2>Delete {app}?</h2>
        <p className="muted">
          Stops the containers and removes the compose files and settings. Volumes and bind-mounted data are kept.
        </p>
        <label>
          Type <code>{app}</code> to confirm
          <input type="text" autoFocus value={typed} onChange={(e) => setTyped(e.target.value)} />
        </label>
        <div className="actions end">
          <button className="secondary" onClick={onCancel}>
            Cancel
          </button>
          <button className="danger" disabled={typed !== app || pending} onClick={onConfirm}>
            {pending ? "Deleting…" : "Delete"}
          </button>
        </div>
      </div>
    </div>
  );
}
