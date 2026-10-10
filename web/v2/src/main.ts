import "./compat/icons.js";
import "./compat/data.js";
import "./compat/i18n.js";
import "./compat/tokens.css";
import "./compat/app.css";
import "./v2.css";

import { createApp } from "vue";
import App from "./App.vue";
import { router } from "./router";
import { boot } from "./store";
import { initPreferences } from "./lib";

initPreferences();

createApp(App)
  .use(router)
  .mount("#root");

void boot();
