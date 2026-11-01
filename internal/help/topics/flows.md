# Flows

A **flow** is a guided page for doing one everyday thing, made for a phone or the shop's tablet, so that anyone can do it without knowing BAM!: leaving something in the shop with their name on it, or taking it home again.

## Running a flow

Open **Flows**, or scan the QR code on a flow's poster, and follow the steps. Each asks one thing at a time:

- **Choose** what kind of thing it is.
- **Fill in** a form: what it is, whose it is, until when you're leaving it, yes or no, one of a few choices, or a photo, whatever the flow asks. A form can be asked only for some kinds, such as long boards only.
- **Find** something already here: scan its code with the camera, type it, or let a barcode scanner type it; search for it; or pick it from a list.
- **Confirm** what will happen before it does.

Above each question it says how far along you are, such as *step 2 of 3*. A choice can bring in a question that only some answers ask, so the count can grow as you go.

When it's done, it says what it did, and shows the label to put on it if it made one. A photo you took is added to it once it is filed. **Start again** is for the next person.

What a flow does, it does as you: it can only do what you're allowed to, and history records it as yours, made through the flow.

## Making a flow

Whoever manages flows makes them on the **Flows** page, with **New flow**: choose the categories it files into or takes from, start from **Leave something** or **Collect it**, and adjust its steps. The steps are listed in order, one open at a time; the preview beside them shows the open step as a phone shows it.

- **Choose category** offers the flow's categories, or always uses one.
- **Form** asks questions of its own, in its own words: text, long text, a number, a date, yes or no, a choice from options you list, or a photo. Each can have help, a default such as `{user}` or `{today}`, and be required. A question's answer is a value later steps use, as `{what}`: its key is made from the question as you type it, and shown under it, such as *Used as `{what}`*; **Change** sets another. Changing the key of a question that later steps use says which, since they would name nothing until changed too.
- **Set fields** puts answers, or other values, into the thing's fields: for each field, chosen from those the flow's categories record, a value such as `{leave_until}`, `{today}` or `{user}`, fixed text, or a mix. A field with choices offers them. It can also add a photo question's photo. Before **Create**, it fills in the new thing as it is filed; after **Find** or **Create**, it updates what was found or filed. The step says which. It asks nothing.
- **Find** finds something already here, by its code, by search or from a list, optionally only what another flow filed, or whose field matches, such as its owner being `{user}`.
- **Create** files it, named from the answers, as `{owner}: {what}`, with the fields set before it, and can put it in a place.
- **Label** shows its label, adds it to the print queue, or prints it.
- **Change** archives it, sets its condition, lends or returns it, moves it, or triggers a [hook](/help/hooks).
- **Confirm** and **Done** say what will happen, and what did.

Names, defaults, texts and Set fields' values can name **values** in braces, such as `{owner}: {what}`. Every run has four: `{today}`, the date; `{now}`, the date and time; `{user}`, who runs it; and `{flow}`, the flow's name. The rest come from the steps before: the category chosen, each question's answer, each field a Set fields step sets, and what was found or filed, with its fields. You needn't remember them: type `{` in any of those boxes, or press **{ }** beside it, and the editor lists the values that step can use, each with where it comes from, to choose with the arrow keys and Enter or Tab. A value from a step shown only sometimes is said to be empty when that step is skipped. A name no earlier step sets is marked under its box.

Any step can be shown **only when** a value set before it is something, such as only for long boards: choose the value from the list, and what it is compared with follows it, a category from the flow's, a choice from the question's choices, a date as a date. A flow can only do what its maker may do.

A flow cannot be saved while a kind of thing it files would be missing a field that kind requires: each field a category requires must be set by a **Set fields** step before **Create**, either one shown always or one shown only for that category. Saving also says when a fixed value doesn't suit its field, such as a date that isn't one. Its **Poster** has its name, its QR code and its one line of instructions, to print and put up, or to make into a label template.

Flows made before forms asked their own questions were converted when BAM! was updated: each form now asks what it asked before, in the same words, and a **Set fields** step before **Create** puts the answers into the same fields as before.

## Left past its date

A flow can name a date to collect by, such as **Leave until**. Once that date has passed, what was left is listed on the **Flows** page as past its date, marked when someone looks for it, and announced once to the notification channels that follow it. Moving the date on clears it.

## Starting from the thing itself

A flow that starts by finding something, such as **Collect it**, is offered on the pages of what it would find: scan the QR code on what you left, and **Collect it** is there, with it already found.
