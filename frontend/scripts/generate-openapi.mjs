import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import converter from 'swagger2openapi';
import YAML from 'yaml';

const root = new URL('../../', import.meta.url);
const source = JSON.parse(await readFile(new URL('data-collector/docs/swagger.json', root), 'utf8'));
const { openapi: spec } = await converter.convertObj(source, { targetVersion: '3.0.3' });
const direct = { url: 'http://localhost:8081/api/v1', description: 'Backend напрямую; порт DATA_COLLECTOR_PORT' };
spec.servers = [direct, {
  url: 'http://localhost:3000/collector/api/v1',
  description: 'Через frontend nginx; только разрешённые клиентские маршруты',
}];
spec.components.securitySchemes.bearer = { type: 'http', scheme: 'bearer', bearerFormat: 'mock.<user_id>' };

const requiredAuth = new Set([
  'get /me', 'get /applicants', 'get /applications', 'post /counterparty/lookup',
  'post /applications/{id}/offers/{offerId}/accept',
  'get /applications/{id}/signing', 'post /applications/{id}/signing/confirm',
]);
const publicAuth = new Set(['get /auth/test-users', 'post /auth/mock-login']);
const internal = new Set(['/check-mock', '/borrowers/{id}/statements', '/credit-events']);
const methods = new Set(['get', 'post', 'put', 'patch', 'delete']);

for (const [path, item] of Object.entries(spec.paths)) {
  for (const [method, operation] of Object.entries(item)) {
    if (!methods.has(method)) continue;
    operation.operationId = method + path.replace(/[{}]/g, '').split('/').filter(Boolean)
      .map((part) => part.replace(/(^|[-_])([a-z])/g, (_, _separator, char) => char.toUpperCase())).join('');
    const key = `${method} ${path}`;
    operation.security = publicAuth.has(key) ? []
      : requiredAuth.has(key) || path.startsWith('/backoffice/') ? [{ bearer: [] }]
        : [{}, { bearer: [] }];
    if (internal.has(path)) operation.servers = [direct];
    if (!publicAuth.has(key)) {
      operation.responses['401'] ??= {
        description: 'Неверный переданный токен либо вход обязателен для этой заявки',
        content: { 'application/problem+json': { schema: { $ref: '#/components/schemas/Problem' } } },
      };
    }
    for (const [status, response] of Object.entries(operation.responses)) {
      if (Number(status) < 400 || !response.content?.['application/json']) continue;
      const media = response.content['application/json'];
      if (media.schema?.$ref === '#/components/schemas/Problem') {
        response.content = { 'application/problem+json': media };
      } else if (media.schema?.$ref === '#/components/schemas/financial.documentErrorResponse') {
        response.content['application/problem+json'] = media;
      }
    }
  }
}

// Swagger 2 cannot express the two distinct body schemas of these handlers.
const questionnaire = spec.paths['/applications/{id}/questionnaire'].post;
questionnaire.requestBody.content['application/json'] = {
  schema: { $ref: '#/components/schemas/QuestionnaireRequest' },
};
questionnaire.requestBody.required = true;
const questionnaireFiles = questionnaire.requestBody.content['multipart/form-data'].schema.properties.files;
questionnaireFiles.items = { type: 'string', format: 'binary' };
questionnaireFiles.maxItems = 10;
const upload = spec.paths['/applications/{id}/documents'].post.requestBody.content['multipart/form-data'].schema;
upload.properties.signatures.items = { type: 'string', format: 'binary' };
upload.properties.signatures.maxItems = 2;
spec.components.schemas['financial.SignatureReport'].properties.signed_content = {
  type: 'string', format: 'byte', description: 'Base64 точных вложенных байтов документа, если валидатор их возвращает.',
};
const answer = spec.paths['/applications/{id}/info-requests/{requestId}/answer'].post;
answer.requestBody.required = true;
answer.requestBody.content['multipart/form-data'] = {
  schema: {
    type: 'object', required: ['answer'],
    properties: {
      answer: { type: 'string', description: 'JSON объекта InfoRequestAnswerInput' },
      files: { type: 'array', maxItems: 10, items: { type: 'string', format: 'binary' } },
    },
  },
};
spec.components.schemas.InfoRequestAnswerInput.properties.value = {
  description: 'JSON-значение по input.type вопроса: число NUMBER/MONEY, код CHOICE или строка TEXT; для NONE можно не передавать.',
};
const signatureReport = spec.paths['/applications/{id}/documents/{documentId}/signature-report'].get;
signatureReport.responses['200'].content['application/json'].schema = { anyOf: [
  { $ref: '#/components/schemas/httptransport.signatureReportResponse' },
  { $ref: '#/components/schemas/financial.SignatureReport' },
] };
for (const schema of Object.values(spec.components.schemas)) {
  for (const property of Object.values(schema.properties ?? {})) {
    if (property['x-raw-json']) delete property.type;
  }
}

// The liveness route is outside Swagger's /api/v1 basePath.
spec.paths['/healthz'] = { get: {
  operationId: 'getHealthz', summary: 'Процесс HTTP API работает', tags: ['health'], security: [],
  servers: [{ url: 'http://localhost:8081' }],
  responses: { 200: { description: 'Процесс доступен; не проверяет готовность всего конвейера', content: {
    'application/json': { schema: { type: 'object', required: ['status'], properties: {
      status: { type: 'string', enum: ['ok'] },
    } } },
  } } },
} };

const destination = new URL('api/openapi.yaml', root);
await writeFile(destination, '# Generated from backend Swagger by npm run gen:openapi. See docs/api.md.\n'
  + YAML.stringify(spec, { lineWidth: 110, aliasDuplicateObjects: false }));
console.log(`Updated ${fileURLToPath(destination)} (${Object.keys(spec.paths).length} paths)`);
