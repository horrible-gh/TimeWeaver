const API_PORT = 8000;
const API_CONTEXT = "/time_weaver";

const config = {
    API_SERVER_URL: `${window.location.protocol}//${window.location.hostname}:${API_PORT}${API_CONTEXT}`,
};

export default config;
