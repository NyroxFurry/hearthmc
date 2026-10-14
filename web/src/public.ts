import { mount } from 'svelte';
import './app.css';
import PublicApp from './public/PublicApp.svelte';

// Die Statusseite hat immer farbiges Band und darunter helles Papier,
// unabhängig vom Farbschema des Panels.
document.documentElement.dataset.theme = 'light';

mount(PublicApp, { target: document.getElementById('app')! });
