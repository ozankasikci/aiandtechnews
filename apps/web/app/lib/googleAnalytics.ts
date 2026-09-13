export const GA_ID = "G-32SP4ZKM67";

// Run before hydration so events can queue even while gtag.js is still loading.
export const GA_INIT_SCRIPT = `
  window.dataLayer = window.dataLayer || [];
  window.gtag = function () { window.dataLayer.push(arguments); };
  window.gtag('js', new Date());
  window.gtag('config', '${GA_ID}', { send_page_view: false });
`;
