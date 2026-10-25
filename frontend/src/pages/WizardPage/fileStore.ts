/*
 * Содержимое файлов анкеты до отправки — в IndexedDB: черновик переживает перезагрузку, значит, и файлы тоже.
 * В localStorage файлы не влезут, а загрузки по одному у data-collector нет — всё уходит одним multipart.
 * Нет IndexedDB (приватный режим, запрет сайта) — держим файлы в памяти вкладки: до перезагрузки.
 */

const DB_NAME = 'credit-cabinet-files';
const STORE = 'files';

const memory = new Map<string, File>();
let connection: Promise<IDBDatabase | undefined> | undefined;

function open(): Promise<IDBDatabase | undefined> {
    connection ??= new Promise((resolve) => {
        try {
            const request = indexedDB.open(DB_NAME, 1);
            request.onupgradeneeded = () => request.result.createObjectStore(STORE);
            request.onsuccess = () => resolve(request.result);
            request.onerror = () => resolve(undefined);
            request.onblocked = () => resolve(undefined);
        } catch {
            resolve(undefined);
        }
    });
    return connection;
}

/** Результат — после завершения транзакции (oncomplete), а не запроса: только тогда запись точно легла. */
async function run<T>(mode: IDBTransactionMode, fn: (store: IDBObjectStore) => IDBRequest<T>): Promise<T | undefined> {
    const db = await open();
    if (!db) return undefined;
    return new Promise((resolve, reject) => {
        const tx = db.transaction(STORE, mode);
        const request = fn(tx.objectStore(STORE));
        tx.oncomplete = () => resolve(request.result);
        tx.onerror = () => reject(tx.error ?? new Error('IndexedDB: ошибка'));
        tx.onabort = () => reject(tx.error ?? new Error('IndexedDB: транзакция отменена'));
    });
}

export const isPersistent = async () => Boolean(await open());

export async function putFile(key: string, file: File) {
    if (await open()) await run('readwrite', (s) => s.put(file, key));
    else memory.set(key, file);
}

export async function getFile(key: string): Promise<File | undefined> {
    if (!(await open())) return memory.get(key);
    return run('readonly', (s) => s.get(key) as IDBRequest<File | undefined>);
}

export async function deleteFile(key: string) {
    memory.delete(key);
    if (await open()) await run('readwrite', (s) => s.delete(key));
}

export async function listFileKeys(): Promise<string[]> {
    if (!(await open())) return [...memory.keys()];
    const keys = await run('readonly', (s) => s.getAllKeys());
    return (keys ?? []).map(String);
}
