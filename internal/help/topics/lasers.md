# Lasers

A laser engraves a label onto something that lasts: a tool handle, a bin front, a plywood tag, a piece of anodized aluminum or acrylic. In BAM! a laser is a kind of printer that makes one piece at a time, on whatever you put on its bed.

## Safety first

Lasers start fires and can blind people, so a person at the machine always starts a job.

- On a machine with its own start button, BAM! sends the job ready, and you press the button.
- Otherwise you press **Start** in BAM!, which needs the **lasers.run** permission.
- BAM! checks the machine's lid, safety key, drawer, flame sensor and exhaust first, where it reports them, and won't start while any reports a problem. On an xTool it also checks that the machine's firmware is one BAM! knows for its model, unless your admin has allowed it.
- Many lasers report little. A GRBL diode laser reports only its own state, such as an alarm or an open safety door; a Ruida controller reports nothing BAM! reads. On those, you are the check: look before you press **Start**.
- BAM! never starts a Ruida CO2 laser: **Start** sends the job to the controller, and you choose it on the controller's panel, frame it with the panel's **Frame** key, and start it there.
- Nothing else starts a laser: not an assistant, a flow, a hook or a script without **lasers.run**. An assistant can prepare a job for you.
- The print queue never sends anything to a laser.
- Stay with the machine while it runs. **Pause** and **Cancel** are always there, for anyone who may print.

## Engraving a label

From an asset's page, choose **Engrave**; from the template editor, choose **Engrave** on a saved template. On the **Lasers** page:

1. Choose the laser and the material. Each material has its power, speed and resolution; one engraved from the back, such as clear acrylic, is mirrored, so lay it face down.
2. Give the workpiece's size, and where its top left corner sits on the bed, or measure it with the camera (below). The label goes where the template says, centered unless it names a corner, or where you place it.
3. **Prepare** draws the label, and shows the work area, the workpiece and the label on it. Nothing moves yet.
4. **Frame** has the machine trace the outline with its pointer, so you can check it on the real piece. On a GRBL laser the laser is off while it frames, unless your admin has set a low power for it; on a Ruida, frame from its panel.
5. **Start**, or press the machine's button. The page shows the job's progress and the machine's state.

When the job is done, the asset's history says its label was engraved, and on what.

## Placing the label with the camera

A laser with a camera, such as the xTool F1 Ultra, finds the workpiece wherever you put it on the bed.

1. **Calibrate once for each height.** In **Camera** on the **Lasers** page, lay light scrap (plywood or card) where it says, choose its material, and **Prepare the marks**. Frame and start them as any job. Then, with the scrap still in place, give the height of its surface above the bed and **Find the marks**. BAM! refuses a calibration that fits too loosely.
2. **Measure.** Lay the workpiece on the bed, give its surface's height, and **Measure with the camera**. Its size, place and turn are filled in. The camera sees the bed differently at each height, so BAM! asks you to calibrate again for a piece much higher or lower than the scrap was.
3. **Check over the photo.** **Prepare** draws the plan over the camera's photo. Tick that it sits on the workpiece before **Frame** and **Start**.

The label goes where the template says on the workpiece: centered, or in a corner, a little in from its edges (set in the template editor, beside the printer, or for one job on the **Lasers** page). It turns with the piece.

## Cutting

In the template editor, tick **Cut** on a shape, such as the tag's outline or the hole it hangs by. A laser cuts along it after engraving, if the material can be cut. Other printers draw it as you designed it.

## Materials

Each laser comes with its maker's presets. Your admin can tune a preset for one laser, or add your own materials, on the **Lasers** page.
