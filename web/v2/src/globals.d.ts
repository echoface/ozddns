declare module "*.vue" {
  import type { DefineComponent } from "vue";
  const component: DefineComponent<Record<string, unknown>, Record<string, unknown>, unknown>;
  export default component;
}

interface Window {
  ICONS: Record<string, string>;
  OZ: {
    data: any;
    t: (key: string, vars?: Record<string, unknown>) => string;
    i18n: {
      codes: string[];
      normalize: (value: unknown) => string | null;
      label: (code: string) => string;
      set: (code: string, persist?: boolean) => string;
      locale: () => string;
      applyDOM: (root: ParentNode) => void;
      override: boolean;
    };
  };
}
