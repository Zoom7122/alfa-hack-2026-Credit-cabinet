import { unwrap } from '../../api/client';
import { COLLECTOR_BASE_URL } from '../../api/collector/client';
import { session } from '../../api/session';
import type { SavedDraft } from './draftTypes';
export type { SavedDraft } from './draftTypes';

export const savedDraftKey = (id: string) => ['collector', 'draft', id] as const;

async function requestDraft(id: string, body?: { data: SavedDraft['data']; version: number }): Promise<SavedDraft> {
    const headers = new Headers({ 'Content-Type': 'application/json' });
    const token = session.getToken();
    if (token) headers.set('Authorization', `Bearer ${token}`);
    const response = await fetch(`${COLLECTOR_BASE_URL}/applications/${id}/draft`, {
        headers, method: body ? 'PUT' : 'GET', body: body ? JSON.stringify(body) : undefined,
    });
    const json = await response.json();
    return unwrap({ response, ...(response.ok ? { data: json as SavedDraft } : { error: json }) });
}

export const loadSavedDraft = (id: string) => requestDraft(id);
export const saveDraft = (id: string, data: SavedDraft['data'], version: number) => requestDraft(id, { data, version });
