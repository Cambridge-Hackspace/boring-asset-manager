import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';
import { installedApp } from './lib/install/app.svelte';
import { applyTheme, storedTheme } from './lib/theme.svelte';

// This browser's light or dark, before anything is drawn (#168).
applyTheme(storedTheme());
// Before the app is drawn too: Chrome offers to install it early, and the
// service worker keeps the shell (docs/installing.md#on-a-phone).
installedApp.start();
mount(App, { target: document.getElementById('app')! });
