-- allow_provisioner_execution lets org admins opt a warehouse's provisioner
-- connector into user execution as a normal managed service. Off by default:
-- the provisioner credential is reserved for the reconcile worker, and user
-- queries must never fall back to it (including when the kill switch is off).
ALTER TABLE warehouses
    ADD COLUMN allow_provisioner_execution BOOLEAN NOT NULL DEFAULT false;
