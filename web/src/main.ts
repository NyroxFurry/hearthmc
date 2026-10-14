import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';
import { applyTheme } from './lib/session.svelte';

applyTheme();
matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => applyTheme());

mount(App, { target: document.getElementById('app')! });
