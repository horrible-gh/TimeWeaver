// Embedded builds must use the listener that served the page, never config.js
// from a developer's machine. The Go server supplies this meta element at runtime.
const element = document.querySelector('meta[name="tw-api-context"]');
const context = element && element.getAttribute("content");
if (!context || !/^\/[A-Za-z0-9._~-]+(?:\/[A-Za-z0-9._~-]+)*$/.test(context) ||
    context.split("/").some(part => part === "." || part === "..")) {
  throw new Error("TimeWeaver API context was not supplied by the server");
}
export default { API_SERVER_URL: window.location.origin + context };
