# Displays

An e-paper display shows a label template the way a printed label does, but it can change: stock levels on a bin, who has the drill, the state of a machine. In BAM! a display is a kind of printer, so you design for it in the template editor and "print" to it as you print labels.

## Adding a display

Under **Admin › Printers**, add a printer and choose a display's model: a panel by its size, an OpenEPaperLink tag, a TRMNL, one of paperlesspaper's OpenPaper frames, or a SoluM shelf label. Its screen is its roll, so the editor and the fit check know its size, resolution and inks. Then choose how BAM! reaches it:

- **OpenEPaperLink access point**: BAM! uploads the image to the access point, with the tag's MAC address, and the tag shows it when it next checks in, usually within a minute.
- **The display fetches it**: BAM! keeps the image, and the display fetches it from its own address with its own token. TRMNL does this, and so do most displays you build yourself, such as ESPHome's.
- **Webhook**: BAM! posts the image to a webhook channel, set up under **Admin › Notifications**, for something that takes an image.
- **paperlesspaper's cloud**: for an OpenPaper frame on paperlesspaper's own firmware, BAM! uploads the image to the frame's paper, by the paper's ID from its address in paperlesspaper's web app, and the frame shows it when it next wakes. Your admin sets paperlesspaper's API key under **Admin › Displays**.
- **SoluM's AIMS**: BAM! pushes the image to a SoluM label, by the code on its back, through your AIMS account, which your admin sets under **Admin › Displays**. Choose the model that is the label's size and inks: BAM! checks, and says if it is not.

## Showing an asset

Make a template the size of the display's screen, from the display's screen in the template editor, and print an asset's label to the display, from the asset's page or the print queue. A display shows one label at a time, and remembers which template and asset it shows. Labels in red or yellow show in those inks on a display that has them.

## Displays that fetch their image

Under **Admin › Displays**, a display that fetches its image has its address, and its tokens. **Make a token** shows the address with the token in it once: give that to the display. Anyone who has it can see what the display shows, and nothing else. **Revoke** stops a token at once.

A TRMNL sets itself up: add it with its MAC address, point it at BAM!, and it takes its token by itself the first time. Revoke its tokens to set it up again.

An OpenPaper frame on paperlesspaper's offline firmware fetches its image too, but sends its token as a password: **Make a token** shows the address, and the token to give the frame as the password of its HTTP authentication, with any user name.

## Keeping displays up to date

A hook can refresh displays when what they show changes. Under **Hooks**, give a hook events that fire it, such as an asset being edited, used or lent, tick **Refresh displays**, and offer it on the categories of the assets shown, and of the bins and shelves they are in. Changes in a burst make one refresh, after a short wait, since e-paper is slow. **Refresh now** on **Admin › Displays** refreshes one by hand.

Refreshes go through the print queue like any job, so they appear in its history, and one that fails says why. Under **Admin › Displays**, your admin sets how long a refresh waits for the rest of a burst, and how often displays that fetch their image ask again.
