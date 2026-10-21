(() => {
  try {
    document.documentElement.dataset.theme = localStorage.getItem('minio-web-theme') === 'dark' ? 'dark' : 'light';
  } catch { /* Documentation also works when local storage is unavailable. */ }
  SwaggerUIBundle({
    url: '/api/openapi.json', dom_id: '#swagger-ui', deepLinking: true, filter: true,
    supportedSubmitMethods: [], persistAuthorization: false, validatorUrl: null,
    displayRequestDuration: false, docExpansion: 'none', tryItOutEnabled: false,
    defaultModelsExpandDepth: 0,
  });
})();
