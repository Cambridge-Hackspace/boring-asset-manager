// What browsers have and jsdom does not.

// ResizeObserver, which bind:clientWidth needs; nothing is laid out, so
// nothing is ever resized.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// A dialog's showModal and close, which open and close it.
HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
  this.open = true;
};
HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
  this.open = false;
  this.dispatchEvent(new Event('close'));
};
