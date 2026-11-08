import { mount } from 'svelte';

import '@fontsource-variable/jetbrains-mono';
import '@fontsource-variable/jetbrains-mono/wght-italic.css';
import './app.css';
import { initTheme } from './lib/theme.svelte';
import { trackWindowControls } from './lib/window-controls';
import App from './App.svelte';

const target = document.getElementById('app');
if (!target) throw new Error('#app is missing');

initTheme();
trackWindowControls();

export default mount(App, { target });
