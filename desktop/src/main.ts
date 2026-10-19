import "./app.css";
import { mount } from "svelte";
import App from "./App.svelte";

const splash = document.getElementById("boot-splash");
if (splash) splash.textContent = "Montando…";

const app = mount(App, {
  target: document.getElementById("app")!,
});

splash?.remove();

export default app;
