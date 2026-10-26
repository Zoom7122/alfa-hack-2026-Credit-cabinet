import { http, HttpResponse } from 'msw';

import type { CollectorDocument } from '../../api/collector/types';
import type { Document, DocumentType } from '../../api/types';
import { isTestDocument, TEST_DOCUMENT_SHA256 } from '../../shared/lib/testDocument';
import { authenticate, problem, toApplication, url } from './handlers';
import { mutateCollector, newId, readCollector, type StoredCollectorApp } from './store';

function view(app: StoredCollectorApp, doc: CollectorDocument): Document & { generation?: number } {
    const effective = toApplication(app).documents?.find((d) => d.id === doc.id) ?? doc;
    return {
        id: doc.id ?? '', type: (doc.type ?? 'OTHER') as DocumentType, file_name: doc.file_name ?? '',
        mime_type: doc.mime_type ?? 'application/octet-stream', size_bytes: doc.size_bytes ?? 0,
        uploaded_at: doc.uploaded_at ?? app.created_at, origin: 'CLIENT_UPLOAD',
        info_request_id: doc.info_request_id, signed_document_id: doc.signed_document_id,
        generation: doc.generation,
        is_test: doc.is_test,
        recognition: { state: doc.generation ? 'PENDING' : 'NOT_APPLICABLE' },
        signatures: { state: effective.signature_state ?? 'NOT_SIGNED', verdict: effective.signature_verdict },
    };
}

export const collectorDocumentHandlers = [
    http.get(url('/applications/:id/documents'), ({ params }) => {
        const app = readCollector().apps.find((a) => a.id === params.id);
        if (!app) return problem(404, 'NOT_FOUND', 'Заявка не найдена');
        return HttpResponse.json({ items: app.documents.map((doc) => view(app, doc)) });
    }),
    http.post(url('/applications/:id/documents'), async ({ request, params }) => {
        const app = readCollector().apps.find((a) => a.id === params.id);
        if (!app) return problem(404, 'NOT_FOUND', 'Заявка не найдена');
        const { user } = authenticate(request);
        if (user?.id !== app.representative.id) return problem(403, 'FORBIDDEN', 'Нет доступа');
        const form = await request.formData();
        const file = form.get('file');
        const type = String(form.get('type'));
        const target = String(form.get('signed_document_id') ?? '');
        if (!(file instanceof File) || file.size === 0 || file.size > 20 * 1024 * 1024) return problem(422, 'VALIDATION_FAILED', 'Нужен файл до 20 МБ');
        const parent = app.documents.find((d) => d.id === target);
        if (type === 'SIGNATURE' && !parent) return problem(422, 'VALIDATION_FAILED', 'Подписанный документ не найден');
        const test = await isTestDocument(file);
        const doc: CollectorDocument = {
            id: newId('doc'), type: type as CollectorDocument['type'], file_name: file.name, mime_type: file.type,
            size_bytes: file.size, uploaded_at: new Date().toISOString(), info_request_id: String(form.get('info_request_id') ?? ''),
            signed_document_id: target || undefined, signature_state: type === 'SIGNATURE' && !test && !parent?.is_test ? 'PENDING' : 'NOT_SIGNED',
            recognition_state: 'NOT_APPLICABLE', generation: !test && ['FINANCIAL_STATEMENTS', 'TAX_RETURN'].includes(type) ? 1 : 0,
            is_test: test, sha256: test ? TEST_DOCUMENT_SHA256 : undefined,
        };
        mutateCollector((db) => {
            const stored = db.apps.find((a) => a.id === app.id);
            if (!stored) return;
            stored.documents.push(doc);
            const signed = stored.documents.find((d) => d.id === target);
            if (signed && !signed.is_test) signed.signature_state = 'PENDING';
        });
        return HttpResponse.json(view(app, doc), { status: 201 });
    }),
    http.delete(url('/applications/:id/documents/:documentId'), ({ params }) => {
        mutateCollector((db) => {
            const app = db.apps.find((a) => a.id === params.id);
            if (app) app.documents = app.documents.filter((d) => d.id !== params.documentId && d.signed_document_id !== params.documentId);
        });
        return new HttpResponse(null, { status: 204 });
    }),
    http.get(url('/applications/:id/documents/:documentId/financial-result'), ({ params }) => {
        const app = readCollector().apps.find((a) => a.id === params.id);
        const doc = app?.documents.find((d) => d.id === params.documentId);
        if (!doc) return problem(404, 'NOT_FOUND', 'Документ не найден');
        if (doc.is_test) return problem(404, 'NOT_FOUND', 'Тестовый документ не обрабатывается');
        if (Date.now() - Date.parse(doc.uploaded_at ?? '') < 1500) return HttpResponse.json({ state: 'PENDING' }, { status: 202 });
        return HttpResponse.json({ document_id: doc.id, extracted: { revenue_2025: 41_600_000 } });
    }),
    http.post(url('/applications/:id/documents/:documentId/signature-verification'), ({ params }) => {
        const app = readCollector().apps.find((a) => a.id === params.id);
        const doc = app?.documents.find((d) => d.id === params.documentId);
        if (!doc) return problem(404, 'NOT_FOUND', 'Документ не найден');
        if (doc.is_test) return HttpResponse.json({ state: 'NOT_SIGNED' }, { status: 202 });
        mutateCollector((db) => {
            const stored = db.apps.find((a) => a.id === app?.id);
            if (stored) for (const d of stored.documents) if (d.id === doc.id || d.signed_document_id === doc.id) d.signature_state = 'PENDING';
        });
        return HttpResponse.json({ state: 'PENDING' }, { status: 202 });
    }),
];
