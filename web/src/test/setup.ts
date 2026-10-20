// What browsers have and jsdom does not.

// ResizeObserver, which bind:clientWidth needs; nothing is laid out, so
// nothing is ever resized.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// A dialog's showModal and close, which open and close it. (Tests run in
// Node rather than jsdom, such as the live renderer's, have no dialogs.)
if (typeof HTMLDialogElement !== 'undefined') {
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.open = false;
    this.dispatchEvent(new Event('close'));
  };
}
