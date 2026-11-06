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

// Scrolling, which the router does on every page change; nothing is laid
// out, so there is nowhere to scroll. jsdom has its own that says it is not
// implemented each time, which buries the warnings that matter.
if (typeof window !== 'undefined') {
  window.scrollTo = () => {};
}

// A canvas's drawing context, which jsdom has none of without the canvas
// package: null, as jsdom's own returns, but without saying so each time.
if (typeof HTMLCanvasElement !== 'undefined') {
  HTMLCanvasElement.prototype.getContext = (() => null) as typeof HTMLCanvasElement.prototype.getContext;
}
