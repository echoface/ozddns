import { createRouter, createWebHashHistory } from "vue-router";
import OverviewPage from "./pages/OverviewPage.vue";
import ProvidersPage from "./pages/ProvidersPage.vue";
import ProviderDetailPage from "./pages/ProviderDetailPage.vue";
import JobsPage from "./pages/JobsPage.vue";
import JobDetailPage from "./pages/JobDetailPage.vue";
import JobFormPage from "./pages/JobFormPage.vue";
import ExecutionsPage from "./pages/ExecutionsPage.vue";
import SettingsPage from "./pages/SettingsPage.vue";
import LoginPage from "./pages/LoginPage.vue";
import NotFoundPage from "./pages/NotFoundPage.vue";
import { modal, navOpen } from "./store";

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", redirect: "/overview" },
    { path: "/overview", component: OverviewPage, meta: { section: "Control room", title: "Overview" } },
    { path: "/providers", component: ProvidersPage, meta: { section: "Providers", title: "All providers" } },
    { path: "/providers/:id/:tab?", component: ProviderDetailPage, meta: { section: "Providers", title: "Provider" } },
    { path: "/jobs", component: JobsPage, meta: { section: "Sync", title: "All tasks" } },
    { path: "/jobs/new", component: JobFormPage, meta: { section: "Sync", title: "New task" } },
    { path: "/jobs/:id/edit", component: JobFormPage, meta: { section: "Sync", title: "Edit task" } },
    { path: "/jobs/:id", component: JobDetailPage, meta: { section: "Sync", title: "Task" } },
    { path: "/executions", component: ExecutionsPage, meta: { section: "Observability", title: "Executions" } },
    { path: "/settings/:tab?", component: SettingsPage, meta: { section: "Configuration", title: "Settings" } },
    { path: "/login", component: LoginPage, meta: { title: "Sign in", shell: false } },
    { path: "/:pathMatch(.*)*", component: NotFoundPage, meta: { section: "", title: "Not found" } }
  ],
  scrollBehavior: () => ({ top: 0 })
});

router.afterEach(() => {
  navOpen.value = false;
  modal.value = null;
});
