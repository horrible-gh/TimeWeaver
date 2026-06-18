# TimeWeaver Client

The `client` directory contains the Vue 3 frontend for TimeWeaver. It provides the login screen and dashboard views for schedules, groups, devices, task lists, charts, and execution history.

## Stack

- Vue 3
- Vue Router
- Vue I18n
- Axios
- Chart.js with `vue-chart-3`
- Phosphor icons

## Project Layout

```text
client/
|-- api.js                 Shared Axios client and request helpers
|-- config.sample.js       API configuration template
|-- vue.config.js          Vue CLI multi-page and dev-server settings
|-- public/                HTML templates and static assets
`-- src/
    |-- login/             Login entry, router, i18n, and form component
    `-- dashboard/         Dashboard entry, router, i18n, and feature views
```

## Setup

Install dependencies:

```powershell
npm install
```

Create the local frontend configuration:

```powershell
Copy-Item config.sample.js config.js
```

Edit `config.js` so `API_SERVER_URL` points to the TimeWeaver backend API. The sample value is:

```javascript
const config = {
    API_SERVER_URL: "http://127.0.0.1:8000/time_weaver"
};

export default config;
```

## Development

Start the Vue development server:

```powershell
npm run serve
```

The development server is configured to listen on `0.0.0.0:10808`. Login is served from `/login`, and the dashboard is served from `/dashboard/`.

## Build and Lint

Create a production build:

```powershell
npm run build
```

Run linting:

```powershell
npm run lint
```

## API Client

`api.js` creates the shared Axios client, enables credentialed requests, injects the bearer token from `localStorage.access_token`, and redirects to `/login` on HTTP 401 responses.

The exported helpers are:

- `getRequest(url, params, type)`
- `postRequest(url, data, type)`
- `putRequest(url, data, type)`
- `deleteRequest(url, params)`
- `useSort(data)`

JSON requests should pass `"json"` as the `type` argument.

## Routes

Dashboard routes are mounted under `/dashboard/`:

- `/`
- `/schedule-history`
- `/groups`
- `/devices`
- `/schedules`

Unauthenticated dashboard access redirects to `/login`.
