import { computed, defineComponent, h, ref } from "vue";
import Icon from "./Icon.vue";
import { fmtAgo, initials, looksMasked, t } from "../lib";
import { getProviderType } from "../store";
import type { Provider, RunStatus } from "../types";

export const Pill = defineComponent({
  name: "Pill",
  props: { kind: { type: String, default: "mute" }, dot: Boolean },
  setup(props, { slots }) {
    return () => h("span", { class: ["pill", props.kind] }, [
      props.dot ? h("span", { class: "dot" }) : null,
      slots.default?.()
    ]);
  }
});

export const StatusPill = defineComponent({
  name: "StatusPill",
  props: { status: { type: String as () => RunStatus, default: "nochange" } },
  setup(props) {
    const meta = computed(() => ({
      success: ["ok", "Updated", "check-circle"],
      nochange: ["mute", "No change", "circle-dot"],
      failed: ["err", "Failed", "x-circle"],
      skipped: ["info", "Skipped", "corner-down-right"],
      running: ["coral", "Running", "loader"]
    }[props.status] || ["mute", "No change", "circle-dot"]));
    return () => h(Pill, { kind: meta.value[0] }, {
      default: () => [h(Icon, { name: meta.value[2] }), t(meta.value[1])]
    });
  }
});

export const ProviderLogo = defineComponent({
  name: "ProviderLogo",
  props: { type: { type: String, required: true }, big: Boolean },
  setup(props) {
    const providerType = computed(() => getProviderType(props.type) || { label: props.type, color: "custom" });
    return () => h("span", {
      class: ["logo", props.big && "lg"],
      style: {
        color: `var(--pv-${providerType.value.color})`,
        background: `var(--pv-${providerType.value.color}-bg)`
      }
    }, initials(providerType.value.label));
  }
});

export const ProviderStatus = defineComponent({
  name: "ProviderStatus",
  props: { provider: { type: Object as () => Provider, required: true } },
  setup(props) {
    return () => {
      if (!props.provider.enabled) return h(Pill, { kind: "mute", dot: true }, () => t("Disabled"));
      if (props.provider.status === "ok") return h(Pill, { kind: "ok", dot: true }, () => t("Connected"));
      if (props.provider.status === "warn") return h(Pill, { kind: "warn", dot: true }, () => t("Attention"));
      return h(Pill, { kind: "err", dot: true }, () => t("Error"));
    };
  }
});

export const Toggle = defineComponent({
  name: "Toggle",
  props: {
    modelValue: Boolean,
    label: { type: String, default: "" },
    disabled: Boolean
  },
  emits: ["update:modelValue", "click"],
  setup(props, { emit }) {
    return () => h("button", {
      type: "button",
      class: ["switch", props.modelValue && "on"],
      "aria-pressed": String(props.modelValue),
      "aria-label": props.label || t(props.modelValue ? "Enabled" : "Disabled"),
      title: props.label || t(props.modelValue ? "Enabled" : "Disabled"),
      disabled: props.disabled,
      onClick: (event: MouseEvent) => {
        event.stopPropagation();
        emit("update:modelValue", !props.modelValue);
        emit("click", event);
      }
    });
  }
});

export const PageHead = defineComponent({
  name: "PageHead",
  props: { eyebrow: String, title: { type: String, required: true }, sub: String },
  setup(props, { slots }) {
    return () => h("div", { class: "page-head" }, [
      h("div", { class: "titles" }, [
        props.eyebrow ? h("div", { class: "eyebrow" }, t(props.eyebrow)) : null,
        h("h1", { class: "page-title" }, t(props.title)),
        props.sub ? h("p", { class: "page-sub" }, t(props.sub)) : null
      ]),
      slots.actions ? h("div", { class: "page-actions" }, slots.actions()) : null
    ]);
  }
});

export const Banner = defineComponent({
  name: "Banner",
  props: { kind: { type: String, default: "info" }, title: { type: String, required: true }, body: String },
  setup(props, { slots }) {
    const icon = computed(() => props.kind === "err" || props.kind === "warn" ? "alert-triangle" : props.kind === "ok" ? "check-circle" : "info");
    return () => h("div", { class: ["banner", props.kind] }, [
      h(Icon, { name: icon.value }),
      h("div", [
        h("div", { class: "b-title" }, t(props.title)),
        props.body || slots.default ? h("div", { class: "b-body" }, slots.default?.() || t(props.body || "")) : null
      ])
    ]);
  }
});

export const EmptyState = defineComponent({
  name: "EmptyState",
  props: { icon: { type: String, default: "info" }, title: { type: String, required: true }, body: { type: String, required: true } },
  setup(props, { slots }) {
    return () => h("div", { class: "empty" }, [
      h("div", { class: "art" }, [h(Icon, { name: props.icon })]),
      h("h3", t(props.title)),
      h("p", t(props.body)),
      slots.default?.()
    ]);
  }
});

export const DiffValue = defineComponent({
  name: "DiffValue",
  props: { oldValue: String, newValue: String, strike: { type: Boolean, default: true } },
  setup(props) {
    return () => props.oldValue === props.newValue
      ? h("span", { class: "mono", style: "color:var(--ink-2)" }, props.newValue || "—")
      : h("span", { class: "diff" }, [
          h("span", { class: props.strike ? "old" : "old flat" }, props.oldValue || "—"),
          h("span", { class: "new" }, props.newValue || "—")
        ]);
  }
});

export const ProviderField = defineComponent({
  name: "ProviderField",
  props: {
    field: { type: Object as () => any, required: true },
    modelValue: { type: [String, Number, Boolean] as any, default: "" },
    savedValue: { type: [String, Number, Boolean] as any, default: "" }
  },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    const reveal = ref(false);
    const input = () => {
      const common = {
        class: ["input", props.field.mono && "mono"],
        value: props.modelValue,
        onInput: (event: Event) => emit("update:modelValue", (event.target as HTMLInputElement).value)
      };
      if (props.field.type === "select") {
        return h("select", common, (props.field.options || []).map((option: string) => h("option", { value: option }, option)));
      }
      if (props.field.type === "bool") {
        return h("select", common, [
          h("option", { value: "true" }, t("Yes")),
          h("option", { value: "false" }, t("No"))
        ]);
      }
      if (props.field.type === "textarea") return h("textarea", { ...common, rows: 3 });
      if (props.field.type === "secret") {
        return h("div", [
          h("div", { class: "input-group" }, [
            h("input", { ...common, type: reveal.value ? "text" : "password", autocomplete: "new-password", placeholder: "••••••••" }),
            h("button", {
              class: ["btn", "peek", !props.modelValue && "is-empty"],
              type: "button",
              title: t("Show / hide"),
              "aria-label": t("Show / hide"),
              onClick: () => { reveal.value = !reveal.value; }
            }, [h(Icon, { name: "eye" })])
          ]),
          looksMasked(props.savedValue)
            ? h("div", { class: "secret-note" }, [
                h(Pill, { kind: "mute", dot: true }, () => [t("Saved"), h("span", { class: "mono" }, String(props.savedValue))]),
                h("span", { class: "tiny muted" }, t("Leave blank to keep it, or type a new value to replace."))
              ])
            : null
        ]);
      }
      return h("input", { ...common, type: props.field.type === "number" ? "number" : "text" });
    };
    return () => h("div", { class: "field" }, [
      h("label", [t(props.field.label || ""), props.field.required ? h("span", { class: "req" }, " *") : null]),
      input(),
      props.field.help ? h("span", { class: "help" }, t(props.field.help)) : null
    ]);
  }
});

export const RelativeTime = defineComponent({
  name: "RelativeTime",
  props: { value: Number },
  setup: (props) => () => h("span", fmtAgo(props.value))
});
