# Relays

A **relay** is BAM itself, run on a small machine on a local network, such as a Raspberry Pi beside a label printer. It lets a BAM hosted elsewhere reach printers and devices on that network. The relay dials out to BAM, so nothing on the local network has to be opened to the internet.

**Relays**, under Admin, lists each relay: whether it is online, and since when, its version, the address it last connected from, what it may reach, and when its certificate expires. Relays need `[auth] secret_key` in BAM's configuration, which keeps their keys safe.

**Add relay** names a relay and gives a code that pairs it once, within the hour, with the command to run on the relay's machine: `bam relay join` followed by BAM's address and the code. The relay makes its own key there, which never leaves it, and is given a certificate and a credential of its own. A relay renews its certificate by itself, every week, while it is paired.

**New code** pairs a relay again, as on a new machine. **Revoke** stops a relay at once: its connections close and it can no longer renew. Its name stays taken until its last certificate expires. A relay that never paired is simply removed.

Once paired, the relay runs as `bam relay`, as a service beside its printers. It reaches only what its own **allowlist** names, in its own configuration: printers by address, range or name, IPP queues, Windows printers and device paths. BAM cannot widen it. The relay renews its certificate by itself, a few days before it expires.

A printer is reached through a relay by choosing it under **Reached through** on the **Printers** page; its address is then on the relay's network. BAM still prepares each label, and the relay only passes it to the printer. A relay that is offline fails the job, and its labels wait in the print queue until it is back. **Relays** lists the printers each relay serves, and how each one's last job went.

**Rotate the mesh root** replaces the key that issues relays' certificates. Every relay is then unpaired, and pairs again with a new code.

Notification channels can announce a relay going offline, and coming online again ([Notifications](/help/notifications)).
