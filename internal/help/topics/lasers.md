# Lasers

A laser engraves a label onto something that lasts: a tool handle, a bin front, a plywood tag, a piece of anodized aluminum or acrylic. In BAM! a laser is a kind of printer that makes one piece at a time, on whatever you put on its bed.

## Safety first

Lasers start fires and can blind people, so a person at the machine always starts a job.

- On a machine with its own start button, BAM! sends the job ready, and you press the button.
- Otherwise you press **Start** in BAM!, which needs the **lasers.run** permission.
- BAM! checks the machine's lid, safety key, flame sensor and exhaust first, where it reports them, and won't start while any reports a problem.
- Nothing else starts a laser: not an assistant, a flow, a hook or a script without **lasers.run**. An assistant can prepare a job for you.
- The print queue never sends anything to a laser.
- Stay with the machine while it runs. **Pause** and **Cancel** are always there, for anyone who may print.

## Engraving a label

From an asset's page, choose **Engrave**; from the template editor, choose **Engrave** on a saved template. On the **Lasers** page:

1. Choose the laser and the material. Each material has its power, speed and resolution; one engraved from the back, such as clear acrylic, is mirrored, so lay it face down.
2. Give the workpiece's size, and where its top left corner sits on the bed. The label is centered on it unless you place it.
3. **Prepare** draws the label, and shows the work area, the workpiece and the label on it. Nothing moves yet.
4. **Frame** has the machine trace the outline with its pointer, so you can check it on the real piece.
5. **Start**, or press the machine's button. The page shows the job's progress and the machine's state.

When the job is done, the asset's history says its label was engraved, and on what.

## Cutting

In the template editor, tick **Cut** on a shape, such as the tag's outline or the hole it hangs by. A laser cuts along it after engraving, if the material can be cut. Other printers draw it as you designed it.

## Materials

Each laser comes with its maker's presets. Your admin can tune a preset for one laser, or add your own materials, on the **Lasers** page.
