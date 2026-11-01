import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';
import { applyTheme, storedTheme } from './lib/theme.svelte';

// This browser's light or dark, before anything is drawn (#168).
applyTheme(storedTheme());
mount(App, { target: document.getElementById('app')! });
