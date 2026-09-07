import { defineConfig } from "vitepress";

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: "Calendar",
  base: "/calendar/",
  titleTemplate: false,
  cleanUrls: true,
  description: "Self-hosted calendar and holiday service with an embedded UI and ICS support",
  markdown: {
    theme: "material-theme",
  },
  head: [["link", { rel: "icon", href: "/calendar/favicon.ico", type: "image/x-icon" }]],
  themeConfig: {
    search: {
      provider: "local",
    },
    nav: [{ text: "About", link: "/" }],
    sidebar: [
      { text: "Overview", link: "/" },
      { text: "Quickstart", link: "/quickstart" },
      { text: "User Guide", link: "/ui-guide" },
    ],

    socialLinks: [
      { icon: "github", link: "https://github.com/rakunlabs/calendar" },
    ],

    editLink: {
      pattern: "https://github.com/rakunlabs/calendar/edit/main/_docs/:path",
    },

    lastUpdated: {
      text: "Updated at",
      formatOptions: {
        dateStyle: "full",
        timeStyle: "short",
      },
    },
  },
});
