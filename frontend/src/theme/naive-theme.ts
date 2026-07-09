import { type GlobalThemeOverrides } from "naive-ui";

export const pixeoThemeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: "#1c1917",
    primaryColorHover: "#292524",
    primaryColorPressed: "#0c0a09",
    primaryColorSuppl: "#1c1917",
    infoColor: "#2563eb",
    successColor: "#16a34a",
    warningColor: "#a16207",
    errorColor: "#dc2626",
    bodyColor: "#f5f4f2",
    cardColor: "#fafaf9",
    modalColor: "#fafaf9",
    popoverColor: "#fafaf9",
    tableColor: "#ffffff",
    inputColor: "#ffffff",
    actionColor: "#f5f4f2",
    borderColor: "rgba(20, 20, 19, 0.14)",
    dividerColor: "rgba(20, 20, 19, 0.14)",
    borderRadius: "12px",
    borderRadiusSmall: "8px",
    fontFamily:
      '"Geist", Inter, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    fontSize: "14px",
    fontSizeMini: "12px",
    fontSizeTiny: "12px",
    fontSizeSmall: "13px",
    fontSizeMedium: "14px",
    fontSizeLarge: "15px",
    fontSizeHuge: "16px",
    heightMedium: "36px",
    heightSmall: "30px",
  },
  Button: {
    borderRadiusMedium: "12px",
    borderRadiusSmall: "10px",
    borderRadiusLarge: "14px",
    fontWeight: "500",
  },
  Card: {
    borderRadius: "16px",
    color: "#fafaf9",
    borderColor: "rgba(20, 20, 19, 0.14)",
    titleFontWeight: "700",
  },
  Input: {
    borderRadius: "12px",
    color: "#ffffff",
    border: "1px solid rgba(20, 20, 19, 0.14)",
    borderHover: "1px solid rgba(20, 20, 19, 0.32)",
    borderFocus: "1px solid #1c1917",
    boxShadowFocus: "0 0 0 2px rgba(28, 25, 23, 0.18)",
  },
  Select: {
    peers: {
      InternalSelection: {
        borderRadius: "12px",
        border: "1px solid rgba(20, 20, 19, 0.14)",
        borderHover: "1px solid rgba(20, 20, 19, 0.32)",
        borderFocus: "1px solid #1c1917",
        boxShadowFocus: "0 0 0 2px rgba(28, 25, 23, 0.18)",
      },
    },
  },
  Tag: {
    borderRadius: "999px",
  },
  Switch: {
    railColor: "#ddd9d3",
    railColorActive: "#1c1917",
  },
  Checkbox: {
    border: "1px solid rgba(20, 20, 19, 0.32)",
    borderChecked: "1px solid #1c1917",
    colorChecked: "#1c1917",
  },
  DataTable: {
    borderColor: "rgba(20, 20, 19, 0.14)",
    thColor: "#e8e6e2",
    thFontWeight: "600",
    tdColor: "#ffffff",
    tdColorStriped: "#f5f4f2",
    borderRadius: "12px",
  },
  Modal: {
    borderRadius: "16px",
    color: "#fafaf9",
  },
  Dialog: {
    borderRadius: "16px",
    color: "#fafaf9",
  },
  Drawer: {
    color: "#fafaf9",
  },
  Alert: {
    borderRadius: "12px",
  },
  Collapse: {
    borderRadius: "12px",
    color: "#ffffff",
    borderColor: "rgba(20, 20, 19, 0.14)",
  },
  Progress: {
    railColor: "rgba(20, 20, 19, 0.12)",
    indicatorColor: "#1c1917",
  },
  Spin: {
    color: "#1c1917",
  },
  Skeleton: {
    borderRadius: "8px",
  },
};

export const pixeoDarkThemeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: "#fafaf9",
    primaryColorHover: "#e7e5e4",
    primaryColorPressed: "#d6d3d1",
    primaryColorSuppl: "#fafaf9",
    bodyColor: "#121110",
    cardColor: "#1c1b19",
    modalColor: "#1c1b19",
    popoverColor: "#1c1b19",
    tableColor: "#161514",
    inputColor: "#161514",
    actionColor: "#23211f",
    borderColor: "rgba(245, 244, 242, 0.16)",
    dividerColor: "rgba(245, 244, 242, 0.16)",
    textColorBase: "#f5f4f2",
    textColor1: "#f5f4f2",
    textColor2: "rgba(245, 244, 242, 0.62)",
    textColor3: "rgba(245, 244, 242, 0.44)",
    placeholderColor: "rgba(245, 244, 242, 0.44)",
  },
  Button: {
    borderRadiusMedium: "12px",
    borderRadiusSmall: "10px",
    borderRadiusLarge: "14px",
    fontWeight: "500",
  },
  Card: {
    borderRadius: "16px",
    color: "#1c1b19",
    borderColor: "rgba(245, 244, 242, 0.16)",
    titleFontWeight: "700",
  },
  Input: {
    borderRadius: "12px",
    color: "#161514",
    border: "1px solid rgba(245, 244, 242, 0.16)",
    borderHover: "1px solid rgba(245, 244, 242, 0.34)",
    borderFocus: "1px solid #fafaf9",
    boxShadowFocus: "0 0 0 2px rgba(245, 244, 242, 0.18)",
  },
  Select: {
    peers: {
      InternalSelection: {
        borderRadius: "12px",
        border: "1px solid rgba(245, 244, 242, 0.16)",
        borderHover: "1px solid rgba(245, 244, 242, 0.34)",
        borderFocus: "1px solid #fafaf9",
        boxShadowFocus: "0 0 0 2px rgba(245, 244, 242, 0.18)",
      },
    },
  },
  Tag: {
    borderRadius: "999px",
  },
  Switch: {
    railColor: "#2c2a27",
    railColorActive: "#fafaf9",
    buttonColorActive: "#121110",
  },
  Checkbox: {
    border: "1px solid rgba(245, 244, 242, 0.34)",
    borderChecked: "1px solid #fafaf9",
    colorChecked: "#fafaf9",
  },
  DataTable: {
    borderColor: "rgba(245, 244, 242, 0.16)",
    thColor: "#23211f",
    thFontWeight: "600",
    tdColor: "#161514",
    tdColorStriped: "#1c1b19",
    borderRadius: "12px",
  },
  Modal: {
    borderRadius: "16px",
    color: "#1c1b19",
  },
  Dialog: {
    borderRadius: "16px",
    color: "#1c1b19",
  },
  Drawer: {
    color: "#1c1b19",
  },
  Alert: {
    borderRadius: "12px",
  },
  Collapse: {
    borderRadius: "12px",
    color: "#161514",
    borderColor: "rgba(245, 244, 242, 0.16)",
  },
  Progress: {
    railColor: "rgba(245, 244, 242, 0.12)",
    indicatorColor: "#fafaf9",
  },
  Spin: {
    color: "#fafaf9",
  },
  Skeleton: {
    borderRadius: "8px",
  },
};
