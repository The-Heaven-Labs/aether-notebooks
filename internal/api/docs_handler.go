package api

import (
	_ "embed"
	"net/http"
)

//go:embed docs/swagger.json
var swaggerJSON []byte

// @Summary Swagger JSON
// @Description Returns the OpenAPI specification in JSON format
// @Tags docs
// @Produce json
// @Success 200 {object} map[string]any
// @Router /swagger.json [get]
func (s *Server) handleSwaggerJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(swaggerJSON)
}

// @Summary Swagger UI
// @Description Serves the Swagger UI documentation page
// @Tags docs
// @Produce html
// @Success 200 {string} string
// @Router /docs [get]
func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
  <title>Aether API Documentation</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" >
  <style>
    :root {
      --bg: #fafafa;
      --bg-card: #ffffff;
      --text-primary: #1a202c;
      --text-secondary: #4a5568;
      --border: #e2e8f0;
      --border-light: #f7fafc;
      --accent: #6366f1;
      --accent-hover: #5558e6;
      --topbar-bg: #1a1a2e;
      --topbar-input-bg: #2d3748;
      --topbar-input-border: #4a5568;
      --topbar-input-text: #e2e8f0;
    }
    [data-theme="dark"] {
      --bg: #141414;
      --bg-card: #1c1c1c;
      --text-primary: #e8e8e8;
      --text-secondary: #aaa;
      --border: #2e2e2e;
      --border-light: #242424;
      --accent: #9b8fc4;
      --accent-hover: #b8a9d8;
      --topbar-bg: #0a0a0a;
      --topbar-input-bg: #1e1e1e;
      --topbar-input-border: #2e2e2e;
      --topbar-input-text: #e8e8e8;
    }
    body {
      margin: 0;
      padding: 0;
      background: var(--bg);
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      transition: background 0.3s, color 0.3s;
    }
    .swagger-ui .topbar {
      background: var(--topbar-bg);
      padding: 20px 0;
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 20px 40px;
    }
    .swagger-ui .topbar .download-url-wrapper {
      display: flex;
      align-items: center;
      gap: 12px;
    }
    .swagger-ui .topbar .download-url-wrapper .download-url-input {
      border: 1px solid var(--topbar-input-border);
      background: var(--topbar-input-bg);
      color: var(--topbar-input-text);
      border-radius: 4px;
      padding: 8px 12px;
    }
    .swagger-ui .info {
      margin: 30px 0;
    }
    .swagger-ui .info .title {
      font-size: 24px;
      color: var(--text-primary) !important;
    }
    .swagger-ui .info .description {
      font-size: 14px;
      color: var(--text-primary) !important;
    }
    .swagger-ui .info .title span {
      color: var(--text-primary) !important;
    }
    .swagger-ui .info .description span {
      color: var(--text-primary) !important;
    }
    .swagger-ui .info h1 {
      color: var(--text-primary) !important;
    }
    .swagger-ui .info h2 {
      color: var(--text-primary) !important;
    }
    .swagger-ui .info p {
      color: var(--text-primary) !important;
    }
    .swagger-ui .scheme-container {
      background: var(--bg-card);
      border: 1px solid var(--border);
    }
    .swagger-ui .scheme-container .servers-title {
      color: var(--text-primary);
    }
    .swagger-ui .scheme-container label {
      color: var(--text-primary);
    }
    .swagger-ui .scheme-container select {
      color: var(--text-primary);
      background: var(--bg);
      border-color: var(--border);
    }
    .swagger-ui .opblock {
      border: 1px solid var(--border);
      border-radius: 4px;
      margin-bottom: 10px;
      background: var(--bg-card);
    }
    .swagger-ui .opblock.opblock-get {
      border-left: 4px solid #48bb78;
    }
    .swagger-ui .opblock.opblock-get .opblock-summary-method {
      background: #48bb78;
    }
    .swagger-ui .opblock.opblock-post {
      border-left: 4px solid #4299e1;
    }
    .swagger-ui .opblock.opblock-post .opblock-summary-method {
      background: #4299e1;
    }
    .swagger-ui .opblock.opblock-put {
      border-left: 4px solid #ed8936;
    }
    .swagger-ui .opblock.opblock-put .opblock-summary-method {
      background: #ed8936;
    }
    .swagger-ui .opblock.opblock-delete {
      border-left: 4px solid #f56565;
    }
    .swagger-ui .opblock.opblock-delete .opblock-summary-method {
      background: #f56565;
    }
    .swagger-ui .opblock .opblock-summary {
      border-color: var(--border);
    }
    .swagger-ui .opblock .opblock-summary-method {
      color: #fff;
      font-weight: 600;
    }
    .swagger-ui .opblock .opblock-summary-path {
      color: var(--text-primary);
    }
    .swagger-ui .opblock .opblock-summary-description {
      color: var(--text-secondary);
    }
    .swagger-ui .opblock .opblock-summary-path__deprecated {
      color: var(--text-secondary);
    }
    .swagger-ui .btn {
      background: var(--accent);
      border-color: var(--accent);
      color: #fff;
    }
    .swagger-ui .btn:hover {
      background: var(--accent-hover);
      border-color: var(--accent-hover);
    }
    .swagger-ui .btn.try-out__btn {
      background: var(--accent);
      color: #fff;
    }
    .swagger-ui .btn.cancel {
      background: transparent;
      border: 1px solid var(--border);
      color: var(--text-secondary);
    }
    .swagger-ui .btn.execute {
      background: var(--accent);
      color: #fff;
    }
    .swagger-ui table thead tr td,
    .swagger-ui table thead tr th {
      border-bottom: 1px solid var(--border);
      color: var(--text-primary);
    }
    .swagger-ui table tbody tr td,
    .swagger-ui table tbody tr th {
      border-bottom: 1px solid var(--border-light);
      color: var(--text-secondary);
    }
    .swagger-ui .model-box {
      background: var(--bg-card);
      border: 1px solid var(--border);
    }
    .swagger-ui section.models {
      border: 1px solid var(--border);
      background: var(--bg-card);
    }
    .swagger-ui section.models h4 {
      color: var(--text-primary);
    }
    .swagger-ui .model-title {
      color: var(--text-primary);
    }
    .swagger-ui .model {
      color: var(--text-secondary);
    }
    .swagger-ui .model-toggle {
      color: var(--text-primary);
    }
    /* Section headers (auth, users, notebooks, etc.) */
    .swagger-ui .opblock-tag {
      color: var(--text-primary);
      border-color: var(--border);
    }
    .swagger-ui .opblock-tag:hover {
      color: var(--accent);
    }
    /* Expanded operation details */
    .swagger-ui .opblock .opblock-description-wrapper {
      color: var(--text-secondary);
    }
    .swagger-ui .opblock .opblock-external-docs-wrapper {
      color: var(--text-secondary);
    }
    /* Response section */
    .swagger-ui .responses-inner {
      color: var(--text-secondary);
    }
    .swagger-ui .response-col_status {
      color: var(--text-primary);
    }
    .swagger-ui .response-col_links {
      color: var(--text-secondary);
    }
    /* Code blocks */
    .swagger-ui .highlight-code {
      background: var(--bg);
    }
    .swagger-ui .microlight {
      background: var(--bg);
      color: var(--text-primary);
    }
    a {
      color: var(--accent);
    }
    a:hover {
      color: var(--accent-hover);
    }
    #theme-toggle {
      position: fixed;
      top: 20px;
      right: 20px;
      z-index: 9999;
      background: var(--accent);
      border: none;
      color: white;
      padding: 10px 16px;
      border-radius: 6px;
      cursor: pointer;
      font-size: 14px;
      font-weight: 500;
      display: flex;
      align-items: center;
      gap: 8px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.2);
      transition: background 0.2s, transform 0.1s;
    }
    #theme-toggle:hover {
      background: var(--accent-hover);
      transform: translateY(-1px);
    }
    #theme-toggle:active {
      transform: translateY(0);
    }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"> </script>
  <script>
    // Theme toggle
    const docHtml = document.documentElement;
    const savedTheme = localStorage.getItem('aether-docs-theme') || 'light';
    if (savedTheme === 'dark') {
      docHtml.setAttribute('data-theme', 'dark');
    }

    function toggleTheme() {
      const current = docHtml.getAttribute('data-theme');
      const next = current === 'dark' ? 'light' : 'dark';
      docHtml.setAttribute('data-theme', next);
      localStorage.setItem('aether-docs-theme', next);
      document.getElementById('theme-icon').textContent = next === 'dark' ? '☀️' : '🌙';
    }

    // Create toggle button immediately
    const themeBtn = document.createElement('button');
    themeBtn.id = 'theme-toggle';
    themeBtn.onclick = toggleTheme;
    themeBtn.innerHTML = '<span id="theme-icon">' + (savedTheme === 'dark' ? '☀️' : '🌙') + '</span> Toggle Theme';
    document.body.appendChild(themeBtn);

    SwaggerUIBundle({
      url: "/swagger.json",
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout",
      deepLinking: true,
      docExpansion: 'none',
    })
  </script>
</body>
</html>`))
}
