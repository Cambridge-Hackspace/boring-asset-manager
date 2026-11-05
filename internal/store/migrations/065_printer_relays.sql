-- 065_printer_relays.sql: printers reached through a relay (#227,
-- docs/relays.md#printers-through-a-relay).
--
-- relay names the relay a printer's connection goes through; empty is
-- this server. The address is then on the relay's network, and the
-- relay's own allowlist decides whether it may be reached.

ALTER TABLE printers ADD COLUMN relay VARCHAR(63) NOT NULL DEFAULT '';
